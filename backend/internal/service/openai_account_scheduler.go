package service

import (
	"container/heap"
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	openAIAccountScheduleLayerPreviousResponse = "previous_response_id"
	openAIAccountScheduleLayerSessionSticky    = "session_hash"
	openAIAccountScheduleLayerLoadBalance      = "load_balance"
	openAIAdvancedSchedulerSettingKey          = "openai_advanced_scheduler_enabled"
)

const (
	openAIAdvancedSchedulerSettingCacheTTL  = 5 * time.Second
	openAIAdvancedSchedulerSettingDBTimeout = 2 * time.Second
)

const (
	openAIQuotaHeadroomNeutralFactor      = 0.5
	openAIQuotaHeadroomSecondaryLowRemain = 0.10
	openAIQuotaHeadroomSnapshotStaleAfter = 8 * time.Hour
)

type cachedOpenAIAdvancedSchedulerSetting struct {
	enabled                     bool
	stickyWeightedEnabled       bool
	subscriptionPriorityEnabled bool
	lbTopKOverride              int
	weightOverrides             map[string]float64
	expiresAt                   int64
}

type openAIAdvancedSchedulerRuntimeSettings struct {
	enabled                     bool
	stickyWeightedEnabled       bool
	subscriptionPriorityEnabled bool
	lbTopKOverride              int
	weightOverrides             map[string]float64
}

var openAIAdvancedSchedulerSettingCache atomic.Value // *cachedOpenAIAdvancedSchedulerSetting
var openAIAdvancedSchedulerSettingSF singleflight.Group

type OpenAIAccountScheduleRequest struct {
	GroupID                 *int64
	Platform                string
	SessionHash             string
	StickyAccountID         int64
	StickyPreviousAccountID int64
	StickyWeighted          bool
	SubscriptionPriority    bool
	PreserveStickyBinding   bool
	PreviousResponseID      string
	PreviousResponseCanMove bool
	RequestedModel          string
	RequiredTransport       OpenAIUpstreamTransport
	RequiredCapability      OpenAIEndpointCapability
	RequiredImageCapability OpenAIImagesCapability
	RequireCompact          bool
	ExcludedIDs             map[int64]struct{}
}

type OpenAIAccountScheduleDecision struct {
	Layer               string
	StickyPreviousHit   bool
	StickySessionHit    bool
	CandidateCount      int
	TopK                int
	LatencyMs           int64
	LoadSkew            float64
	SelectedAccountID   int64
	SelectedAccountType string
}

type OpenAIAccountSchedulerMetricsSnapshot struct {
	SelectTotal              int64
	StickyPreviousHitTotal   int64
	StickySessionHitTotal    int64
	LoadBalanceSelectTotal   int64
	AccountSwitchTotal       int64
	SchedulerLatencyMsTotal  int64
	SchedulerLatencyMsAvg    float64
	StickyHitRatio           float64
	AccountSwitchRate        float64
	LoadSkewAvg              float64
	RuntimeStatsAccountCount int
}

type OpenAIAccountScheduler interface {
	Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)
	ReportResult(accountID int64, success bool, firstTokenMs *int)
	ReportSwitch()
	SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot
}

type openAIAccountSchedulerMetrics struct {
	selectTotal            atomic.Int64
	stickyPreviousHitTotal atomic.Int64
	stickySessionHitTotal  atomic.Int64
	loadBalanceSelectTotal atomic.Int64
	accountSwitchTotal     atomic.Int64
	latencyMsTotal         atomic.Int64
	loadSkewMilliTotal     atomic.Int64
}

type openAIAccountLoadPlan struct {
	allCandidates             []openAIAccountCandidateScore
	candidates                []openAIAccountCandidateScore
	staleSnapshotCompactRetry []openAIAccountCandidateScore
	selectionOrder            []openAIAccountCandidateScore
	candidateCount            int
	topK                      int
	loadSkew                  float64
}

type openAIAccountLoadSelectionAttempt struct {
	result              *AccountSelectionResult
	selectionOrder      []openAIAccountCandidateScore
	drain               openAIAccountDrainContext
	candidateCount      int
	topK                int
	loadSkew            float64
	compactBlocked      bool
	noCompactCandidates bool
	err                 error
}

func (m *openAIAccountSchedulerMetrics) recordSelect(decision OpenAIAccountScheduleDecision) {
	if m == nil {
		return
	}
	m.selectTotal.Add(1)
	m.latencyMsTotal.Add(decision.LatencyMs)
	m.loadSkewMilliTotal.Add(int64(math.Round(decision.LoadSkew * 1000)))
	if decision.StickyPreviousHit {
		m.stickyPreviousHitTotal.Add(1)
	}
	if decision.StickySessionHit {
		m.stickySessionHitTotal.Add(1)
	}
	if decision.Layer == openAIAccountScheduleLayerLoadBalance {
		m.loadBalanceSelectTotal.Add(1)
	}
}

func (m *openAIAccountSchedulerMetrics) recordSwitch() {
	if m == nil {
		return
	}
	m.accountSwitchTotal.Add(1)
}

type openAIAccountRuntimeStats struct {
	accounts     sync.Map
	accountCount atomic.Int64
}

type openAIAccountRuntimeStat struct {
	errorRateEWMABits             atomic.Uint64
	ttftEWMABits                  atomic.Uint64
	ttftSampleCount               atomic.Int64
	successStreak                 atomic.Int64
	latencyHealth                 atomic.Int32
	apiKeyTTFTDemotion            atomic.Int32
	apiKeyTTFTDemotedAtUnixNano   atomic.Int64
	apiKeyTTFTLastProbeAtUnixNano atomic.Int64
}

type openAIAccountLatencyHealth int32

const (
	openAIAccountLatencyHealthy openAIAccountLatencyHealth = iota
	openAIAccountLatencyDegraded
	openAIAccountLatencySevere
)

type openAIAPIKeyTTFTDemotionState int32

const (
	openAIAPIKeyTTFTNormal openAIAPIKeyTTFTDemotionState = iota
	openAIAPIKeyTTFTDemoted
)

const (
	openAIAPIKeyTTFTDemoteThresholdMs  = 15000.0
	openAIAPIKeyTTFTRecoverThresholdMs = 8000.0
	openAIAPIKeyTTFTDemotionProbeEvery = 5 * time.Minute
)

type openAIAccountLatencyConfig struct {
	degradeTTFTMs     float64
	recoverTTFTMs     float64
	severeTTFTMs      float64
	minSamples        int64
	recoverySuccesses int64
	severeErrorRate   float64
}

type openAIAccountLatencySnapshot struct {
	errorRate       float64
	ttft            float64
	hasTTFT         bool
	ttftSampleCount int64
	successStreak   int64
	health          openAIAccountLatencyHealth
}

type openAIAPIKeyTTFTDemotionSnapshot struct {
	state    openAIAPIKeyTTFTDemotionState
	demoted  bool
	ttft     float64
	hasTTFT  bool
	probeDue bool
}

func newOpenAIAccountRuntimeStats() *openAIAccountRuntimeStats {
	return &openAIAccountRuntimeStats{}
}

func (s *openAIAccountRuntimeStats) loadOrCreate(accountID int64) *openAIAccountRuntimeStat {
	if value, ok := s.accounts.Load(accountID); ok {
		stat, _ := value.(*openAIAccountRuntimeStat)
		if stat != nil {
			return stat
		}
	}

	stat := &openAIAccountRuntimeStat{}
	stat.ttftEWMABits.Store(math.Float64bits(math.NaN()))
	actual, loaded := s.accounts.LoadOrStore(accountID, stat)
	if !loaded {
		s.accountCount.Add(1)
		return stat
	}
	existing, _ := actual.(*openAIAccountRuntimeStat)
	if existing != nil {
		return existing
	}
	return stat
}

func updateEWMAAtomic(target *atomic.Uint64, sample float64, alpha float64) {
	for {
		oldBits := target.Load()
		oldValue := math.Float64frombits(oldBits)
		newValue := alpha*sample + (1-alpha)*oldValue
		if target.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
			return
		}
	}
}

func (s *openAIAccountRuntimeStats) report(accountID int64, success bool, firstTokenMs *int) {
	if s == nil || accountID <= 0 {
		return
	}
	const alpha = 0.2
	stat := s.loadOrCreate(accountID)

	errorSample := 1.0
	if success {
		errorSample = 0.0
		stat.successStreak.Add(1)
	} else {
		stat.successStreak.Store(0)
	}
	updateEWMAAtomic(&stat.errorRateEWMABits, errorSample, alpha)

	if firstTokenMs != nil && *firstTokenMs > 0 {
		stat.ttftSampleCount.Add(1)
		ttft := float64(*firstTokenMs)
		ttftBits := math.Float64bits(ttft)
		for {
			oldBits := stat.ttftEWMABits.Load()
			oldValue := math.Float64frombits(oldBits)
			if math.IsNaN(oldValue) {
				if stat.ttftEWMABits.CompareAndSwap(oldBits, ttftBits) {
					break
				}
				continue
			}
			newValue := alpha*ttft + (1-alpha)*oldValue
			if stat.ttftEWMABits.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
				break
			}
		}
	}
}

func (s *openAIAccountRuntimeStats) snapshot(accountID int64) (errorRate float64, ttft float64, hasTTFT bool) {
	if s == nil || accountID <= 0 {
		return 0, 0, false
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return 0, 0, false
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil {
		return 0, 0, false
	}
	errorRate = clamp01(math.Float64frombits(stat.errorRateEWMABits.Load()))
	ttftValue := math.Float64frombits(stat.ttftEWMABits.Load())
	if math.IsNaN(ttftValue) {
		return errorRate, 0, false
	}
	return errorRate, ttftValue, true
}

func normalizeOpenAIAccountLatencyConfig(cfg openAIAccountLatencyConfig) openAIAccountLatencyConfig {
	if cfg.degradeTTFTMs <= 0 {
		cfg.degradeTTFTMs = 8000
	}
	if cfg.recoverTTFTMs <= 0 || cfg.recoverTTFTMs >= cfg.degradeTTFTMs {
		cfg.recoverTTFTMs = 6000
	}
	if cfg.severeTTFTMs <= 0 || cfg.severeTTFTMs < cfg.degradeTTFTMs {
		cfg.severeTTFTMs = 20000
	}
	if cfg.severeTTFTMs < cfg.degradeTTFTMs {
		cfg.severeTTFTMs = cfg.degradeTTFTMs
	}
	if cfg.minSamples <= 0 {
		cfg.minSamples = 3
	}
	if cfg.minSamples > 100 {
		cfg.minSamples = 100
	}
	if cfg.recoverySuccesses <= 0 {
		cfg.recoverySuccesses = 2
	}
	if cfg.severeErrorRate <= 0 || cfg.severeErrorRate > 1 {
		cfg.severeErrorRate = 0.5
	}
	return cfg
}

func (s *openAIAccountRuntimeStats) latencySnapshot(accountID int64, cfg openAIAccountLatencyConfig) openAIAccountLatencySnapshot {
	snapshot := openAIAccountLatencySnapshot{health: openAIAccountLatencyHealthy}
	if s == nil || accountID <= 0 {
		return snapshot
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return snapshot
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil {
		return snapshot
	}

	cfg = normalizeOpenAIAccountLatencyConfig(cfg)
	snapshot.errorRate = clamp01(math.Float64frombits(stat.errorRateEWMABits.Load()))
	snapshot.ttftSampleCount = stat.ttftSampleCount.Load()
	snapshot.successStreak = stat.successStreak.Load()
	ttftValue := math.Float64frombits(stat.ttftEWMABits.Load())
	if !math.IsNaN(ttftValue) {
		snapshot.ttft = ttftValue
		snapshot.hasTTFT = true
	}

	current := openAIAccountLatencyHealth(stat.latencyHealth.Load())
	if current < openAIAccountLatencyHealthy || current > openAIAccountLatencySevere {
		current = openAIAccountLatencyHealthy
	}
	next := current
	enoughTTFTSamples := snapshot.hasTTFT && snapshot.ttftSampleCount >= cfg.minSamples
	switch {
	case enoughTTFTSamples && snapshot.ttft >= cfg.severeTTFTMs:
		next = openAIAccountLatencySevere
	case snapshot.errorRate > cfg.severeErrorRate:
		next = openAIAccountLatencySevere
	case enoughTTFTSamples && snapshot.ttft >= cfg.degradeTTFTMs:
		next = openAIAccountLatencyDegraded
	case current != openAIAccountLatencyHealthy:
		ttftRecovered := !snapshot.hasTTFT || !enoughTTFTSamples || snapshot.ttft <= cfg.recoverTTFTMs
		if ttftRecovered && snapshot.successStreak >= cfg.recoverySuccesses && snapshot.errorRate <= cfg.severeErrorRate {
			next = openAIAccountLatencyHealthy
		}
	default:
		next = openAIAccountLatencyHealthy
	}
	if next != current {
		stat.latencyHealth.Store(int32(next))
	}
	snapshot.health = next
	return snapshot
}

func (s *openAIAccountRuntimeStats) apiKeyTTFTDemotionSnapshot(account *Account) openAIAPIKeyTTFTDemotionSnapshot {
	snapshot := openAIAPIKeyTTFTDemotionSnapshot{state: openAIAPIKeyTTFTNormal}
	if s == nil || account == nil || account.ID <= 0 || !account.IsOpenAIApiKey() {
		return snapshot
	}
	value, ok := s.accounts.Load(account.ID)
	if !ok {
		return snapshot
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil {
		return snapshot
	}

	ttftValue := math.Float64frombits(stat.ttftEWMABits.Load())
	if math.IsNaN(ttftValue) {
		return snapshot
	}
	snapshot.ttft = ttftValue
	snapshot.hasTTFT = true

	current := openAIAPIKeyTTFTDemotionState(stat.apiKeyTTFTDemotion.Load())
	if current < openAIAPIKeyTTFTNormal || current > openAIAPIKeyTTFTDemoted {
		current = openAIAPIKeyTTFTNormal
	}
	next := current
	switch {
	case ttftValue > openAIAPIKeyTTFTDemoteThresholdMs:
		next = openAIAPIKeyTTFTDemoted
	case current == openAIAPIKeyTTFTDemoted && ttftValue < openAIAPIKeyTTFTRecoverThresholdMs:
		next = openAIAPIKeyTTFTNormal
	case current != openAIAPIKeyTTFTDemoted:
		next = openAIAPIKeyTTFTNormal
	}
	if next != current {
		stat.apiKeyTTFTDemotion.Store(int32(next))
	}
	snapshot.state = next
	snapshot.demoted = next == openAIAPIKeyTTFTDemoted
	nowUnix := time.Now().UnixNano()
	if next == openAIAPIKeyTTFTDemoted {
		demotedAt := stat.apiKeyTTFTDemotedAtUnixNano.Load()
		if current != openAIAPIKeyTTFTDemoted || demotedAt <= 0 {
			demotedAt = nowUnix
			stat.apiKeyTTFTDemotedAtUnixNano.Store(demotedAt)
			stat.apiKeyTTFTLastProbeAtUnixNano.Store(0)
		}
		lastProbeAt := stat.apiKeyTTFTLastProbeAtUnixNano.Load()
		probeInterval := int64(openAIAPIKeyTTFTDemotionProbeEvery)
		snapshot.probeDue = demotedAt > 0 &&
			nowUnix-demotedAt >= probeInterval &&
			(lastProbeAt <= 0 || nowUnix-lastProbeAt >= probeInterval)
	} else {
		stat.apiKeyTTFTDemotedAtUnixNano.Store(0)
		stat.apiKeyTTFTLastProbeAtUnixNano.Store(0)
	}
	return snapshot
}

func (s *openAIAccountRuntimeStats) recordAPIKeyTTFTDemotionProbe(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil || openAIAPIKeyTTFTDemotionState(stat.apiKeyTTFTDemotion.Load()) != openAIAPIKeyTTFTDemoted {
		return
	}
	stat.apiKeyTTFTLastProbeAtUnixNano.Store(time.Now().UnixNano())
}

func (s *openAIAccountRuntimeStats) size() int {
	if s == nil {
		return 0
	}
	return int(s.accountCount.Load())
}

type defaultOpenAIAccountScheduler struct {
	service           *OpenAIGatewayService
	metrics           openAIAccountSchedulerMetrics
	stats             *openAIAccountRuntimeStats
	localDrainTargets sync.Map
}

type openAIStickyEscapeConfig struct {
	enabled   bool
	ttftMs    float64
	errorRate float64
}

func newDefaultOpenAIAccountScheduler(service *OpenAIGatewayService, stats *openAIAccountRuntimeStats) OpenAIAccountScheduler {
	if stats == nil {
		stats = newOpenAIAccountRuntimeStats()
	}
	return &defaultOpenAIAccountScheduler{
		service: service,
		stats:   stats,
	}
}

func (s *defaultOpenAIAccountScheduler) Select(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	decision := OpenAIAccountScheduleDecision{}
	start := time.Now()
	defer func() {
		decision.LatencyMs = time.Since(start).Milliseconds()
		s.metrics.recordSelect(decision)
	}()

	previousResponseID := strings.TrimSpace(req.PreviousResponseID)
	if previousResponseID != "" && normalizeOpenAICompatiblePlatform(req.Platform) == PlatformOpenAI &&
		(!req.StickyWeighted || !req.PreviousResponseCanMove) {
		selection, err := s.service.selectAccountByPreviousResponseIDForCapability(
			ctx,
			req.GroupID,
			previousResponseID,
			req.RequestedModel,
			req.ExcludedIDs,
			req.RequiredCapability,
			req.RequireCompact,
		)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			if !s.isAccountTransportCompatible(selection.Account, req.RequiredTransport) {
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				selection = nil
			}
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerPreviousResponse
			decision.StickyPreviousHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			if req.SessionHash != "" {
				_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, selection.Account.ID)
			}
			return selection, decision, nil
		}
	}

	var stickyWaitAccount *Account
	if !req.StickyWeighted {
		selection, excludedStickyAccountID, preserveStickyBinding, err := s.selectBySessionHash(ctx, req)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerSessionSticky
			decision.StickySessionHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			return selection, decision, nil
		}
		if excludedStickyAccountID > 0 {
			if !preserveStickyBinding {
				if account, fetchErr := s.service.getSchedulableAccount(ctx, excludedStickyAccountID); fetchErr == nil {
					stickyWaitAccount = account
				} else {
					slog.Warn("sticky_wait_account_fetch_failed", "account_id", excludedStickyAccountID, "err", fetchErr)
				}
			}
			req.PreserveStickyBinding = preserveStickyBinding
			excludedIDs := cloneExcludedAccountIDs(req.ExcludedIDs)
			if excludedIDs == nil {
				excludedIDs = make(map[int64]struct{}, 1)
			}
			excludedIDs[excludedStickyAccountID] = struct{}{}
			req.ExcludedIDs = excludedIDs
		}
	}

	selection, candidateCount, topK, loadSkew, err := s.selectByLoadBalance(ctx, req)
	decision.Layer = openAIAccountScheduleLayerLoadBalance
	decision.CandidateCount = candidateCount
	decision.TopK = topK
	decision.LoadSkew = loadSkew
	if err != nil {
		if stickyWaitAccount != nil && isNoAvailableOpenAISelectionError(err) {
			selection, waitErr := s.service.newOpenAIStickyWaitSelection(ctx, stickyWaitAccount, s.service.schedulingConfig())
			if waitErr != nil {
				return nil, decision, waitErr
			}
			decision.SelectedAccountID = stickyWaitAccount.ID
			decision.SelectedAccountType = stickyWaitAccount.Type
			return selection, decision, nil
		}
		return nil, decision, err
	}
	if selection != nil && selection.Account != nil {
		decision.SelectedAccountID = selection.Account.ID
		decision.SelectedAccountType = selection.Account.Type
		if req.StickyWeighted {
			if req.StickyPreviousAccountID > 0 && selection.Account.ID == req.StickyPreviousAccountID {
				decision.StickyPreviousHit = true
			}
			if req.StickyAccountID > 0 && selection.Account.ID == req.StickyAccountID {
				decision.StickySessionHit = true
			}
		}
		return selection, decision, nil
	}
	if stickyWaitAccount != nil {
		waitSelection, waitErr := s.service.newOpenAIStickyWaitSelection(ctx, stickyWaitAccount, s.service.schedulingConfig())
		if waitErr != nil {
			return nil, decision, waitErr
		}
		decision.SelectedAccountID = stickyWaitAccount.ID
		decision.SelectedAccountType = stickyWaitAccount.Type
		return waitSelection, decision, nil
	}
	return selection, decision, nil
}

func (s *defaultOpenAIAccountScheduler) selectBySessionHash(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, int64, bool, error) {
	sessionHash := strings.TrimSpace(req.SessionHash)
	if sessionHash == "" || s == nil || s.service == nil || s.service.cache == nil {
		return nil, 0, false, nil
	}

	accountID := req.StickyAccountID
	if accountID <= 0 {
		var err error
		accountID, err = s.service.getStickySessionAccountID(ctx, req.GroupID, sessionHash)
		if err != nil || accountID <= 0 {
			return nil, 0, false, nil
		}
	}
	if accountID <= 0 {
		return nil, 0, false, nil
	}
	if req.ExcludedIDs != nil {
		if _, excluded := req.ExcludedIDs[accountID]; excluded {
			return nil, 0, false, nil
		}
	}

	account, err := s.service.getSchedulableAccount(ctx, accountID)
	if err != nil || account == nil {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, 0, false, nil
	}
	if shouldClearStickySession(account, req.RequestedModel) || account.Platform != normalizeOpenAICompatiblePlatform(req.Platform) || !account.IsOpenAICompatible() || !account.IsSchedulable() {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, 0, false, nil
	}
	if !s.isAccountRequestCompatible(ctx, account, req) {
		return nil, 0, false, nil
	}
	if !s.isAccountTransportCompatible(account, req.RequiredTransport) {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, 0, false, nil
	}
	account = s.service.recheckSelectedOpenAIAccountFromDB(ctx, account, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
	if account == nil || !openAIStickyAccountMatchesGroup(account, req.GroupID) || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
		_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		return nil, 0, false, nil
	}
	escapeCfg := s.service.openAIStickyEscapeConfig()
	if reason, errorRate, ttft, shouldEscape := s.shouldEscapeStickyAccount(accountID, escapeCfg); shouldEscape {
		slog.Info("sticky_escape_triggered",
			"account_id", accountID,
			"reason", reason,
			"error_rate", errorRate,
			"ttft", ttft,
		)
		return nil, accountID, true, nil
	}
	result, acquireErr := s.service.tryAcquireAccountSlot(ctx, accountID, account.Concurrency)
	if acquireErr == nil && result != nil && result.Acquired {
		_ = s.service.refreshStickySessionTTL(ctx, req.GroupID, sessionHash, s.service.openAIWSSessionStickyTTL())
		return &AccountSelectionResult{
			Account:     account,
			Acquired:    true,
			ReleaseFunc: result.ReleaseFunc,
		}, 0, false, nil
	}

	if s.service.concurrencyService != nil && acquireErr == nil && result != nil && !result.Acquired {
		return nil, accountID, false, nil
	}
	return nil, 0, false, nil
}

func openAIStickyAccountMatchesGroup(account *Account, groupID *int64) bool {
	if account == nil {
		return false
	}
	if groupID == nil {
		return len(account.AccountGroups) == 0 && len(account.GroupIDs) == 0
	}
	for _, accountGroupID := range account.GroupIDs {
		if accountGroupID == *groupID {
			return true
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.GroupID == *groupID {
			return true
		}
	}
	return false
}

func openAIAccountSchedulingPriority(account *Account) int {
	if account == nil {
		return 0
	}
	return account.Priority
}

func dedicatedAccountLess(a, b *Account) bool {
	if a == nil || b == nil {
		return a != nil && b == nil
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.ID < b.ID
}

func (s *defaultOpenAIAccountScheduler) shouldEscapeStickyAccount(accountID int64, cfg openAIStickyEscapeConfig) (reason string, errorRate float64, ttft float64, shouldEscape bool) {
	if !cfg.enabled || s == nil || s.stats == nil || accountID <= 0 {
		return "", 0, 0, false
	}
	latencyCfg := normalizeOpenAIAccountLatencyConfig(openAIAccountLatencyConfig{})
	if s.service != nil {
		latencyCfg = s.service.openAIAccountLatencyConfig()
	}
	latency := s.stats.latencySnapshot(accountID, latencyCfg)
	if latency.health != openAIAccountLatencySevere {
		return "", latency.errorRate, latency.ttft, false
	}
	if latency.hasTTFT && latency.ttft >= latencyCfg.severeTTFTMs {
		return "ttft", latency.errorRate, latency.ttft, true
	}
	if latency.errorRate > latencyCfg.severeErrorRate {
		return "error_rate", latency.errorRate, latency.ttft, true
	}
	return "latency_health", latency.errorRate, latency.ttft, true
}

type openAIAccountCandidateScore struct {
	account            *Account
	loadInfo           *AccountLoadInfo
	score              float64
	priority           int
	errorRate          float64
	ttft               float64
	hasTTFT            bool
	latencyHealth      openAIAccountLatencyHealth
	apiKeyTTFTDemotion openAIAPIKeyTTFTDemotionState
	apiKeyTTFTProbeDue bool
	drainBucket        SchedulerDrainTargetBucket
	drainCanClaim      bool
	drainReplaceTarget int64
	drainClaimBlocked  bool
}

type openAIAccountCandidateHeap []openAIAccountCandidateScore

func (h openAIAccountCandidateHeap) Len() int {
	return len(h)
}

func (h openAIAccountCandidateHeap) Less(i, j int) bool {
	// 最小堆根节点保存“最差”候选，便于 O(log k) 维护 topK。
	return isOpenAIAccountCandidateBetter(h[j], h[i])
}

func (h openAIAccountCandidateHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *openAIAccountCandidateHeap) Push(x any) {
	candidate, ok := x.(openAIAccountCandidateScore)
	if !ok {
		panic("openAIAccountCandidateHeap: invalid element type")
	}
	*h = append(*h, candidate)
}

func (h *openAIAccountCandidateHeap) Pop() any {
	old := *h
	n := len(old)
	last := old[n-1]
	*h = old[:n-1]
	return last
}

func isOpenAIAccountCandidateBetter(left openAIAccountCandidateScore, right openAIAccountCandidateScore) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.priority != right.priority {
		return left.priority < right.priority
	}
	if left.loadInfo != nil && right.loadInfo != nil {
		if left.loadInfo.LoadRate != right.loadInfo.LoadRate {
			return left.loadInfo.LoadRate < right.loadInfo.LoadRate
		}
		if left.loadInfo.WaitingCount != right.loadInfo.WaitingCount {
			return left.loadInfo.WaitingCount < right.loadInfo.WaitingCount
		}
	}
	return dedicatedAccountLess(left.account, right.account)
}

func selectTopKOpenAICandidates(candidates []openAIAccountCandidateScore, topK int) []openAIAccountCandidateScore {
	if len(candidates) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 1
	}
	if topK >= len(candidates) {
		ranked := append([]openAIAccountCandidateScore(nil), candidates...)
		sort.Slice(ranked, func(i, j int) bool {
			return isOpenAIAccountCandidateBetter(ranked[i], ranked[j])
		})
		return ranked
	}

	best := make(openAIAccountCandidateHeap, 0, topK)
	for _, candidate := range candidates {
		if len(best) < topK {
			heap.Push(&best, candidate)
			continue
		}
		if isOpenAIAccountCandidateBetter(candidate, best[0]) {
			best[0] = candidate
			heap.Fix(&best, 0)
		}
	}

	ranked := make([]openAIAccountCandidateScore, len(best))
	copy(ranked, best)
	sort.Slice(ranked, func(i, j int) bool {
		return isOpenAIAccountCandidateBetter(ranked[i], ranked[j])
	})
	return ranked
}

type openAISelectionRNG struct {
	state uint64
}

func newOpenAISelectionRNG(seed uint64) openAISelectionRNG {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	return openAISelectionRNG{state: seed}
}

func (r *openAISelectionRNG) nextUint64() uint64 {
	x := r.state
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.state = x
	return x * 2685821657736338717
}

func (r *openAISelectionRNG) nextFloat64() float64 {
	return float64(r.nextUint64()>>11) / (1 << 53)
}

func deriveOpenAISelectionSeed(req OpenAIAccountScheduleRequest) uint64 {
	hasher := fnv.New64a()
	writeValue := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		_, _ = hasher.Write([]byte(trimmed))
		_, _ = hasher.Write([]byte{0})
	}

	writeValue(req.SessionHash)
	writeValue(req.PreviousResponseID)
	writeValue(req.RequestedModel)
	if req.GroupID != nil {
		_, _ = hasher.Write([]byte(strconv.FormatInt(*req.GroupID, 10)))
	}

	seed := hasher.Sum64()
	// 对“无会话锚点”的纯负载均衡请求引入时间熵，避免固定命中同一账号。
	if strings.TrimSpace(req.SessionHash) == "" && strings.TrimSpace(req.PreviousResponseID) == "" {
		seed ^= uint64(time.Now().UnixNano())
	}
	if seed == 0 {
		seed = uint64(time.Now().UnixNano()) ^ 0x9e3779b97f4a7c15
	}
	return seed
}

func buildOpenAIWeightedSelectionOrder(
	candidates []openAIAccountCandidateScore,
	req OpenAIAccountScheduleRequest,
) []openAIAccountCandidateScore {
	if len(candidates) <= 1 {
		return append([]openAIAccountCandidateScore(nil), candidates...)
	}

	pool := append([]openAIAccountCandidateScore(nil), candidates...)
	weights := make([]float64, len(pool))
	minScore := pool[0].score
	for i := 1; i < len(pool); i++ {
		if pool[i].score < minScore {
			minScore = pool[i].score
		}
	}
	for i := range pool {
		// 将 top-K 分值平移到正区间，避免“单一最高分账号”长期垄断。
		weight := (pool[i].score - minScore) + 1.0
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			weight = 1.0
		}
		weights[i] = weight
	}

	order := make([]openAIAccountCandidateScore, 0, len(pool))
	rng := newOpenAISelectionRNG(deriveOpenAISelectionSeed(req))
	for len(pool) > 0 {
		total := 0.0
		for _, w := range weights {
			total += w
		}

		selectedIdx := 0
		if total > 0 {
			r := rng.nextFloat64() * total
			acc := 0.0
			for i, w := range weights {
				acc += w
				if r <= acc {
					selectedIdx = i
					break
				}
			}
		} else {
			selectedIdx = int(rng.nextUint64() % uint64(len(pool)))
		}

		order = append(order, pool[selectedIdx])
		pool = append(pool[:selectedIdx], pool[selectedIdx+1:]...)
		weights = append(weights[:selectedIdx], weights[selectedIdx+1:]...)
	}
	return order
}

type openAIAccountDrainContext struct {
	enabled      bool
	cache        SchedulerCache
	bucket       SchedulerBucket
	hardEligible map[string]map[int64]struct{}
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIAccountDrainContext(req OpenAIAccountScheduleRequest, hardEligible map[string]map[int64]struct{}) openAIAccountDrainContext {
	ctx := openAIAccountDrainContext{}
	if s == nil || s.service == nil || normalizeOpenAICompatiblePlatform(req.Platform) != PlatformOpenAI {
		return ctx
	}
	groupID := int64(0)
	if req.GroupID != nil && *req.GroupID > 0 {
		groupID = *req.GroupID
	}
	if s.service.cfg != nil && s.service.cfg.RunMode == config.RunModeSimple {
		groupID = 0
	}
	ctx.enabled = true
	ctx.cache = s.openAIAccountDrainCache()
	ctx.bucket = SchedulerBucket{GroupID: groupID, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
	ctx.hardEligible = hardEligible
	return ctx
}

func (s *defaultOpenAIAccountScheduler) openAIAccountDrainCache() SchedulerCache {
	if s == nil || s.service == nil || s.service.schedulerSnapshot == nil {
		return nil
	}
	return s.service.schedulerSnapshot.cache
}

func (s *defaultOpenAIAccountScheduler) getOpenAIAccountDrainTarget(ctx context.Context, drain openAIAccountDrainContext, bucket SchedulerDrainTargetBucket) (int64, bool) {
	if !drain.enabled {
		return 0, false
	}
	if drain.cache != nil {
		targetID, ok, err := drain.cache.GetDrainTarget(ctx, bucket)
		if err != nil {
			slog.Debug("openai_drain_target_read_failed", "bucket", bucket.String(), "err", err)
			return 0, false
		}
		return targetID, ok
	}
	if value, ok := s.localDrainTargets.Load(bucket.String()); ok {
		targetID, _ := value.(int64)
		if targetID > 0 {
			return targetID, true
		}
	}
	return 0, false
}

func (s *defaultOpenAIAccountScheduler) setOpenAIAccountDrainTarget(ctx context.Context, bucket SchedulerDrainTargetBucket, expectedID, accountID int64, drain openAIAccountDrainContext) bool {
	if !drain.enabled || accountID <= 0 {
		return true
	}
	if drain.cache != nil {
		if expectedID > 0 {
			ok, err := drain.cache.AdvanceDrainTarget(ctx, bucket, expectedID, accountID)
			if err == nil && ok {
				return true
			}
			if err != nil {
				slog.Debug("openai_drain_target_advance_failed", "bucket", bucket.String(), "expected", expectedID, "account_id", accountID, "err", err)
				return false
			}
			claimed, claimErr := drain.cache.TryClaimDrainTarget(ctx, bucket, accountID)
			if claimErr != nil {
				slog.Debug("openai_drain_target_claim_failed", "bucket", bucket.String(), "account_id", accountID, "err", claimErr)
				return false
			}
			return claimed
		}
		claimed, err := drain.cache.TryClaimDrainTarget(ctx, bucket, accountID)
		if err != nil {
			slog.Debug("openai_drain_target_claim_failed", "bucket", bucket.String(), "account_id", accountID, "err", err)
			return false
		}
		return claimed
	}
	key := bucket.String()
	if expectedID > 0 {
		if s.localDrainTargets.CompareAndSwap(key, expectedID, accountID) {
			return true
		}
		actual, loaded := s.localDrainTargets.LoadOrStore(key, accountID)
		if !loaded {
			return true
		}
		current, _ := actual.(int64)
		return current == accountID
	}
	actual, loaded := s.localDrainTargets.LoadOrStore(key, accountID)
	if !loaded {
		return true
	}
	current, _ := actual.(int64)
	return current == accountID
}

func openAIAccountDrainPoolType(account *Account) string {
	if account != nil && account.Type == AccountTypeAPIKey {
		return AccountTypeAPIKey
	}
	return AccountTypeOAuth
}

func openAIAccountDrainPoolHasNormalAPIKey(candidates []openAIAccountCandidateScore) bool {
	for _, candidate := range candidates {
		if candidate.account != nil &&
			candidate.account.Type == AccountTypeAPIKey &&
			candidate.apiKeyTTFTDemotion != openAIAPIKeyTTFTDemoted {
			return true
		}
	}
	return false
}

func openAIAccountDrainCandidateIDs(candidates []openAIAccountCandidateScore) map[int64]struct{} {
	out := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.account != nil && candidate.account.ID > 0 {
			out[candidate.account.ID] = struct{}{}
		}
	}
	return out
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIAccountLoadPlan(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
) openAIAccountLoadPlan {
	allCandidates := make([]openAIAccountCandidateScore, 0, len(filtered))
	latencyCfg := s.service.openAIAccountLatencyConfig()
	for _, account := range filtered {
		loadInfo := loadMap[account.ID]
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
		}
		errorRate, ttft, hasTTFT := 0.0, 0.0, false
		latencyHealth := openAIAccountLatencyHealthy
		apiKeyTTFTDemotion := openAIAPIKeyTTFTNormal
		apiKeyTTFTProbeDue := false
		if s.stats != nil {
			latency := s.stats.latencySnapshot(account.ID, latencyCfg)
			errorRate, ttft, hasTTFT = latency.errorRate, latency.ttft, latency.hasTTFT
			latencyHealth = latency.health
			apiKeyDemotion := s.stats.apiKeyTTFTDemotionSnapshot(account)
			apiKeyTTFTDemotion = apiKeyDemotion.state
			apiKeyTTFTProbeDue = apiKeyDemotion.probeDue
		}
		allCandidates = append(allCandidates, openAIAccountCandidateScore{
			account:            account,
			loadInfo:           loadInfo,
			errorRate:          errorRate,
			ttft:               ttft,
			hasTTFT:            hasTTFT,
			latencyHealth:      latencyHealth,
			apiKeyTTFTDemotion: apiKeyTTFTDemotion,
			apiKeyTTFTProbeDue: apiKeyTTFTProbeDue,
		})
	}

	candidates := allCandidates
	staleSnapshotCompactRetry := make([]openAIAccountCandidateScore, 0, len(allCandidates))
	if req.RequireCompact {
		candidates = make([]openAIAccountCandidateScore, 0, len(allCandidates))
		for _, candidate := range allCandidates {
			if openAICompactSupportTier(candidate.account) == 0 {
				staleSnapshotCompactRetry = append(staleSnapshotCompactRetry, candidate)
				continue
			}
			candidates = append(candidates, candidate)
		}
	}

	plan := openAIAccountLoadPlan{
		allCandidates:             allCandidates,
		candidates:                candidates,
		staleSnapshotCompactRetry: staleSnapshotCompactRetry,
		candidateCount:            len(candidates),
	}
	if len(candidates) == 0 {
		plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
		return plan
	}

	minPriority, maxPriority := openAIAccountSchedulingPriority(candidates[0].account), openAIAccountSchedulingPriority(candidates[0].account)
	maxWaiting := 1
	loadRateSum := 0.0
	loadRateSumSquares := 0.0
	minTTFT, maxTTFT := 0.0, 0.0
	hasTTFTSample := false
	for i := range candidates {
		candidate := &candidates[i]
		candidate.priority = openAIAccountSchedulingPriority(candidate.account)
		if candidate.priority < minPriority {
			minPriority = candidate.priority
		}
		if candidate.priority > maxPriority {
			maxPriority = candidate.priority
		}
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}
		if candidate.hasTTFT && candidate.ttft > 0 {
			if !hasTTFTSample {
				minTTFT, maxTTFT = candidate.ttft, candidate.ttft
				hasTTFTSample = true
			} else {
				if candidate.ttft < minTTFT {
					minTTFT = candidate.ttft
				}
				if candidate.ttft > maxTTFT {
					maxTTFT = candidate.ttft
				}
			}
		}
		loadRate := float64(candidate.loadInfo.LoadRate)
		loadRateSum += loadRate
		loadRateSumSquares += loadRate * loadRate
	}
	plan.loadSkew = calcLoadSkewByMoments(loadRateSum, loadRateSumSquares, len(candidates))

	weights := s.service.openAIWSSchedulerWeightsForRequest(ctx)

	// Reset 因子（use-it-or-lose-it）：在拥有「未来会话窗口结束时间」的账号中，
	// 剩余时间越短 → 因子越接近 1（越早重置越优先用尽）。无活跃窗口的账号因子为 0。
	// 仅在 weights.Reset > 0 时计算，默认关闭不影响原有行为。
	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	if weights.Reset > 0 {
		now := time.Now()
		for _, candidate := range candidates {
			end := candidate.account.SessionWindowEnd
			if end == nil || !now.Before(*end) {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	now := time.Now()
	healthyCandidates := 0
	degradedCandidates := 0
	severeCandidates := 0
	for i := range candidates {
		item := &candidates[i]
		switch item.latencyHealth {
		case openAIAccountLatencyHealthy:
			healthyCandidates++
		case openAIAccountLatencyDegraded:
			degradedCandidates++
		case openAIAccountLatencySevere:
			severeCandidates++
		}
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(item.priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - clamp01(float64(item.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(item.loadInfo.WaitingCount)/float64(maxWaiting))
		errorFactor := 1 - clamp01(item.errorRate)
		ttftFactor := 0.5
		if item.hasTTFT && hasTTFTSample && maxTTFT > minTTFT {
			ttftFactor = 1 - clamp01((item.ttft-minTTFT)/(maxTTFT-minTTFT))
		}
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end := item.account.SessionWindowEnd; end != nil && now.Before(*end) {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					// 所有有窗口的账号剩余时间相同：一律给满分，让其优于无窗口账号。
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(item.account, now)
		}

		item.score = weights.Priority*priorityFactor +
			weights.Load*loadFactor +
			weights.Queue*queueFactor +
			weights.ErrorRate*errorFactor +
			weights.TTFT*ttftFactor +
			weights.Reset*resetFactor +
			weights.QuotaHeadroom*quotaHeadroomFactor
		if req.StickyWeighted {
			if req.PreviousResponseCanMove && req.StickyPreviousAccountID > 0 && item.account.ID == req.StickyPreviousAccountID {
				item.score += weights.Previous
			}
			if req.StickyAccountID > 0 && item.account.ID == req.StickyAccountID {
				item.score += weights.SessionSticky
			}
		}
	}
	if healthyCandidates == 0 && (degradedCandidates > 0 || severeCandidates > 0) {
		slog.Debug("openai_scheduler_all_candidates_latency_degraded",
			"candidate_count", len(candidates),
			"degraded_count", degradedCandidates,
			"severe_count", severeCandidates,
		)
	}
	plan.candidates = candidates

	plan.topK = s.service.openAIWSLBTopKForRequest(ctx)
	if plan.topK > len(candidates) {
		plan.topK = len(candidates)
	}
	if plan.topK <= 0 {
		plan.topK = 1
	}

	plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
	return plan
}

func (s *defaultOpenAIAccountScheduler) buildOpenAISelectionOrder(
	req OpenAIAccountScheduleRequest,
	plan openAIAccountLoadPlan,
) []openAIAccountCandidateScore {
	buildSelectionOrder := func(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
		if len(pool) == 0 || plan.topK <= 0 {
			return nil
		}
		groupTopK := plan.topK
		if groupTopK > len(pool) {
			groupTopK = len(pool)
		}
		ranked := selectTopKOpenAICandidates(pool, groupTopK)
		if req.StickyWeighted {
			for _, stickyID := range []int64{req.StickyPreviousAccountID, req.StickyAccountID} {
				if stickyID <= 0 {
					continue
				}
				for i, candidate := range ranked {
					if candidate.account != nil && candidate.account.ID == stickyID {
						ordered := append([]openAIAccountCandidateScore{candidate}, ranked[:i]...)
						ordered = append(ordered, ranked[i+1:]...)
						return ordered
					}
				}
			}
		}
		return buildOpenAIWeightedSelectionOrder(ranked, req)
	}

	if req.RequireCompact {
		supported := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		unknown := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		for _, candidate := range plan.candidates {
			switch openAICompactSupportTier(candidate.account) {
			case 2:
				supported = append(supported, candidate)
			case 1:
				unknown = append(unknown, candidate)
			}
		}
		selectionOrder := make([]openAIAccountCandidateScore, 0, len(plan.allCandidates))
		selectionOrder = append(selectionOrder, buildSelectionOrder(supported)...)
		selectionOrder = append(selectionOrder, buildSelectionOrder(unknown)...)
		if len(plan.staleSnapshotCompactRetry) > 0 && s.service.schedulerSnapshot != nil {
			selectionOrder = append(selectionOrder, sortOpenAICompactRetryCandidates(plan.staleSnapshotCompactRetry)...)
		}
		return selectionOrder
	}

	return buildSelectionOrder(plan.candidates)
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIDrainSelectionOrder(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	plan openAIAccountLoadPlan,
	drain openAIAccountDrainContext,
) []openAIAccountCandidateScore {
	if !drain.enabled || req.StickyWeighted {
		return plan.selectionOrder
	}
	baseOrder := plan.candidates
	if req.RequireCompact {
		supported := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		unknown := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		for _, candidate := range plan.candidates {
			switch openAICompactSupportTier(candidate.account) {
			case 2:
				supported = append(supported, candidate)
			case 1:
				unknown = append(unknown, candidate)
			}
		}
		baseOrder = make([]openAIAccountCandidateScore, 0, len(plan.allCandidates))
		baseOrder = append(baseOrder, supported...)
		baseOrder = append(baseOrder, unknown...)
		if len(plan.staleSnapshotCompactRetry) > 0 && s.service.schedulerSnapshot != nil {
			baseOrder = append(baseOrder, plan.staleSnapshotCompactRetry...)
		}
	}
	if len(baseOrder) == 0 {
		return baseOrder
	}

	oauthCandidates := make([]openAIAccountCandidateScore, 0, len(baseOrder))
	apiKeyCandidates := make([]openAIAccountCandidateScore, 0, len(baseOrder))
	for _, candidate := range baseOrder {
		if candidate.account == nil {
			continue
		}
		if candidate.account.Type == AccountTypeAPIKey {
			apiKeyCandidates = append(apiKeyCandidates, candidate)
			continue
		}
		oauthCandidates = append(oauthCandidates, candidate)
	}

	oauthOrder := s.buildOpenAIDrainPoolOrder(ctx, drain, AccountTypeOAuth, oauthCandidates)
	apiKeyOrder := s.buildOpenAIDrainPoolOrder(ctx, drain, AccountTypeAPIKey, apiKeyCandidates)
	if len(oauthOrder) == 0 {
		return apiKeyOrder
	}
	out := make([]openAIAccountCandidateScore, 0, len(oauthOrder)+len(apiKeyOrder))
	out = append(out, oauthOrder...)
	out = append(out, apiKeyOrder...)
	return out
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIDrainPoolOrder(
	ctx context.Context,
	drain openAIAccountDrainContext,
	accountType string,
	candidates []openAIAccountCandidateScore,
) []openAIAccountCandidateScore {
	if len(candidates) == 0 {
		return nil
	}
	var ordered []openAIAccountCandidateScore
	if accountType == AccountTypeAPIKey {
		ordered = sortOpenAIAPIKeyDrainSelectionOrder(candidates)
	} else {
		ordered = sortOpenAILatencyAwareSelectionOrder(candidates)
	}

	bucket := NewSchedulerDrainTargetBucket(drain.bucket, accountType)
	targetID, hasTarget := s.getOpenAIAccountDrainTarget(ctx, drain, bucket)
	if !hasTarget || targetID <= 0 {
		return markOpenAIDrainCandidates(ordered, bucket, 0, 0, false)
	}

	candidateIDs := openAIAccountDrainCandidateIDs(ordered)
	if _, ok := candidateIDs[targetID]; !ok {
		if !openAIAccountDrainHardEligible(drain, accountType, targetID) {
			return markOpenAIDrainCandidates(ordered, bucket, 0, targetID, false)
		}
		return markOpenAIDrainCandidates(ordered, bucket, 0, 0, true)
	}

	target := openAIAccountDrainCandidateByID(ordered, targetID)
	if accountType == AccountTypeAPIKey &&
		target != nil &&
		target.apiKeyTTFTDemotion == openAIAPIKeyTTFTDemoted &&
		openAIAccountDrainPoolHasNormalAPIKey(ordered) {
		return markOpenAIDrainCandidates(sortOpenAIAPIKeyDrainReplacementOrder(ordered), bucket, targetID, targetID, false)
	}

	out := make([]openAIAccountCandidateScore, 0, len(ordered))
	if accountType == AccountTypeAPIKey {
		for _, candidate := range ordered {
			if candidate.account == nil || !candidate.apiKeyTTFTProbeDue || candidate.account.ID == targetID {
				continue
			}
			candidate.drainBucket = bucket
			out = append(out, candidate)
		}
	}
	for _, candidate := range ordered {
		if candidate.account != nil && candidate.account.ID == targetID {
			candidate.drainBucket = bucket
			out = append(out, candidate)
			break
		}
	}
	for _, candidate := range ordered {
		if candidate.account != nil && candidate.account.ID == targetID {
			continue
		}
		if accountType == AccountTypeAPIKey && candidate.apiKeyTTFTProbeDue {
			continue
		}
		candidate.drainBucket = bucket
		out = append(out, candidate)
	}
	return out
}

func markOpenAIDrainCandidates(
	candidates []openAIAccountCandidateScore,
	bucket SchedulerDrainTargetBucket,
	demotedTargetID int64,
	replaceTargetID int64,
	claimBlocked bool,
) []openAIAccountCandidateScore {
	out := append([]openAIAccountCandidateScore(nil), candidates...)
	for i := range out {
		out[i].drainBucket = bucket
		out[i].drainClaimBlocked = claimBlocked
		out[i].drainCanClaim = !claimBlocked && replaceTargetID == 0 && demotedTargetID == 0
		if out[i].apiKeyTTFTProbeDue {
			out[i].drainCanClaim = false
			continue
		}
		if replaceTargetID > 0 {
			out[i].drainCanClaim = false
			if demotedTargetID > 0 && out[i].account != nil && out[i].account.Type == AccountTypeAPIKey && out[i].apiKeyTTFTDemotion == openAIAPIKeyTTFTDemoted {
				continue
			}
			out[i].drainReplaceTarget = replaceTargetID
		}
	}
	return out
}

func openAIAccountDrainCandidateByID(candidates []openAIAccountCandidateScore, accountID int64) *openAIAccountCandidateScore {
	for i := range candidates {
		if candidates[i].account != nil && candidates[i].account.ID == accountID {
			return &candidates[i]
		}
	}
	return nil
}

func openAIAccountDrainHardEligible(drain openAIAccountDrainContext, accountType string, accountID int64) bool {
	if accountID <= 0 || drain.hardEligible == nil {
		return false
	}
	ids := drain.hardEligible[accountType]
	if len(ids) == 0 {
		return false
	}
	_, ok := ids[accountID]
	return ok
}

func sortOpenAILatencyAwareSelectionOrder(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	ordered := append([]openAIAccountCandidateScore(nil), pool...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.latencyHealth != b.latencyHealth {
			return a.latencyHealth < b.latencyHealth
		}
		if a.latencyHealth != openAIAccountLatencyHealthy || b.latencyHealth != openAIAccountLatencyHealthy {
			if math.Abs(a.errorRate-b.errorRate) > 0.01 {
				return a.errorRate < b.errorRate
			}
			if a.hasTTFT && b.hasTTFT && math.Abs(a.ttft-b.ttft) > 250 {
				return a.ttft < b.ttft
			}
			if a.loadInfo != nil && b.loadInfo != nil {
				if a.loadInfo.LoadRate != b.loadInfo.LoadRate {
					return a.loadInfo.LoadRate < b.loadInfo.LoadRate
				}
				if a.loadInfo.WaitingCount != b.loadInfo.WaitingCount {
					return a.loadInfo.WaitingCount < b.loadInfo.WaitingCount
				}
			}
			if a.score != b.score {
				return a.score > b.score
			}
		}
		return dedicatedAccountLess(a.account, b.account)
	})
	return ordered
}

func sortOpenAIAPIKeyDrainSelectionOrder(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	probe := make([]openAIAccountCandidateScore, 0, len(pool))
	normal := make([]openAIAccountCandidateScore, 0, len(pool))
	demoted := make([]openAIAccountCandidateScore, 0, len(pool))
	for _, candidate := range pool {
		if candidate.apiKeyTTFTProbeDue {
			probe = append(probe, candidate)
			continue
		}
		if candidate.apiKeyTTFTDemotion == openAIAPIKeyTTFTDemoted {
			demoted = append(demoted, candidate)
			continue
		}
		normal = append(normal, candidate)
	}
	ordered := make([]openAIAccountCandidateScore, 0, len(pool))
	ordered = append(ordered, sortOpenAILatencyAwareSelectionOrder(probe)...)
	ordered = append(ordered, sortOpenAILatencyAwareSelectionOrder(normal)...)
	ordered = append(ordered, sortOpenAILatencyAwareSelectionOrder(demoted)...)
	return ordered
}

func sortOpenAIAPIKeyDrainReplacementOrder(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	normal := make([]openAIAccountCandidateScore, 0, len(pool))
	demoted := make([]openAIAccountCandidateScore, 0, len(pool))
	for _, candidate := range pool {
		if candidate.apiKeyTTFTDemotion == openAIAPIKeyTTFTDemoted {
			demoted = append(demoted, candidate)
			continue
		}
		normal = append(normal, candidate)
	}
	ordered := make([]openAIAccountCandidateScore, 0, len(pool))
	ordered = append(ordered, sortOpenAILatencyAwareSelectionOrder(normal)...)
	ordered = append(ordered, sortOpenAILatencyAwareSelectionOrder(demoted)...)
	return ordered
}

func sortOpenAIDedicatedSelectionOrder(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	ordered := append([]openAIAccountCandidateScore(nil), pool...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return dedicatedAccountLess(ordered[i].account, ordered[j].account)
	})
	return ordered
}

func sortOpenAICompactRetryCandidates(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
	if len(pool) == 0 {
		return nil
	}
	ordered := append([]openAIAccountCandidateScore(nil), pool...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.account.Priority != b.account.Priority {
			return a.account.Priority < b.account.Priority
		}
		if a.loadInfo.LoadRate != b.loadInfo.LoadRate {
			return a.loadInfo.LoadRate < b.loadInfo.LoadRate
		}
		if a.loadInfo.WaitingCount != b.loadInfo.WaitingCount {
			return a.loadInfo.WaitingCount < b.loadInfo.WaitingCount
		}
		switch {
		case a.account.LastUsedAt == nil && b.account.LastUsedAt != nil:
			return true
		case a.account.LastUsedAt != nil && b.account.LastUsedAt == nil:
			return false
		case a.account.LastUsedAt == nil && b.account.LastUsedAt == nil:
			return false
		default:
			return a.account.LastUsedAt.Before(*b.account.LastUsedAt)
		}
	})
	return ordered
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAISelectionOrder(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	selectionOrder []openAIAccountCandidateScore,
	drain openAIAccountDrainContext,
) (*AccountSelectionResult, bool, error) {
	compactBlocked := false
	for i := 0; i < len(selectionOrder); i++ {
		candidate := selectionOrder[i]
		fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			continue
		}
		fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
			compactBlocked = true
			continue
		}
		result, acquireErr := s.service.tryAcquireAccountSlot(ctx, fresh.ID, fresh.Concurrency)
		if acquireErr != nil {
			return nil, compactBlocked, acquireErr
		}
		if result != nil && result.Acquired {
			if !s.commitOpenAIAccountSelectionAfterAcquire(ctx, candidate, fresh.ID, drain) {
				if result.ReleaseFunc != nil {
					result.ReleaseFunc()
				}
				continue
			}
			if req.SessionHash != "" && !req.PreserveStickyBinding {
				_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, fresh.ID)
			}
			return &AccountSelectionResult{
				Account:     fresh,
				Acquired:    true,
				ReleaseFunc: result.ReleaseFunc,
			}, compactBlocked, nil
		}
	}
	return nil, compactBlocked, nil
}

func (s *defaultOpenAIAccountScheduler) commitOpenAIAccountSelectionAfterAcquire(
	ctx context.Context,
	candidate openAIAccountCandidateScore,
	accountID int64,
	drain openAIAccountDrainContext,
) bool {
	if !s.commitOpenAIAccountDrainSelection(ctx, candidate, accountID, drain) {
		return false
	}
	if candidate.apiKeyTTFTProbeDue && s != nil && s.stats != nil {
		s.stats.recordAPIKeyTTFTDemotionProbe(accountID)
	}
	return true
}

func (s *defaultOpenAIAccountScheduler) commitOpenAIAccountDrainSelection(
	ctx context.Context,
	candidate openAIAccountCandidateScore,
	accountID int64,
	drain openAIAccountDrainContext,
) bool {
	if !drain.enabled || candidate.drainClaimBlocked || accountID <= 0 || candidate.drainBucket.AccountType == "" {
		return true
	}
	if candidate.drainReplaceTarget > 0 && candidate.drainReplaceTarget != accountID {
		return s.setOpenAIAccountDrainTarget(ctx, candidate.drainBucket, candidate.drainReplaceTarget, accountID, drain)
	}
	if candidate.drainCanClaim {
		return s.setOpenAIAccountDrainTarget(ctx, candidate.drainBucket, 0, accountID, drain)
	}
	return true
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIDrainWaitCommit(
	candidate openAIAccountCandidateScore,
	accountID int64,
	drain openAIAccountDrainContext,
) func(context.Context) bool {
	if accountID <= 0 {
		return nil
	}
	needsDrainCommit := drain.enabled && !candidate.drainClaimBlocked && candidate.drainBucket.AccountType != "" &&
		(candidate.drainReplaceTarget > 0 || candidate.drainCanClaim)
	if !needsDrainCommit && !candidate.apiKeyTTFTProbeDue {
		return nil
	}
	return func(ctx context.Context) bool {
		return s.commitOpenAIAccountSelectionAfterAcquire(ctx, candidate, accountID, drain)
	}
}

func (s *defaultOpenAIAccountScheduler) tryFallbackToWeightedSticky(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, error) {
	if !req.StickyWeighted {
		return nil, nil
	}
	for _, accountID := range []int64{req.StickyPreviousAccountID, req.StickyAccountID} {
		if accountID <= 0 {
			continue
		}
		if req.ExcludedIDs != nil {
			if _, excluded := req.ExcludedIDs[accountID]; excluded {
				continue
			}
		}
		account, err := s.service.getSchedulableAccount(ctx, accountID)
		if err != nil || account == nil {
			continue
		}
		if !s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		account = s.service.recheckSelectedOpenAIAccountFromDB(ctx, account, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
		if account == nil || !s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		// 粘性绑定只证明绑定时账号在分组内；账号被移出分组后绑定仍会在 TTL 内存活，
		// 必须与 selectBySessionHash 一样重验分组归属，否则会把分组流量泄漏到组外账号。
		if !openAIStickyAccountMatchesGroup(account, req.GroupID) {
			if accountID == req.StickyAccountID && strings.TrimSpace(req.SessionHash) != "" {
				_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, req.SessionHash)
			}
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(account) == 0 {
			continue
		}
		result, acquireErr := s.service.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if result != nil && result.Acquired {
			if req.SessionHash != "" && !req.PreserveStickyBinding {
				_ = s.service.BindStickySession(ctx, req.GroupID, req.SessionHash, account.ID)
			}
			return &AccountSelectionResult{
				Account:     account,
				Acquired:    true,
				ReleaseFunc: result.ReleaseFunc,
			}, nil
		}
		if s.service.concurrencyService != nil {
			cfg := s.service.schedulingConfig()
			return &AccountSelectionResult{
				Account: account,
				WaitPlan: &AccountWaitPlan{
					AccountID:      account.ID,
					MaxConcurrency: account.Concurrency,
					Timeout:        cfg.StickySessionWaitTimeout,
					MaxWaiting:     cfg.StickySessionMaxWaiting,
				},
			}, nil
		}
	}
	return nil, nil
}

func (s *defaultOpenAIAccountScheduler) selectByLoadBalance(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, int, int, float64, error) {
	accounts, err := s.service.listSchedulableAccounts(ctx, req.GroupID, req.Platform)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if len(accounts) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false)
	}

	// require_privacy_set: 获取分组信息
	var schedGroup *Group
	if req.GroupID != nil && s.service.schedulerSnapshot != nil {
		schedGroup, _ = s.service.schedulerSnapshot.GetGroupByID(ctx, *req.GroupID)
	}

	filtered := make([]*Account, 0, len(accounts))
	loadReq := make([]AccountWithConcurrency, 0, len(accounts))
	hardEligible := make(map[string]map[int64]struct{}, 2)
	for i := range accounts {
		account := &accounts[i]
		if !account.IsSchedulable() || account.Platform != normalizeOpenAICompatiblePlatform(req.Platform) || !account.IsOpenAICompatible() {
			continue
		}
		if s.service.isOpenAIAccountRuntimeBlocked(account) {
			continue
		}
		// require_privacy_set: 跳过 privacy 未设置的账号并标记异常
		if schedGroup != nil && schedGroup.RequirePrivacySet && !account.IsPrivacySet() {
			s.service.BlockAccountScheduling(account, time.Time{}, "privacy_not_set")
			_ = s.service.accountRepo.SetError(ctx, account.ID,
				fmt.Sprintf("Privacy not set, required by group [%s]", schedGroup.Name))
			continue
		}
		if paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
			continue
		}
		if !parentHealthyForShadow(account, func(id int64) *Account {
			return s.lookupShadowParentAccount(ctx, id)
		}) {
			continue
		}
		poolType := openAIAccountDrainPoolType(account)
		if hardEligible[poolType] == nil {
			hardEligible[poolType] = make(map[int64]struct{})
		}
		hardEligible[poolType][account.ID] = struct{}{}
		if req.ExcludedIDs != nil {
			if _, excluded := req.ExcludedIDs[account.ID]; excluded {
				continue
			}
		}
		if !s.isAccountRequestCompatible(ctx, account, req) {
			continue
		}
		if !s.isAccountTransportCompatible(account, req.RequiredTransport) {
			continue
		}
		filtered = append(filtered, account)
		loadReq = append(loadReq, AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	if len(filtered) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false)
	}

	loadMap := map[int64]*AccountLoadInfo{}
	if s.service.concurrencyService != nil {
		if batchLoad, loadErr := s.service.concurrencyService.GetAccountsLoadBatch(ctx, loadReq); loadErr == nil {
			loadMap = batchLoad
		}
	}

	drain := s.buildOpenAIAccountDrainContext(req, hardEligible)
	if req.SubscriptionPriority {
		subscriptionAccounts, regularAccounts := partitionOpenAIChatGPTSubscriptionAccounts(filtered)
		if len(subscriptionAccounts) > 0 {
			attempt := s.trySelectByLoadBalancePool(ctx, req, subscriptionAccounts, loadMap, drain)
			if attempt.err != nil && (!attempt.noCompactCandidates || len(regularAccounts) <= 0) {
				return nil, attempt.candidateCount, attempt.topK, attempt.loadSkew, attempt.err
			}
			if attempt.result != nil {
				return attempt.result, attempt.candidateCount, attempt.topK, attempt.loadSkew, nil
			}
			if len(regularAccounts) > 0 {
				regularAttempt := s.trySelectByLoadBalancePool(ctx, req, regularAccounts, loadMap, drain)
				if regularAttempt.err != nil && !regularAttempt.noCompactCandidates {
					return nil, regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew, regularAttempt.err
				}
				if regularAttempt.result != nil {
					return regularAttempt.result, regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew, nil
				}
				var result *AccountSelectionResult
				candidateCount, topK, loadSkew := regularAttempt.candidateCount, regularAttempt.topK, regularAttempt.loadSkew
				fallbackErr := regularAttempt.err
				if regularAttempt.err == nil {
					result, candidateCount, topK, loadSkew, fallbackErr = s.finishLoadBalanceSelectionFallback(ctx, req, regularAttempt)
					if fallbackErr == nil && result != nil {
						return result, candidateCount, topK, loadSkew, nil
					}
				}
				// 常规池既无法获取也无法排队（含仅剩不支持 compact 的候选）时，
				// 回退到订阅池的等待计划：busy-but-waitable 的订阅账号不应因常规池存在
				// 而被丢弃，否则开启订阅优先反而让本可排队成功的请求硬失败。
				subResult, subCandidateCount, subTopK, subLoadSkew, subErr := s.finishLoadBalanceSelectionFallback(ctx, req, attempt)
				if subErr == nil && subResult != nil {
					return subResult, subCandidateCount, subTopK, subLoadSkew, nil
				}
				return result, candidateCount, topK, loadSkew, fallbackErr
			}
			return s.finishLoadBalanceSelectionFallback(ctx, req, attempt)
		}
	}

	attempt := s.trySelectByLoadBalancePool(ctx, req, filtered, loadMap, drain)
	if attempt.err != nil {
		return nil, attempt.candidateCount, attempt.topK, attempt.loadSkew, attempt.err
	}
	if attempt.result != nil {
		return attempt.result, attempt.candidateCount, attempt.topK, attempt.loadSkew, nil
	}
	return s.finishLoadBalanceSelectionFallback(ctx, req, attempt)
}

func partitionOpenAIChatGPTSubscriptionAccounts(accounts []*Account) ([]*Account, []*Account) {
	subscriptionAccounts := make([]*Account, 0, len(accounts))
	regularAccounts := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil && account.IsOpenAIChatGPTSubscription() {
			subscriptionAccounts = append(subscriptionAccounts, account)
			continue
		}
		regularAccounts = append(regularAccounts, account)
	}
	return subscriptionAccounts, regularAccounts
}

func (s *defaultOpenAIAccountScheduler) trySelectByLoadBalancePool(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
	drain openAIAccountDrainContext,
) openAIAccountLoadSelectionAttempt {
	plan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, loadMap)
	selectionOrder := s.buildOpenAIDrainSelectionOrder(ctx, req, plan, drain)
	topK := plan.topK
	if drain.enabled && !req.StickyWeighted {
		topK = len(selectionOrder)
	}
	attempt := openAIAccountLoadSelectionAttempt{
		selectionOrder: selectionOrder,
		drain:          drain,
		candidateCount: plan.candidateCount,
		topK:           topK,
		loadSkew:       plan.loadSkew,
	}
	if req.RequireCompact && len(plan.candidates) == 0 && len(plan.staleSnapshotCompactRetry) == 0 {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if req.RequireCompact && len(selectionOrder) == 0 && s.service.schedulerSnapshot == nil {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if len(selectionOrder) == 0 {
		attempt.compactBlocked = req.RequireCompact && len(plan.allCandidates) > 0
		return attempt
	}

	result, compactBlocked, acquireErr := s.tryAcquireOpenAISelectionOrder(ctx, req, selectionOrder, drain)
	attempt.compactBlocked = compactBlocked
	if acquireErr != nil {
		attempt.err = acquireErr
		return attempt
	}
	if result != nil {
		attempt.result = result
		return attempt
	}

	if s.service.concurrencyService != nil {
		loadReq := buildOpenAIAccountLoadRequest(filtered)
		if freshLoadMap, loadErr := s.service.concurrencyService.GetAccountsLoadBatchFresh(ctx, loadReq); loadErr == nil {
			freshPlan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, freshLoadMap)
			freshSelectionOrder := s.buildOpenAIDrainSelectionOrder(ctx, req, freshPlan, drain)
			if len(freshSelectionOrder) > 0 {
				freshTopK := freshPlan.topK
				if drain.enabled && !req.StickyWeighted {
					freshTopK = len(freshSelectionOrder)
				}
				freshResult, freshCompactBlocked, freshAcquireErr := s.tryAcquireOpenAISelectionOrder(ctx, req, freshSelectionOrder, drain)
				if freshAcquireErr != nil {
					attempt.err = freshAcquireErr
					return attempt
				}
				if freshResult != nil {
					attempt.result = freshResult
					attempt.selectionOrder = freshSelectionOrder
					attempt.candidateCount = freshPlan.candidateCount
					attempt.topK = freshTopK
					attempt.loadSkew = freshPlan.loadSkew
					return attempt
				}
				attempt.compactBlocked = attempt.compactBlocked || freshCompactBlocked
				attempt.selectionOrder = freshSelectionOrder
				attempt.candidateCount = freshPlan.candidateCount
				attempt.topK = freshTopK
				attempt.loadSkew = freshPlan.loadSkew
			}
		}
	}

	return attempt
}

func buildOpenAIAccountLoadRequest(accounts []*Account) []AccountWithConcurrency {
	loadReq := make([]AccountWithConcurrency, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadReq = append(loadReq, AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	return loadReq
}

func (s *defaultOpenAIAccountScheduler) finishLoadBalanceSelectionFallback(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	attempt openAIAccountLoadSelectionAttempt,
) (*AccountSelectionResult, int, int, float64, error) {
	candidateCount := attempt.candidateCount
	topK := attempt.topK
	loadSkew := attempt.loadSkew

	if len(attempt.selectionOrder) == 0 {
		return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, attempt.compactBlocked)
	}

	if stickyFallback, stickyErr := s.tryFallbackToWeightedSticky(ctx, req); stickyErr != nil {
		return nil, candidateCount, topK, loadSkew, stickyErr
	} else if stickyFallback != nil {
		return stickyFallback, candidateCount, topK, loadSkew, nil
	}

	cfg := s.service.schedulingConfig()
	compactBlocked := attempt.compactBlocked
	// WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待。
	for _, candidate := range attempt.selectionOrder {
		fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			continue
		}
		fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(fresh, req.RequiredTransport) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
			compactBlocked = true
			continue
		}
		return &AccountSelectionResult{
			Account: fresh,
			WaitPlan: &AccountWaitPlan{
				AccountID:          fresh.ID,
				MaxConcurrency:     fresh.Concurrency,
				Timeout:            cfg.FallbackWaitTimeout,
				MaxWaiting:         cfg.FallbackMaxWaiting,
				CommitAfterAcquire: s.buildOpenAIDrainWaitCommit(candidate, fresh.ID, attempt.drain),
			},
		}, candidateCount, topK, loadSkew, nil
	}

	return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, compactBlocked)
}

func (s *defaultOpenAIAccountScheduler) isAccountTransportCompatible(account *Account, requiredTransport OpenAIUpstreamTransport) bool {
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || s.service == nil {
		return false
	}
	return s.service.isOpenAIAccountTransportCompatible(account, requiredTransport)
}

func (s *defaultOpenAIAccountScheduler) lookupShadowParentAccount(ctx context.Context, id int64) *Account {
	if s == nil || s.service == nil {
		return nil
	}
	if s.service.schedulerSnapshot != nil {
		if account, err := s.service.schedulerSnapshot.GetAccount(ctx, id); err == nil && account != nil {
			return account
		}
	}
	if s.service.accountRepo == nil {
		return nil
	}
	account, _ := s.service.accountRepo.GetByID(ctx, id)
	return account
}

func (s *defaultOpenAIAccountScheduler) isAccountRequestCompatible(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) bool {
	if account == nil {
		return false
	}
	if s != nil && s.service != nil && s.service.isOpenAIAccountRuntimeBlocked(account) {
		return false
	}
	// Quota auto-pause must be evaluated during the initial filter too. Without it the
	// TopK candidate pool can be filled with paused accounts and the later fresh/DB
	// rechecks won't reach healthy accounts that fell outside TopK — manifesting as
	// "no available accounts" even though healthy ones exist.
	if paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
		return false
	}
	// 母账号健康联动：影子账号的凭据来自母账号，母账号不可调度时影子也不应被选中。
	// Parent-health gate: shadow borrows the parent's credentials; an unschedulable
	// parent must block the shadow across all scheduler paths.
	if !parentHealthyForShadow(account, func(id int64) *Account {
		return s.lookupShadowParentAccount(ctx, id)
	}) {
		return false
	}
	if req.RequestedModel != "" && !account.IsModelSupported(req.RequestedModel) {
		return false
	}
	if req.GroupID != nil && s != nil && s.service != nil &&
		s.service.needsUpstreamChannelRestrictionCheck(ctx, req.GroupID) &&
		s.service.isUpstreamModelRestrictedByChannel(ctx, *req.GroupID, account, req.RequestedModel, req.RequireCompact) {
		return false
	}
	return accountSupportsOpenAICapabilities(account, req.RequiredCapability, req.RequiredImageCapability)
}

func (s *defaultOpenAIAccountScheduler) ReportResult(accountID int64, success bool, firstTokenMs *int) {
	if s == nil || s.stats == nil {
		return
	}
	s.stats.report(accountID, success, firstTokenMs)
}

func (s *defaultOpenAIAccountScheduler) ReportSwitch() {
	if s == nil {
		return
	}
	s.metrics.recordSwitch()
}

func (s *defaultOpenAIAccountScheduler) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	if s == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}

	selectTotal := s.metrics.selectTotal.Load()
	prevHit := s.metrics.stickyPreviousHitTotal.Load()
	sessionHit := s.metrics.stickySessionHitTotal.Load()
	switchTotal := s.metrics.accountSwitchTotal.Load()
	latencyTotal := s.metrics.latencyMsTotal.Load()
	loadSkewTotal := s.metrics.loadSkewMilliTotal.Load()

	snapshot := OpenAIAccountSchedulerMetricsSnapshot{
		SelectTotal:              selectTotal,
		StickyPreviousHitTotal:   prevHit,
		StickySessionHitTotal:    sessionHit,
		LoadBalanceSelectTotal:   s.metrics.loadBalanceSelectTotal.Load(),
		AccountSwitchTotal:       switchTotal,
		SchedulerLatencyMsTotal:  latencyTotal,
		RuntimeStatsAccountCount: s.stats.size(),
	}
	if selectTotal > 0 {
		snapshot.SchedulerLatencyMsAvg = float64(latencyTotal) / float64(selectTotal)
		snapshot.StickyHitRatio = float64(prevHit+sessionHit) / float64(selectTotal)
		snapshot.AccountSwitchRate = float64(switchTotal) / float64(selectTotal)
		snapshot.LoadSkewAvg = float64(loadSkewTotal) / 1000 / float64(selectTotal)
	}
	return snapshot
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerSettingRepo() SettingRepository {
	if s == nil || s.rateLimitService == nil || s.rateLimitService.settingService == nil {
		return nil
	}
	return s.rateLimitService.settingService.settingRepo
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerRuntimeSettings(ctx context.Context) openAIAdvancedSchedulerRuntimeSettings {
	if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return openAIAdvancedSchedulerRuntimeSettings{
				enabled:                     cached.enabled,
				stickyWeightedEnabled:       cached.stickyWeightedEnabled,
				subscriptionPriorityEnabled: cached.subscriptionPriorityEnabled,
				lbTopKOverride:              cached.lbTopKOverride,
				weightOverrides:             cloneOpenAIAdvancedSchedulerWeightOverrides(cached.weightOverrides),
			}
		}
	}

	result, _, _ := openAIAdvancedSchedulerSettingSF.Do(openAIAdvancedSchedulerSettingKey, func() (any, error) {
		if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return openAIAdvancedSchedulerRuntimeSettings{
					enabled:                     cached.enabled,
					stickyWeightedEnabled:       cached.stickyWeightedEnabled,
					subscriptionPriorityEnabled: cached.subscriptionPriorityEnabled,
					lbTopKOverride:              cached.lbTopKOverride,
					weightOverrides:             cloneOpenAIAdvancedSchedulerWeightOverrides(cached.weightOverrides),
				}, nil
			}
		}

		enabled := false
		stickyWeightedEnabled := false
		subscriptionPriorityEnabled := false
		lbTopKOverride := 0
		weightOverrides := map[string]float64{}
		if repo := s.openAIAdvancedSchedulerSettingRepo(); repo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIAdvancedSchedulerSettingDBTimeout)
			defer cancel()

			if values, err := repo.GetMultiple(dbCtx, openAIAdvancedSchedulerRuntimeSettingKeys()); err == nil {
				enabled = strings.EqualFold(strings.TrimSpace(values[openAIAdvancedSchedulerSettingKey]), "true")
				stickyWeightedEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled]), "true")
				subscriptionPriorityEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled]), "true")
				lbTopKOverride = parsePositiveIntOverride(values[SettingKeyOpenAIAdvancedSchedulerLBTopK])
				weightOverrides = parseOpenAIAdvancedSchedulerWeightOverrides(values)
			} else {
				// 批量读取失败时逐键降级，覆盖全部键（含 TopK/权重），避免只加载布尔开关
				// 而静默丢弃管理员配置的覆盖值；降级状态会被缓存一个 TTL，必须留痕。
				slog.Warn("openai_advanced_scheduler_settings_batch_load_failed", "error", err)
				fallbackValues := make(map[string]string)
				for _, key := range openAIAdvancedSchedulerRuntimeSettingKeys() {
					if value, valueErr := repo.GetValue(dbCtx, key); valueErr == nil {
						fallbackValues[key] = value
					}
				}
				enabled = strings.EqualFold(strings.TrimSpace(fallbackValues[openAIAdvancedSchedulerSettingKey]), "true")
				stickyWeightedEnabled = strings.EqualFold(strings.TrimSpace(fallbackValues[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled]), "true")
				subscriptionPriorityEnabled = strings.EqualFold(strings.TrimSpace(fallbackValues[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled]), "true")
				lbTopKOverride = parsePositiveIntOverride(fallbackValues[SettingKeyOpenAIAdvancedSchedulerLBTopK])
				weightOverrides = parseOpenAIAdvancedSchedulerWeightOverrides(fallbackValues)
			}
		}

		openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
			enabled:                     enabled,
			stickyWeightedEnabled:       stickyWeightedEnabled,
			subscriptionPriorityEnabled: subscriptionPriorityEnabled,
			lbTopKOverride:              lbTopKOverride,
			weightOverrides:             cloneOpenAIAdvancedSchedulerWeightOverrides(weightOverrides),
			expiresAt:                   time.Now().Add(openAIAdvancedSchedulerSettingCacheTTL).UnixNano(),
		})
		return openAIAdvancedSchedulerRuntimeSettings{
			enabled:                     enabled,
			stickyWeightedEnabled:       stickyWeightedEnabled,
			subscriptionPriorityEnabled: subscriptionPriorityEnabled,
			lbTopKOverride:              lbTopKOverride,
			weightOverrides:             weightOverrides,
		}, nil
	})

	settings, _ := result.(openAIAdvancedSchedulerRuntimeSettings)
	return settings
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerEnabled(ctx context.Context) bool {
	return s.openAIAdvancedSchedulerRuntimeSettings(ctx).enabled
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return settings.enabled && settings.stickyWeightedEnabled
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return settings.enabled && settings.subscriptionPriorityEnabled
}

func openAIAdvancedSchedulerRuntimeSettingKeys() []string {
	keys := []string{
		openAIAdvancedSchedulerSettingKey,
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled,
		SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled,
		SettingKeyOpenAIAdvancedSchedulerLBTopK,
	}
	for _, spec := range openAIAdvancedSchedulerWeightOverrideSpecs() {
		keys = append(keys, spec.key)
	}
	return keys
}

type openAIAdvancedSchedulerWeightOverrideSpec struct {
	key  string
	name string
}

func openAIAdvancedSchedulerWeightOverrideSpecs() []openAIAdvancedSchedulerWeightOverrideSpec {
	return []openAIAdvancedSchedulerWeightOverrideSpec{
		{key: SettingKeyOpenAIAdvancedSchedulerWeightPriority, name: "priority"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightLoad, name: "load"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightQueue, name: "queue"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightErrorRate, name: "error_rate"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightTTFT, name: "ttft"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightReset, name: "reset"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom, name: "quota_headroom"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse, name: "previous_response"},
		{key: SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky, name: "session_sticky"},
	}
}

func parsePositiveIntOverride(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func parseOpenAIAdvancedSchedulerWeightOverrides(values map[string]string) map[string]float64 {
	overrides := map[string]float64{}
	for _, spec := range openAIAdvancedSchedulerWeightOverrideSpecs() {
		raw := strings.TrimSpace(values[spec.key])
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		overrides[spec.name] = value
	}
	return overrides
}

func cloneOpenAIAdvancedSchedulerWeightOverrides(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *OpenAIGatewayService) getOpenAIAccountScheduler(ctx context.Context) OpenAIAccountScheduler {
	if s == nil {
		return nil
	}
	if !s.isOpenAIAdvancedSchedulerEnabled(ctx) {
		return nil
	}
	s.openaiSchedulerOnce.Do(func() {
		if s.openaiAccountStats == nil {
			s.openaiAccountStats = newOpenAIAccountRuntimeStats()
		}
		if s.openaiScheduler == nil {
			s.openaiScheduler = newDefaultOpenAIAccountScheduler(s, s.openaiAccountStats)
		}
	})
	return s.openaiScheduler
}

func resetOpenAIAdvancedSchedulerSettingCacheForTest() {
	openAIAdvancedSchedulerSettingCache = atomic.Value{}
	openAIAdvancedSchedulerSettingSF = singleflight.Group{}
}

func (s *OpenAIGatewayService) SelectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, "", "", requireCompact, PlatformOpenAI, false)
}

// SelectAccountWithSchedulerForCapability 按能力要求调度账号。
// previousResponseCanMove 表示首包 input 可自行重建工具续链，previous_response_id 允许跨账号迁移
// （粘性加权模式下改为加权偏好而非硬粘连）。
func (s *OpenAIGatewayService) SelectAccountWithSchedulerForCapability(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
	previousResponseCanMove bool,
	platformOverride ...string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	platform := PlatformOpenAI
	if len(platformOverride) > 0 {
		platform = platformOverride[0]
	}
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, "", requireCompact, platform, previousResponseCanMove)
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForImages(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIImagesCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	selection, decision, err := s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", requiredCapability, false, PlatformOpenAI, false)
	if err == nil && selection != nil && selection.Account != nil {
		return selection, decision, nil
	}
	// 如果要求 native 能力（如指定了模型）但没有可用的 APIKey 账号，回退到 basic（OAuth 账号）
	if requiredCapability == OpenAIImagesCapabilityNative {
		return s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", OpenAIImagesCapabilityBasic, false, PlatformOpenAI, false)
	}
	return selection, decision, err
}

func (s *OpenAIGatewayService) selectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requiredImageCapability OpenAIImagesCapability,
	requireCompact bool,
	platform string,
	previousResponseCanMove bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	platform = normalizeOpenAICompatiblePlatform(platform)
	decision := OpenAIAccountScheduleDecision{}
	scheduler := s.getOpenAIAccountScheduler(ctx)
	if scheduler == nil {
		decision.Layer = openAIAccountScheduleLayerLoadBalance
		if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
			effectiveExcludedIDs := cloneExcludedAccountIDs(excludedIDs)
			for {
				selection, err := s.selectAccountWithLoadAwareness(ctx, groupID, platform, sessionHash, requestedModel, effectiveExcludedIDs, requireCompact, requiredCapability)
				if err != nil {
					return nil, decision, err
				}
				if selection == nil || selection.Account == nil {
					return selection, decision, nil
				}
				if accountSupportsOpenAICapabilities(selection.Account, requiredCapability, requiredImageCapability) {
					return selection, decision, nil
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				if effectiveExcludedIDs == nil {
					effectiveExcludedIDs = make(map[int64]struct{})
				}
				if _, exists := effectiveExcludedIDs[selection.Account.ID]; exists {
					return nil, decision, ErrNoAvailableAccounts
				}
				effectiveExcludedIDs[selection.Account.ID] = struct{}{}
			}
		}

		effectiveExcludedIDs := cloneExcludedAccountIDs(excludedIDs)
		for {
			selection, err := s.selectAccountWithLoadAwareness(ctx, groupID, platform, sessionHash, requestedModel, effectiveExcludedIDs, requireCompact, requiredCapability)
			if err != nil {
				return nil, decision, err
			}
			if selection == nil || selection.Account == nil {
				return selection, decision, nil
			}
			if s.isOpenAIAccountTransportCompatible(selection.Account, requiredTransport) &&
				accountSupportsOpenAICapabilities(selection.Account, requiredCapability, requiredImageCapability) {
				return selection, decision, nil
			}
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if effectiveExcludedIDs == nil {
				effectiveExcludedIDs = make(map[int64]struct{})
			}
			if _, exists := effectiveExcludedIDs[selection.Account.ID]; exists {
				return nil, decision, ErrNoAvailableAccounts
			}
			effectiveExcludedIDs[selection.Account.ID] = struct{}{}
		}
	}

	if s.checkChannelPricingRestriction(ctx, groupID, requestedModel) {
		slog.Warn("channel pricing restriction blocked request",
			"group_id", derefGroupID(groupID),
			"model", requestedModel)
		return nil, decision, fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, requestedModel)
	}

	var stickyAccountID int64
	if sessionHash != "" && s.cache != nil {
		if accountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash); err == nil && accountID > 0 {
			stickyAccountID = accountID
		}
	}
	stickyWeighted := s.isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx)
	subscriptionPriority := s.isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx)
	stickyPreviousAccountID := int64(0)
	if stickyWeighted && previousResponseCanMove && strings.TrimSpace(previousResponseID) != "" && platform == PlatformOpenAI {
		stickyPreviousAccountID = s.ResolveAccountIDByPreviousResponseIDForScheduler(ctx, groupID, previousResponseID, requestedModel, excludedIDs, requiredCapability, requireCompact)
	}

	return scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID:                 groupID,
		Platform:                platform,
		SessionHash:             sessionHash,
		StickyAccountID:         stickyAccountID,
		StickyPreviousAccountID: stickyPreviousAccountID,
		StickyWeighted:          stickyWeighted,
		SubscriptionPriority:    subscriptionPriority,
		PreviousResponseID:      previousResponseID,
		PreviousResponseCanMove: previousResponseCanMove,
		RequestedModel:          requestedModel,
		RequiredTransport:       requiredTransport,
		RequiredCapability:      requiredCapability,
		RequiredImageCapability: requiredImageCapability,
		RequireCompact:          requireCompact,
		ExcludedIDs:             excludedIDs,
	})
}

func accountSupportsOpenAICapabilities(account *Account, requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability) bool {
	if account == nil {
		return false
	}
	return account.SupportsOpenAIEndpointCapability(requiredCapability) &&
		account.SupportsOpenAIImageCapability(requiredImageCapability)
}

func cloneExcludedAccountIDs(excludedIDs map[int64]struct{}) map[int64]struct{} {
	if len(excludedIDs) == 0 {
		return nil
	}
	cloned := make(map[int64]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		cloned[id] = struct{}{}
	}
	return cloned
}

func (s *OpenAIGatewayService) isOpenAIAccountTransportCompatible(account *Account, requiredTransport OpenAIUpstreamTransport) bool {
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || account == nil {
		return false
	}
	if requiredTransport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress {
		if s.cfg == nil || !s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled {
			return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == OpenAIUpstreamTransportResponsesWebsocketV2
		}
		mode := account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault)
		switch mode {
		case OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
			return true
		default:
			return false
		}
	}
	return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == requiredTransport
}

func (s *OpenAIGatewayService) ReportOpenAIAccountScheduleResult(accountID int64, success bool, firstTokenMs *int) {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return
	}
	scheduler.ReportResult(accountID, success, firstTokenMs)
}

func (s *OpenAIGatewayService) RecordOpenAIAccountSwitch() {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return
	}
	scheduler.ReportSwitch()
}

func (s *OpenAIGatewayService) SnapshotOpenAIAccountSchedulerMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}
	return scheduler.SnapshotMetrics()
}

func (s *OpenAIGatewayService) openAIWSSessionStickyTTL() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second
	}
	return openaiStickySessionTTL
}

func (s *OpenAIGatewayService) openAIWSLBTopK() int {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.LBTopK > 0 {
		return s.cfg.Gateway.OpenAIWS.LBTopK
	}
	return 7
}

func (s *OpenAIGatewayService) openAIWSLBTopKForRequest(ctx context.Context) int {
	base := s.openAIWSLBTopK()
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	// DB 覆盖值与 stickyWeighted/subscriptionPriority 一样受总开关门控：
	// 关闭高级调度器后所有调用方（含管理页分数快照）都应回到配置/默认行为。
	if !settings.enabled {
		return base
	}
	if settings.lbTopKOverride > 0 {
		return settings.lbTopKOverride
	}
	return base
}

func (s *OpenAIGatewayService) openAIStickyEscapeConfig() openAIStickyEscapeConfig {
	if s != nil && s.cfg != nil {
		cfg := s.cfg.Gateway.OpenAIScheduler
		enabled := cfg.StickyEscapeEnabled
		if !enabled && cfg.StickyEscapeTTFTMs == 0 && cfg.StickyEscapeErrorRate == 0 {
			enabled = true
		}
		ttftMs := float64(cfg.StickyEscapeTTFTMs)
		if ttftMs <= 0 {
			ttftMs = 15000
		}
		errorRate := cfg.StickyEscapeErrorRate
		if errorRate < 0 || errorRate > 1 {
			errorRate = 0.5
		}
		if errorRate == 0 && cfg.StickyEscapeTTFTMs == 0 && cfg.StickyEscapeErrorRate == 0 {
			errorRate = 0.5
		}
		return openAIStickyEscapeConfig{
			enabled:   enabled,
			ttftMs:    ttftMs,
			errorRate: errorRate,
		}
	}
	return openAIStickyEscapeConfig{
		enabled:   true,
		ttftMs:    15000,
		errorRate: 0.5,
	}
}

func (s *OpenAIGatewayService) openAIAccountLatencyConfig() openAIAccountLatencyConfig {
	if s != nil && s.cfg != nil {
		cfg := s.cfg.Gateway.OpenAIScheduler
		return normalizeOpenAIAccountLatencyConfig(openAIAccountLatencyConfig{
			degradeTTFTMs:     float64(cfg.LatencyDegradeTTFTMs),
			recoverTTFTMs:     float64(cfg.LatencyRecoverTTFTMs),
			severeTTFTMs:      float64(cfg.LatencySevereTTFTMs),
			minSamples:        int64(cfg.LatencyMinSamples),
			recoverySuccesses: int64(cfg.LatencyRecoverySuccesses),
			severeErrorRate:   cfg.LatencySevereErrorRate,
		})
	}
	return normalizeOpenAIAccountLatencyConfig(openAIAccountLatencyConfig{})
}

func (s *OpenAIGatewayService) openAIWSSchedulerWeights() GatewayOpenAIWSSchedulerScoreWeightsView {
	if s != nil && s.cfg != nil {
		return GatewayOpenAIWSSchedulerScoreWeightsView{
			Priority:      s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority,
			Load:          s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load,
			Queue:         s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Queue,
			ErrorRate:     s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.ErrorRate,
			TTFT:          s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT,
			Reset:         s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Reset,
			QuotaHeadroom: s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.QuotaHeadroom,
			Previous:      s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.PreviousResponse,
			SessionSticky: s.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky,
		}
	}
	return GatewayOpenAIWSSchedulerScoreWeightsView{
		Priority:      1.0,
		Load:          1.0,
		Queue:         0.7,
		ErrorRate:     0.8,
		TTFT:          0.5,
		Reset:         0.0,
		QuotaHeadroom: 0.0,
		Previous:      5.0,
		SessionSticky: 3.0,
	}
}

func (s *OpenAIGatewayService) openAIWSSchedulerWeightsForRequest(ctx context.Context) GatewayOpenAIWSSchedulerScoreWeightsView {
	weights := s.openAIWSSchedulerWeights()
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	// 同 openAIWSLBTopKForRequest：总开关关闭时不应用 DB 覆盖值。
	if !settings.enabled {
		return weights
	}
	return applyOpenAIAdvancedSchedulerWeightOverrides(weights, settings.weightOverrides)
}

func applyOpenAIAdvancedSchedulerWeightOverrides(
	weights GatewayOpenAIWSSchedulerScoreWeightsView,
	overrides map[string]float64,
) GatewayOpenAIWSSchedulerScoreWeightsView {
	for key, value := range overrides {
		switch key {
		case "priority":
			weights.Priority = value
		case "load":
			weights.Load = value
		case "queue":
			weights.Queue = value
		case "error_rate":
			weights.ErrorRate = value
		case "ttft":
			weights.TTFT = value
		case "reset":
			weights.Reset = value
		case "quota_headroom":
			weights.QuotaHeadroom = value
		case "previous_response":
			weights.Previous = value
		case "session_sticky":
			weights.SessionSticky = value
		}
	}
	return weights
}

type GatewayOpenAIWSSchedulerScoreWeightsView struct {
	Priority  float64
	Load      float64
	Queue     float64
	ErrorRate float64
	TTFT      float64
	// Reset 倾向「会话窗口最早重置」的账号；0 表示关闭（默认）。
	Reset         float64
	QuotaHeadroom float64
	Previous      float64
	SessionSticky float64
}

type OpenAIAccountSchedulerScoreSnapshot struct {
	BaseScore             float64
	StickyScore           float64
	StickyScoreInfinity   bool
	StickyWeightedEnabled bool
}

func (s *RateLimitService) BuildOpenAIAccountSchedulerScoreSnapshot(
	ctx context.Context,
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{cfg: nil, rateLimitService: s}
	if s != nil {
		gateway.cfg = s.cfg
	}
	return buildOpenAIAccountSchedulerScoreSnapshot(accounts, loadMap, gateway.openAIWSSchedulerWeightsForRequest(ctx), gateway.isOpenAIAdvancedSchedulerStickyWeightedEnabled(ctx))
}

func BuildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{}
	return buildOpenAIAccountSchedulerScoreSnapshot(accounts, loadMap, gateway.openAIWSSchedulerWeights(), false)
}

func buildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
	weights GatewayOpenAIWSSchedulerScoreWeightsView,
	stickyWeightedEnabled bool,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	if len(accounts) == 0 {
		return nil
	}
	candidates := make([]openAIAccountCandidateScore, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadInfo := loadMap[account.ID]
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
		}
		candidates = append(candidates, openAIAccountCandidateScore{
			account:   account,
			loadInfo:  loadInfo,
			errorRate: 0,
			ttft:      0,
			hasTTFT:   false,
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	minPriority, maxPriority := openAIAccountSchedulingPriority(candidates[0].account), openAIAccountSchedulingPriority(candidates[0].account)
	maxWaiting := 1
	for i := range candidates {
		candidate := &candidates[i]
		candidate.priority = openAIAccountSchedulingPriority(candidate.account)
		if candidate.priority < minPriority {
			minPriority = candidate.priority
		}
		if candidate.priority > maxPriority {
			maxPriority = candidate.priority
		}
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}
	}

	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	now := time.Now()
	if weights.Reset > 0 {
		for _, candidate := range candidates {
			end := candidate.account.SessionWindowEnd
			if end == nil || !now.Before(*end) {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	result := make(map[int64]OpenAIAccountSchedulerScoreSnapshot, len(candidates))
	for _, candidate := range candidates {
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(candidate.priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - clamp01(float64(candidate.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(candidate.loadInfo.WaitingCount)/float64(maxWaiting))
		errorFactor := 1.0
		ttftFactor := 0.5
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end := candidate.account.SessionWindowEnd; end != nil && now.Before(*end) {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(candidate.account, now)
		}
		baseScore := weights.Priority*priorityFactor +
			weights.Load*loadFactor +
			weights.Queue*queueFactor +
			weights.ErrorRate*errorFactor +
			weights.TTFT*ttftFactor +
			weights.Reset*resetFactor +
			weights.QuotaHeadroom*quotaHeadroomFactor
		score := OpenAIAccountSchedulerScoreSnapshot{
			BaseScore:             baseScore,
			StickyWeightedEnabled: stickyWeightedEnabled,
			StickyScoreInfinity:   !stickyWeightedEnabled,
		}
		if stickyWeightedEnabled {
			score.StickyScore = baseScore + weights.Previous + weights.SessionSticky
		}
		result[candidate.account.ID] = score
	}
	return result
}

func openAIQuotaHeadroomFactor(account *Account, now time.Time) float64 {
	if account == nil || len(account.Extra) == 0 || openAIQuotaHeadroomSnapshotStale(account.Extra, now) {
		return openAIQuotaHeadroomNeutralFactor
	}
	primaryUsedPercent, ok := resolveAccountExtraNumber(account.Extra, "codex_primary_used_percent", "codex_7d_used_percent")
	if !ok || openAIQuotaWindowResetAny(account.Extra, now, "primary", "7d") {
		return openAIQuotaHeadroomNeutralFactor
	}

	factor := 1 - clamp01(primaryUsedPercent/100)
	if secondaryUsedPercent, ok := resolveAccountExtraNumber(account.Extra, "codex_secondary_used_percent", "codex_5h_used_percent"); ok &&
		!openAIQuotaWindowResetAny(account.Extra, now, "secondary", "5h") {
		secondaryRemaining := 1 - clamp01(secondaryUsedPercent/100)
		if secondaryRemaining < openAIQuotaHeadroomSecondaryLowRemain {
			factor *= openAIQuotaHeadroomNeutralFactor
		}
	}
	return factor
}

func openAIQuotaHeadroomSnapshotStale(extra map[string]any, now time.Time) bool {
	updatedRaw, ok := extra["codex_usage_updated_at"]
	if !ok {
		return true
	}
	updatedAt, err := parseTime(fmt.Sprint(updatedRaw))
	if err != nil {
		return true
	}
	return now.Sub(updatedAt) >= openAIQuotaHeadroomSnapshotStaleAfter
}

func openAIQuotaWindowResetAny(extra map[string]any, now time.Time, windows ...string) bool {
	for _, window := range windows {
		if openAIQuotaWindowReset(extra, window, now) {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func calcLoadSkewByMoments(sum float64, sumSquares float64, count int) float64 {
	if count <= 1 {
		return 0
	}
	mean := sum / float64(count)
	variance := sumSquares/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}
