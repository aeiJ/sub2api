package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// OpenAIPriorityDrainTTFTThresholdExtraKey stores an account-level override
	// that must remain available in scheduler snapshots.
	OpenAIPriorityDrainTTFTThresholdExtraKey = "openai_priority_drain_ttft_threshold_seconds"
	openAIPriorityDrainTTFTThresholdExtraKey = OpenAIPriorityDrainTTFTThresholdExtraKey
	defaultOpenAIPriorityDrainTTFTThreshold  = 15
	defaultOpenAIPriorityDrainSlowCount      = 2
	defaultOpenAIPriorityDrainWindow         = 15 * time.Minute
	defaultOpenAIPriorityDrainCooldown       = 15 * time.Minute
)

type openAIPriorityDrainSettings struct {
	enabled              bool
	ttftThresholdSeconds int
	consecutiveSlowCount int
	statisticsWindow     time.Duration
	softCooldown         time.Duration
}

func (s openAIPriorityDrainSettings) normalized() openAIPriorityDrainSettings {
	if s.ttftThresholdSeconds < 1 || s.ttftThresholdSeconds > 120 {
		s.ttftThresholdSeconds = defaultOpenAIPriorityDrainTTFTThreshold
	}
	if s.consecutiveSlowCount < 1 {
		s.consecutiveSlowCount = defaultOpenAIPriorityDrainSlowCount
	}
	if s.statisticsWindow <= 0 {
		s.statisticsWindow = defaultOpenAIPriorityDrainWindow
	}
	if s.softCooldown <= 0 {
		s.softCooldown = defaultOpenAIPriorityDrainCooldown
	}
	return s
}

type openAIPriorityDrainMetrics struct {
	oauthPreferredTotal             atomic.Int64
	oauthFullFallbackTotal          atomic.Int64
	oauthUnschedulableFallbackTotal atomic.Int64
	apiKeyCooldownTotal             atomic.Int64
	apiKeyAllCooledFallbackTotal    atomic.Int64
	apiKeyRecoveredTotal            atomic.Int64
	redisFallbackTotal              atomic.Int64
}

// OpenAIAccountOrderingHook owns only the priority-drain ordering and API Key
// TTFT observation. OpenAI setup-token accounts follow the OAuth ordering path.
// Candidate eligibility, slot acquisition, stickiness, DB rechecks, and
// failover stay in defaultOpenAIAccountScheduler.
type OpenAIAccountOrderingHook struct {
	stateStore OpenAIPriorityDrainTTFTStateStore
	metrics    openAIPriorityDrainMetrics
}

func NewOpenAIAccountOrderingHook(snapshot *SchedulerSnapshotService) *OpenAIAccountOrderingHook {
	var stateStore OpenAIPriorityDrainTTFTStateStore
	if snapshot != nil {
		stateStore = snapshot.OpenAIPriorityDrainTTFTStateStore()
	}
	return &OpenAIAccountOrderingHook{stateStore: stateStore}
}

type openAIPriorityDrainCandidate struct {
	candidate openAIAccountCandidateScore
	state     OpenAIPriorityDrainTTFTState
	tie       uint64
}

// openAIPriorityDrainAttemptTrace records why an earlier OAuth candidate was
// skipped after the hook had ordered an otherwise eligible pool. It is kept
// outside the hook so candidate freshness and slot acquisition remain owned by
// the upstream scheduler path.
type openAIPriorityDrainAttemptTrace struct {
	oauthSlotFull      bool
	oauthUnschedulable bool
}

// Order returns a complete ordering for the already-filtered OpenAI pool.
// Returning applied=false leaves the caller on the upstream scheduler path.
func (h *OpenAIAccountOrderingHook) Order(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	candidates []openAIAccountCandidateScore,
	settings openAIPriorityDrainSettings,
) (order []openAIAccountCandidateScore, applied bool) {
	if h == nil || !settings.enabled || normalizeOpenAICompatiblePlatform(req.Platform) != PlatformOpenAI {
		return nil, false
	}
	settings = settings.normalized()

	oauth := make([]openAIPriorityDrainCandidate, 0, len(candidates))
	apiKeys := make([]openAIPriorityDrainCandidate, 0, len(candidates))
	recognizedCandidate := false
	for _, candidate := range candidates {
		if candidate.account == nil {
			continue
		}
		switch {
		case isOpenAIPriorityDrainOAuthLike(candidate.account):
			recognizedCandidate = true
			if !openAIPriorityDrainAccountIsGrouped(candidate.account) {
				continue
			}
			oauth = append(oauth, openAIPriorityDrainCandidate{candidate: candidate})
		case candidate.account.IsOpenAIApiKey():
			recognizedCandidate = true
			if !openAIPriorityDrainAccountIsGrouped(candidate.account) {
				continue
			}
			apiKeys = append(apiKeys, openAIPriorityDrainCandidate{candidate: candidate})
		default:
			// A pool containing another account kind may have special upstream
			// semantics. Leave it entirely to the original scheduler.
			return nil, false
		}
	}
	if len(oauth) == 0 && len(apiKeys) == 0 {
		if recognizedCandidate {
			return []openAIAccountCandidateScore{}, true
		}
		return nil, false
	}

	rng := newOpenAISelectionRNG(deriveOpenAISelectionSeed(req))
	for i := range oauth {
		oauth[i].tie = rng.nextUint64()
	}
	for i := range apiKeys {
		apiKeys[i].tie = rng.nextUint64()
	}

	sortOpenAIPriorityDrainOAuth(oauth)

	if len(apiKeys) > 0 && h.stateStore != nil {
		policies := make(map[int64]OpenAIPriorityDrainTTFTPolicy, len(apiKeys))
		for _, candidate := range apiKeys {
			policies[candidate.candidate.account.ID] = openAIPriorityDrainTTFTPolicyForAccount(candidate.candidate.account, settings)
		}
		states, err := h.stateStore.GetOpenAIPriorityDrainTTFTStates(ctx, policies)
		if err != nil {
			h.metrics.redisFallbackTotal.Add(1)
			slog.Warn("openai_priority_drain_redis_state_read_failed", "error", err)
		} else {
			for i := range apiKeys {
				apiKeys[i].state = states[apiKeys[i].candidate.account.ID]
			}
		}
	} else if len(apiKeys) > 0 {
		h.metrics.redisFallbackTotal.Add(1)
	}

	order = make([]openAIAccountCandidateScore, 0, len(candidates))
	for _, candidate := range oauth {
		order = append(order, candidate.candidate)
	}

	if len(apiKeys) == 0 {
		return order, true
	}

	now := time.Now()
	active := make([]openAIPriorityDrainCandidate, 0, len(apiKeys))
	cooled := make([]openAIPriorityDrainCandidate, 0, len(apiKeys))
	for _, candidate := range apiKeys {
		if candidate.state.IsCoolingDown(now) {
			cooled = append(cooled, candidate)
		} else {
			active = append(active, candidate)
		}
	}
	if len(active) > 0 {
		sortOpenAIPriorityDrainAPIKeys(active)
		for _, candidate := range active {
			order = append(order, candidate.candidate)
		}
		return order, true
	}

	// Soft cooldown is never a hard outage: if every key is cooled, keep
	// serving from the least-slow account and let normal concurrency handling
	// decide whether it can accept this request.
	h.metrics.apiKeyAllCooledFallbackTotal.Add(1)
	sort.Slice(cooled, func(i, j int) bool {
		left, right := cooled[i], cooled[j]
		leftTTFT, rightTTFT := left.state.LastTTFTMs, right.state.LastTTFTMs
		if leftTTFT <= 0 {
			leftTTFT = int64(^uint64(0) >> 1)
		}
		if rightTTFT <= 0 {
			rightTTFT = int64(^uint64(0) >> 1)
		}
		if leftTTFT != rightTTFT {
			return leftTTFT < rightTTFT
		}
		if left.candidate.account.Priority != right.candidate.account.Priority {
			return left.candidate.account.Priority < right.candidate.account.Priority
		}
		if openAIPriorityDrainCurrentConcurrency(left) != openAIPriorityDrainCurrentConcurrency(right) {
			return openAIPriorityDrainCurrentConcurrency(left) < openAIPriorityDrainCurrentConcurrency(right)
		}
		return left.tie < right.tie
	})
	for _, candidate := range cooled {
		order = append(order, candidate.candidate)
	}
	return order, true
}

func openAIPriorityDrainAccountIsGrouped(account *Account) bool {
	return account != nil && (len(account.GroupIDs) > 0 || len(account.AccountGroups) > 0)
}

func isOpenAIPriorityDrainOAuthLike(account *Account) bool {
	return account != nil && account.IsOpenAI() && account.IsOAuth()
}

// sortOpenAIPriorityDrainOAuth uses the account ID as the deterministic
// tiebreaker when multiple OAuth or setup-token accounts share a priority.
func sortOpenAIPriorityDrainOAuth(candidates []openAIPriorityDrainCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.candidate.account.Priority != right.candidate.account.Priority {
			return left.candidate.account.Priority < right.candidate.account.Priority
		}
		return left.candidate.account.ID < right.candidate.account.ID
	})
}

func sortOpenAIPriorityDrainAPIKeys(candidates []openAIPriorityDrainCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.candidate.account.Priority != right.candidate.account.Priority {
			return left.candidate.account.Priority < right.candidate.account.Priority
		}
		if openAIPriorityDrainCurrentConcurrency(left) != openAIPriorityDrainCurrentConcurrency(right) {
			return openAIPriorityDrainCurrentConcurrency(left) < openAIPriorityDrainCurrentConcurrency(right)
		}
		return left.tie < right.tie
	})
}

func openAIPriorityDrainCurrentConcurrency(candidate openAIPriorityDrainCandidate) int {
	if candidate.candidate.loadInfo == nil {
		return 0
	}
	return candidate.candidate.loadInfo.CurrentConcurrency
}

func (h *OpenAIAccountOrderingHook) ObserveTTFT(
	ctx context.Context,
	account *Account,
	success bool,
	firstTokenMs *int,
	settings openAIPriorityDrainSettings,
) {
	if h == nil || !settings.enabled || h.stateStore == nil || !success || firstTokenMs == nil || account == nil || !account.IsOpenAIApiKey() || !openAIPriorityDrainAccountIsGrouped(account) {
		return
	}
	settings = settings.normalized()
	thresholdSeconds := openAIPriorityDrainTTFTThresholdSeconds(account, settings.ttftThresholdSeconds)
	policy := openAIPriorityDrainTTFTPolicyForAccount(account, settings)
	observation, err := h.stateStore.ObserveOpenAIPriorityDrainTTFT(
		ctx,
		account.ID,
		int64(*firstTokenMs),
		int64(thresholdSeconds)*1000,
		settings.consecutiveSlowCount,
		settings.statisticsWindow,
		settings.softCooldown,
		policy,
	)
	if err != nil {
		h.metrics.redisFallbackTotal.Add(1)
		slog.Warn("openai_priority_drain_ttft_observe_failed", "account_id", account.ID, "error", err)
		return
	}
	if observation.EnteredCooldown {
		h.metrics.apiKeyCooldownTotal.Add(1)
	}
	if observation.Recovered {
		h.metrics.apiKeyRecoveredTotal.Add(1)
	}
}

func openAIPriorityDrainTTFTPolicyForAccount(account *Account, settings openAIPriorityDrainSettings) OpenAIPriorityDrainTTFTPolicy {
	return OpenAIPriorityDrainTTFTPolicy{
		GlobalFingerprint:  openAIPriorityDrainGlobalPolicyFingerprint(settings),
		AccountFingerprint: openAIPriorityDrainAccountPolicyFingerprint(account),
	}
}

func openAIPriorityDrainGlobalPolicyFingerprint(settings openAIPriorityDrainSettings) string {
	settings = settings.normalized()
	return fmt.Sprintf(
		"threshold=%d;count=%d;window_ms=%d;cooldown_ms=%d",
		settings.ttftThresholdSeconds,
		settings.consecutiveSlowCount,
		settings.statisticsWindow.Milliseconds(),
		settings.softCooldown.Milliseconds(),
	)
}

func openAIPriorityDrainAccountPolicyFingerprint(account *Account) string {
	if threshold, ok := openAIPriorityDrainTTFTThresholdOverrideSeconds(account); ok {
		return "threshold=" + strconv.Itoa(threshold)
	}
	return "default"
}

func openAIPriorityDrainTTFTThresholdOverrideSeconds(account *Account) (int, bool) {
	if account != nil && account.Extra != nil {
		switch value := account.Extra[openAIPriorityDrainTTFTThresholdExtraKey].(type) {
		case int:
			return value, value >= 1 && value <= 120
		case int64:
			return int(value), value >= 1 && value <= 120
		case float64:
			return int(value), value >= 1 && value <= 120 && math.Trunc(value) == value
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			return parsed, err == nil && parsed >= 1 && parsed <= 120
		}
	}
	return 0, false
}

func (h *OpenAIAccountOrderingHook) RecordSelection(order []openAIAccountCandidateScore, selected *Account, trace *openAIPriorityDrainAttemptTrace) {
	if h == nil || selected == nil {
		return
	}
	firstOAuthID := int64(0)
	for _, candidate := range order {
		if isOpenAIPriorityDrainOAuthLike(candidate.account) {
			firstOAuthID = candidate.account.ID
			break
		}
	}
	if isOpenAIPriorityDrainOAuthLike(selected) && selected.ID == firstOAuthID {
		h.metrics.oauthPreferredTotal.Add(1)
		return
	}
	if trace != nil && trace.oauthSlotFull {
		h.metrics.oauthFullFallbackTotal.Add(1)
	}
	if trace != nil && trace.oauthUnschedulable {
		h.metrics.oauthUnschedulableFallbackTotal.Add(1)
	}
}

func openAIPriorityDrainTTFTThresholdSeconds(account *Account, fallback int) int {
	if threshold, ok := openAIPriorityDrainTTFTThresholdOverrideSeconds(account); ok {
		return threshold
	}
	if fallback < 1 || fallback > 120 {
		return defaultOpenAIPriorityDrainTTFTThreshold
	}
	return fallback
}

func (h *OpenAIAccountOrderingHook) SnapshotMetrics() OpenAIPriorityDrainMetricsSnapshot {
	if h == nil {
		return OpenAIPriorityDrainMetricsSnapshot{}
	}
	return OpenAIPriorityDrainMetricsSnapshot{
		OAuthPreferredTotal:             h.metrics.oauthPreferredTotal.Load(),
		OAuthFullFallbackTotal:          h.metrics.oauthFullFallbackTotal.Load(),
		OAuthUnschedulableFallbackTotal: h.metrics.oauthUnschedulableFallbackTotal.Load(),
		APIKeyCooldownTotal:             h.metrics.apiKeyCooldownTotal.Load(),
		APIKeyAllCooledFallbackTotal:    h.metrics.apiKeyAllCooledFallbackTotal.Load(),
		APIKeyRecoveredTotal:            h.metrics.apiKeyRecoveredTotal.Load(),
		RedisFallbackTotal:              h.metrics.redisFallbackTotal.Load(),
	}
}

type OpenAIPriorityDrainMetricsSnapshot struct {
	OAuthPreferredTotal             int64 `json:"oauth_preferred_total"`
	OAuthFullFallbackTotal          int64 `json:"oauth_full_fallback_total"`
	OAuthUnschedulableFallbackTotal int64 `json:"oauth_unschedulable_fallback_total"`
	APIKeyCooldownTotal             int64 `json:"api_key_cooldown_total"`
	APIKeyAllCooledFallbackTotal    int64 `json:"api_key_all_cooled_fallback_total"`
	APIKeyRecoveredTotal            int64 `json:"api_key_recovered_total"`
	RedisFallbackTotal              int64 `json:"redis_fallback_total"`
}
