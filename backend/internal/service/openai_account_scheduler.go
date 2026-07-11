package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
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
	enabled   bool
	expiresAt int64
}

var openAIAdvancedSchedulerSettingCache atomic.Value // *cachedOpenAIAdvancedSchedulerSetting
var openAIAdvancedSchedulerSettingSF singleflight.Group

type OpenAIAccountScheduleRequest struct {
	GroupID                 *int64
	Platform                string
	SessionHash             string
	StickyAccountID         int64
	PreserveStickyBinding   bool
	PreviousResponseID      string
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
	if previousResponseID != "" && normalizeOpenAICompatiblePlatform(req.Platform) == PlatformOpenAI {
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
	var stickyWaitAccount *Account
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

	minPriority, maxPriority := candidates[0].account.Priority, candidates[0].account.Priority
	maxWaiting := 1
	loadRateSum := 0.0
	loadRateSumSquares := 0.0
	minTTFT, maxTTFT := 0.0, 0.0
	hasTTFTSample := false
	for _, candidate := range candidates {
		if candidate.account.Priority < minPriority {
			minPriority = candidate.account.Priority
		}
		if candidate.account.Priority > maxPriority {
			maxPriority = candidate.account.Priority
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

	weights := s.service.openAIWSSchedulerWeights()

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
			priorityFactor = 1 - float64(item.account.Priority-minPriority)/float64(maxPriority-minPriority)
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
	}
	if healthyCandidates == 0 && (degradedCandidates > 0 || severeCandidates > 0) {
		slog.Debug("openai_scheduler_all_candidates_latency_degraded",
			"candidate_count", len(candidates),
			"degraded_count", degradedCandidates,
			"severe_count", severeCandidates,
		)
	}
	plan.candidates = candidates

	plan.topK = len(candidates)

	plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
	return plan
}

func (s *defaultOpenAIAccountScheduler) buildOpenAISelectionOrder(
	req OpenAIAccountScheduleRequest,
	plan openAIAccountLoadPlan,
) []openAIAccountCandidateScore {
	buildSelectionOrder := func(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
		if len(pool) == 0 {
			return nil
		}
		return sortOpenAILatencyAwareSelectionOrder(pool)
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
	if !drain.enabled || len(plan.selectionOrder) == 0 {
		return plan.selectionOrder
	}

	oauthCandidates := make([]openAIAccountCandidateScore, 0, len(plan.selectionOrder))
	apiKeyCandidates := make([]openAIAccountCandidateScore, 0, len(plan.selectionOrder))
	for _, candidate := range plan.selectionOrder {
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

	plan := s.buildOpenAIAccountLoadPlan(req, filtered, loadMap)
	candidateCount := plan.candidateCount
	topK := plan.topK
	loadSkew := plan.loadSkew
	drain := s.buildOpenAIAccountDrainContext(req, hardEligible)
	selectionOrder := s.buildOpenAIDrainSelectionOrder(ctx, req, plan, drain)
	if req.RequireCompact && len(plan.candidates) == 0 && len(plan.staleSnapshotCompactRetry) == 0 {
		return nil, 0, 0, 0, ErrNoAvailableCompactAccounts
	}
	if req.RequireCompact && len(selectionOrder) == 0 && s.service.schedulerSnapshot == nil {
		return nil, candidateCount, topK, loadSkew, ErrNoAvailableCompactAccounts
	}
	if len(selectionOrder) == 0 {
		return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, req.RequireCompact && len(plan.allCandidates) > 0)
	}

	result, compactBlocked, acquireErr := s.tryAcquireOpenAISelectionOrder(ctx, req, selectionOrder, drain)
	if acquireErr != nil {
		return nil, candidateCount, topK, loadSkew, acquireErr
	}
	if result != nil {
		return result, candidateCount, topK, loadSkew, nil
	}

	if s.service.concurrencyService != nil {
		if freshLoadMap, loadErr := s.service.concurrencyService.GetAccountsLoadBatchFresh(ctx, loadReq); loadErr == nil {
			freshPlan := s.buildOpenAIAccountLoadPlan(req, filtered, freshLoadMap)
			freshSelectionOrder := s.buildOpenAIDrainSelectionOrder(ctx, req, freshPlan, drain)
			if len(freshSelectionOrder) > 0 {
				freshResult, freshCompactBlocked, freshAcquireErr := s.tryAcquireOpenAISelectionOrder(ctx, req, freshSelectionOrder, drain)
				if freshAcquireErr != nil {
					return nil, candidateCount, topK, loadSkew, freshAcquireErr
				}
				if freshResult != nil {
					return freshResult, freshPlan.candidateCount, freshPlan.topK, freshPlan.loadSkew, nil
				}
				compactBlocked = compactBlocked || freshCompactBlocked
				selectionOrder = freshSelectionOrder
				candidateCount = freshPlan.candidateCount
				topK = freshPlan.topK
				loadSkew = freshPlan.loadSkew
			}
		}
	}

	cfg := s.service.schedulingConfig()
	// WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待。
	for _, candidate := range selectionOrder {
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
				CommitAfterAcquire: s.buildOpenAIDrainWaitCommit(candidate, fresh.ID, drain),
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

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerEnabled(ctx context.Context) bool {
	if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.enabled
		}
	}

	result, _, _ := openAIAdvancedSchedulerSettingSF.Do(openAIAdvancedSchedulerSettingKey, func() (any, error) {
		if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.enabled, nil
			}
		}

		enabled := false
		if repo := s.openAIAdvancedSchedulerSettingRepo(); repo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIAdvancedSchedulerSettingDBTimeout)
			defer cancel()

			value, err := repo.GetValue(dbCtx, openAIAdvancedSchedulerSettingKey)
			if err == nil {
				enabled = strings.EqualFold(strings.TrimSpace(value), "true")
			}
		}

		openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
			enabled:   enabled,
			expiresAt: time.Now().Add(openAIAdvancedSchedulerSettingCacheTTL).UnixNano(),
		})
		return enabled, nil
	})

	enabled, _ := result.(bool)
	return enabled
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
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, "", "", requireCompact, PlatformOpenAI)
}

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
	platformOverride ...string,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	platform := PlatformOpenAI
	if len(platformOverride) > 0 {
		platform = platformOverride[0]
	}
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, "", requireCompact, platform)
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForImages(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIImagesCapability,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	selection, decision, err := s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", requiredCapability, false, PlatformOpenAI)
	if err == nil && selection != nil && selection.Account != nil {
		return selection, decision, nil
	}
	// 如果要求 native 能力（如指定了模型）但没有可用的 APIKey 账号，回退到 basic（OAuth 账号）
	if requiredCapability == OpenAIImagesCapabilityNative {
		return s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", OpenAIImagesCapabilityBasic, false, PlatformOpenAI)
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

	return scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID:                 groupID,
		Platform:                platform,
		SessionHash:             sessionHash,
		StickyAccountID:         stickyAccountID,
		PreviousResponseID:      previousResponseID,
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
	}
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
