package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIPriorityDrainStateStoreStub struct {
	states         map[int64]OpenAIPriorityDrainTTFTState
	getErr         error
	observeErr     error
	observeResults []OpenAIPriorityDrainTTFTObservation
	observeCalls   []openAIPriorityDrainObserveCall
	clearedIDs     []int64
	clearAllCalls  int
	clearErr       error
	fencedGlobal   string
	fencedAccounts map[int64]string
}

type openAIPriorityDrainObserveCall struct {
	accountID   int64
	ttftMs      int64
	thresholdMs int64
	slowCount   int
	window      time.Duration
	cooldown    time.Duration
	policy      OpenAIPriorityDrainTTFTPolicy
}

type priorityDrainValidationAccountRepo struct {
	AdminAccountRepository
	accounts             []Account
	targetAccounts       []*Account
	bulkCalled           bool
	bulkUpdates          []AccountBulkUpdate
	setSchedulableCalled bool
}

func (r *priorityDrainValidationAccountRepo) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r *priorityDrainValidationAccountRepo) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return append([]*Account(nil), r.targetAccounts...), nil
}

func (r *priorityDrainValidationAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *priorityDrainValidationAccountRepo) SetSchedulable(context.Context, int64, bool) error {
	r.setSchedulableCalled = true
	return nil
}

func (r *priorityDrainValidationAccountRepo) BulkUpdate(_ context.Context, _ []int64, update AccountBulkUpdate) (int64, error) {
	r.bulkCalled = true
	r.bulkUpdates = append(r.bulkUpdates, update)
	return 1, nil
}

type priorityDrainValidationSettingRepo struct {
	SettingRepository
	values map[string]string
}

type priorityDrainMetricsProviderStub struct {
	snapshot OpenAIAccountSchedulerMetricsSnapshot
}

func (s *priorityDrainMetricsProviderStub) BlockAccountScheduling(*Account, time.Time, string) {}
func (s *priorityDrainMetricsProviderStub) ClearAccountSchedulingBlock(int64)                  {}
func (s *priorityDrainMetricsProviderStub) SnapshotOpenAIAccountSchedulerMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	return s.snapshot
}

func (r *priorityDrainValidationSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *priorityDrainValidationSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *priorityDrainValidationSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (s *openAIPriorityDrainStateStoreStub) GetOpenAIPriorityDrainTTFTStates(context.Context, map[int64]OpenAIPriorityDrainTTFTPolicy) (map[int64]OpenAIPriorityDrainTTFTState, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.states, nil
}

func (s *openAIPriorityDrainStateStoreStub) ObserveOpenAIPriorityDrainTTFT(_ context.Context, accountID, ttftMs, thresholdMs int64, slowCount int, window, cooldown time.Duration, policy OpenAIPriorityDrainTTFTPolicy) (OpenAIPriorityDrainTTFTObservation, error) {
	s.observeCalls = append(s.observeCalls, openAIPriorityDrainObserveCall{
		accountID: accountID, ttftMs: ttftMs, thresholdMs: thresholdMs,
		slowCount: slowCount, window: window, cooldown: cooldown, policy: policy,
	})
	if s.observeErr != nil {
		return OpenAIPriorityDrainTTFTObservation{}, s.observeErr
	}
	if len(s.observeResults) == 0 {
		return OpenAIPriorityDrainTTFTObservation{}, nil
	}
	result := s.observeResults[0]
	s.observeResults = s.observeResults[1:]
	return result, nil
}

func (s *openAIPriorityDrainStateStoreStub) FenceOpenAIPriorityDrainTTFTGlobalPolicy(_ context.Context, fingerprint string) error {
	s.fencedGlobal = fingerprint
	return s.clearErr
}

func (s *openAIPriorityDrainStateStoreStub) FenceOpenAIPriorityDrainTTFTAccountPolicies(_ context.Context, fingerprints map[int64]string) error {
	s.fencedAccounts = make(map[int64]string, len(fingerprints))
	for accountID, fingerprint := range fingerprints {
		s.fencedAccounts[accountID] = fingerprint
	}
	return s.clearErr
}

func (s *openAIPriorityDrainStateStoreStub) ClearOpenAIPriorityDrainTTFTStates(_ context.Context, accountIDs []int64) error {
	s.clearedIDs = append([]int64(nil), accountIDs...)
	return s.clearErr
}

func (s *openAIPriorityDrainStateStoreStub) ClearAllOpenAIPriorityDrainTTFTStates(context.Context) error {
	s.clearAllCalls++
	return s.clearErr
}

func priorityDrainCandidate(id int64, accountType string, priority, concurrency int) openAIAccountCandidateScore {
	return openAIAccountCandidateScore{
		account:   &Account{ID: id, Platform: PlatformOpenAI, Type: accountType, Priority: priority, GroupIDs: []int64{1}},
		loadInfo:  &AccountLoadInfo{AccountID: id, CurrentConcurrency: concurrency},
		loadKnown: true,
	}
}

func TestOpenAIAccountOrderingHookExcludesUngroupedAccounts(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}, []openAIAccountCandidateScore{
		{account: &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Priority: 1}},
		{account: &Account{ID: 15, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Priority: 1}},
		{account: &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 1}},
	}, openAIPriorityDrainSettings{enabled: true})

	require.True(t, applied)
	require.Empty(t, order)
}

func TestOpenAIAccountSchedulerPriorityDrainKeepsCompactStaleRetryFallback(t *testing.T) {
	service := &OpenAIGatewayService{rateLimitService: newOpenAIPriorityDrainSchedulerRateLimitService(t, false)}
	scheduler := &defaultOpenAIAccountScheduler{
		service:           service,
		stats:             newOpenAIAccountRuntimeStats(),
		priorityDrainHook: &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}},
	}
	plan := openAIAccountLoadPlan{
		candidates: []openAIAccountCandidateScore{
			priorityDrainCandidate(20, AccountTypeAPIKey, 1, 0),
		},
		staleSnapshotCompactRetry: []openAIAccountCandidateScore{
			priorityDrainCandidate(10, AccountTypeOAuth, 1, 0),
		},
		candidateCount: 1,
	}

	require.True(t, scheduler.applyOpenAIPriorityDrainOrdering(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequireCompact: true}, &plan))
	require.Equal(t, []int64{10, 20}, candidateIDs(plan.selectionOrder))
	require.Equal(t, 2, plan.topK)
	require.Equal(t, 2, plan.candidateCount)
}

func TestOpenAIAccountSchedulerPriorityDrainAppliesCooldownAcrossCompactPartitions(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		10: {LastTTFTMs: 20_000, CooldownUntilUnixMs: time.Now().Add(time.Minute).UnixMilli()},
	}}}
	service := &OpenAIGatewayService{rateLimitService: newOpenAIPriorityDrainSchedulerRateLimitService(t, false)}
	scheduler := &defaultOpenAIAccountScheduler{service: service, stats: newOpenAIAccountRuntimeStats(), priorityDrainHook: hook}
	plan := openAIAccountLoadPlan{
		candidates:                []openAIAccountCandidateScore{priorityDrainCandidate(20, AccountTypeAPIKey, 2, 0)},
		staleSnapshotCompactRetry: []openAIAccountCandidateScore{priorityDrainCandidate(10, AccountTypeAPIKey, 1, 0)},
	}

	require.True(t, scheduler.applyOpenAIPriorityDrainOrdering(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequireCompact: true}, &plan))
	require.Equal(t, []int64{20}, candidateIDs(plan.selectionOrder))
}

func TestOpenAIAccountSchedulerPriorityDrainRejectsUngroupedStickyAccount(t *testing.T) {
	service := &OpenAIGatewayService{rateLimitService: newOpenAIPriorityDrainSchedulerRateLimitService(t, false)}
	scheduler := &defaultOpenAIAccountScheduler{service: service, stats: newOpenAIAccountRuntimeStats()}
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

	compatible, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), account, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI})
	require.False(t, compatible)
	require.Equal(t, "priority_drain_ungrouped", reason)
}

func TestOpenAIAccountOrderingHookOAuthBeforeAPIKeys(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "new-session"}, []openAIAccountCandidateScore{
		priorityDrainCandidate(30, AccountTypeAPIKey, 1, 0),
		priorityDrainCandidate(20, AccountTypeOAuth, 2, 0),
		priorityDrainCandidate(10, AccountTypeOAuth, 1, 10),
		priorityDrainCandidate(15, AccountTypeSetupToken, 1, 10),
		priorityDrainCandidate(40, AccountTypeAPIKey, 1, 2),
	}, openAIPriorityDrainSettings{enabled: true})
	require.True(t, applied)
	require.Equal(t, []int64{10, 15, 20, 30, 40}, candidateIDs(order))
}

func TestOpenAIAccountOrderingHookOAuthSamePriorityUsesAccountID(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}, []openAIAccountCandidateScore{
		priorityDrainCandidate(99, AccountTypeOAuth, 1, 0),
		priorityDrainCandidate(20, AccountTypeOAuth, 2, 0),
		priorityDrainCandidate(50, AccountTypeSetupToken, 1, 0),
		priorityDrainCandidate(1, AccountTypeOAuth, 1, 0),
	}, openAIPriorityDrainSettings{enabled: true})

	require.True(t, applied)
	require.Equal(t, []int64{1, 50, 99, 20}, candidateIDs(order))
}

func TestOpenAIAccountOrderingHookDoesNotObserveSetupTokenTTFT(t *testing.T) {
	store := &openAIPriorityDrainStateStoreStub{}
	hook := &OpenAIAccountOrderingHook{stateStore: store}
	account := &Account{
		ID:       45,
		Platform: PlatformOpenAI,
		Type:     AccountTypeSetupToken,
		GroupIDs: []int64{10},
	}
	ttft := 20_000

	hook.ObserveTTFT(context.Background(), account, true, &ttft, openAIPriorityDrainSettings{enabled: true})

	require.Empty(t, store.observeCalls)
}

func TestOpenAIAccountOrderingHookAllCooledFallsBackToLowestTTFT(t *testing.T) {
	now := time.Now().Add(time.Minute).UnixMilli()
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		10: {LastTTFTMs: 9000, CooldownUntilUnixMs: now},
		20: {LastTTFTMs: 4000, CooldownUntilUnixMs: now},
	}}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}, []openAIAccountCandidateScore{
		priorityDrainCandidate(10, AccountTypeAPIKey, 1, 0),
		priorityDrainCandidate(20, AccountTypeAPIKey, 9, 9),
	}, openAIPriorityDrainSettings{enabled: true})
	require.True(t, applied)
	require.Equal(t, []int64{20, 10}, candidateIDs(order))
}

func TestOpenAIAccountOrderingHookSkipsCooledAPIKeyWhenActiveKeyExists(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		10: {LastTTFTMs: 1000, CooldownUntilUnixMs: time.Now().Add(time.Minute).UnixMilli()},
	}}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}, []openAIAccountCandidateScore{
		priorityDrainCandidate(10, AccountTypeAPIKey, 1, 0),
		priorityDrainCandidate(20, AccountTypeAPIKey, 2, 3),
	}, openAIPriorityDrainSettings{enabled: true})

	require.True(t, applied)
	require.Equal(t, []int64{20}, candidateIDs(order))
}

func TestOpenAIAccountOrderingHookExactAPIKeyTiesVaryBySessionInsteadOfAccountID(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	firstIDs := make(map[int64]struct{})
	for i := 0; i < 128; i++ {
		order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{
			Platform:    PlatformOpenAI,
			SessionHash: fmt.Sprintf("tie-session-%d", i),
		}, []openAIAccountCandidateScore{
			priorityDrainCandidate(1, AccountTypeAPIKey, 5, 2),
			priorityDrainCandidate(99, AccountTypeAPIKey, 5, 2),
		}, openAIPriorityDrainSettings{enabled: true})
		require.True(t, applied)
		require.Len(t, order, 2)
		firstIDs[order[0].account.ID] = struct{}{}
	}

	require.Equal(t, map[int64]struct{}{1: {}, 99: {}}, firstIDs)
}

func TestOpenAIAccountOrderingHookDisabledLeavesOrderingToExistingScheduler(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}, []openAIAccountCandidateScore{
		priorityDrainCandidate(20, AccountTypeOAuth, 2, 0),
		priorityDrainCandidate(10, AccountTypeOAuth, 1, 0),
	}, openAIPriorityDrainSettings{enabled: false})

	require.False(t, applied)
	require.Nil(t, order)
}

func TestOpenAIAccountOrderingHookAPIKeyPriorityThenConcurrency(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "priority-before-id"}, []openAIAccountCandidateScore{
		priorityDrainCandidate(1, AccountTypeAPIKey, 2, 0),
		priorityDrainCandidate(99, AccountTypeAPIKey, 1, 7),
		priorityDrainCandidate(100, AccountTypeAPIKey, 1, 1),
	}, openAIPriorityDrainSettings{enabled: true})
	require.True(t, applied)
	require.Equal(t, []int64{100, 99, 1}, candidateIDs(order))
}

func TestOpenAIAccountOrderingHookFallsBackToStaticOrderWhenRedisIsUnavailable(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{getErr: errors.New("redis unavailable")}}
	order, applied := hook.Order(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "redis-fallback"}, []openAIAccountCandidateScore{
		priorityDrainCandidate(10, AccountTypeAPIKey, 2, 0),
		priorityDrainCandidate(20, AccountTypeAPIKey, 1, 3),
	}, openAIPriorityDrainSettings{enabled: true})
	require.True(t, applied)
	require.Equal(t, []int64{20, 10}, candidateIDs(order))
	require.Equal(t, int64(1), hook.SnapshotMetrics().RedisFallbackTotal)
}

func TestOpenAIPriorityDrainLogLimiterReportsSuppressedFailuresAtNextInterval(t *testing.T) {
	var limiter openAIPriorityDrainLogLimiter
	base := time.Unix(1_800_000_000, 0)

	allowed, suppressed := limiter.Allow(base, time.Minute)
	require.True(t, allowed)
	require.Zero(t, suppressed)

	allowed, _ = limiter.Allow(base.Add(time.Second), time.Minute)
	require.False(t, allowed)
	allowed, _ = limiter.Allow(base.Add(30*time.Second), time.Minute)
	require.False(t, allowed)

	allowed, suppressed = limiter.Allow(base.Add(time.Minute), time.Minute)
	require.True(t, allowed)
	require.Equal(t, int64(2), suppressed)
}

func TestOpenAIPriorityDrainRedisFallbackMetricCountsSuppressedLogs(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{stateStore: &openAIPriorityDrainStateStoreStub{getErr: errors.New("redis unavailable")}}
	account := &Account{
		ID:       46,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		GroupIDs: []int64{10},
	}
	settings := openAIPriorityDrainSettings{enabled: true}

	require.False(t, hook.IsAPIKeyCoolingDown(context.Background(), account, settings))
	require.False(t, hook.IsAPIKeyCoolingDown(context.Background(), account, settings))
	require.Equal(t, int64(2), hook.SnapshotMetrics().RedisFallbackTotal)
}

func TestOpenAIAccountOrderingHookObserveTTFTUsesAccountOverrideAndRecordsLifecycle(t *testing.T) {
	store := &openAIPriorityDrainStateStoreStub{observeResults: []OpenAIPriorityDrainTTFTObservation{
		{EnteredCooldown: true},
		{Recovered: true},
	}}
	hook := &OpenAIAccountOrderingHook{stateStore: store}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		GroupIDs: []int64{10},
		Extra: map[string]any{
			openAIPriorityDrainTTFTThresholdExtraKey: 10,
		},
	}
	settings := openAIPriorityDrainSettings{
		enabled: true, ttftThresholdSeconds: 15, consecutiveSlowCount: 2,
		statisticsWindow: 3 * time.Minute, softCooldown: 4 * time.Minute,
	}
	slowTTFT := 10_001
	fastTTFT := 9_999
	hook.ObserveTTFT(context.Background(), account, true, &slowTTFT, settings)
	hook.ObserveTTFT(context.Background(), account, true, &fastTTFT, settings)

	require.Equal(t, []openAIPriorityDrainObserveCall{
		{accountID: 42, ttftMs: 10_001, thresholdMs: 10_000, slowCount: 2, window: 3 * time.Minute, cooldown: 4 * time.Minute, policy: OpenAIPriorityDrainTTFTPolicy{GlobalFingerprint: "threshold=15;count=2;window_ms=180000;cooldown_ms=240000", AccountFingerprint: "threshold=10"}},
		{accountID: 42, ttftMs: 9_999, thresholdMs: 10_000, slowCount: 2, window: 3 * time.Minute, cooldown: 4 * time.Minute, policy: OpenAIPriorityDrainTTFTPolicy{GlobalFingerprint: "threshold=15;count=2;window_ms=180000;cooldown_ms=240000", AccountFingerprint: "threshold=10"}},
	}, store.observeCalls)
	metrics := hook.SnapshotMetrics()
	require.Equal(t, int64(1), metrics.APIKeyCooldownTotal)
	require.Equal(t, int64(1), metrics.APIKeyRecoveredTotal)
}

func TestOpenAIAccountSchedulerReportResultRoutesSuccessfulAPIKeyTTFTToHook(t *testing.T) {
	store := &openAIPriorityDrainStateStoreStub{}
	account := Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, GroupIDs: []int64{10}}
	service := &OpenAIGatewayService{
		accountRepo:      schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		rateLimitService: newOpenAIPriorityDrainSchedulerRateLimitService(t, false),
	}
	scheduler := &defaultOpenAIAccountScheduler{
		service:           service,
		stats:             newOpenAIAccountRuntimeStats(),
		priorityDrainHook: &OpenAIAccountOrderingHook{stateStore: store},
	}
	ttft := 16_000

	scheduler.ReportResult(account.ID, true, &ttft)

	require.Equal(t, []openAIPriorityDrainObserveCall{{
		accountID: account.ID, ttftMs: 16_000, thresholdMs: 15_000,
		slowCount: 2, window: 15 * time.Minute, cooldown: 15 * time.Minute,
		policy: OpenAIPriorityDrainTTFTPolicy{GlobalFingerprint: "threshold=15;count=2;window_ms=900000;cooldown_ms=900000", AccountFingerprint: "default"},
	}}, store.observeCalls)
}

func TestOpenAIAccountSchedulerReportResultUsesSchedulerSnapshotAccount(t *testing.T) {
	store := &openAIPriorityDrainStateStoreStub{}
	account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, GroupIDs: []int64{10}}
	service := &OpenAIGatewayService{
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{account.ID: account}}},
		rateLimitService:  newOpenAIPriorityDrainSchedulerRateLimitService(t, false),
	}
	scheduler := &defaultOpenAIAccountScheduler{
		service:           service,
		stats:             newOpenAIAccountRuntimeStats(),
		priorityDrainHook: &OpenAIAccountOrderingHook{stateStore: store},
	}
	ttft := 16_000

	scheduler.ReportResult(account.ID, true, &ttft)

	require.Len(t, store.observeCalls, 1)
	require.Equal(t, account.ID, store.observeCalls[0].accountID)
}

func TestOpenAIPriorityDrainRequiresAdvancedSchedulerAndTakesOverSubscriptionPriority(t *testing.T) {
	settings := &SystemSettings{
		OpenAIPriorityDrainEnabled:                 true,
		OpenAIPriorityDrainTTFTThresholdSeconds:    15,
		OpenAIPriorityDrainConsecutiveSlowCount:    2,
		OpenAIPriorityDrainStatisticsWindowSeconds: 900,
		OpenAIPriorityDrainSoftCooldownSeconds:     900,
	}
	svc := NewSettingService(&priorityDrainValidationSettingRepo{}, &config.Config{})
	err := svc.normalizeOpenAIAdvancedSchedulerOverrides(settings)
	require.Error(t, err)
	require.ErrorContains(t, err, "requires the advanced scheduler")

	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	repo := &openAIAdvancedSchedulerSettingRepoStub{values: map[string]string{
		openAIAdvancedSchedulerSettingKey:                            "true",
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled:       "true",
		SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled: "true",
		SettingKeyOpenAIPriorityDrainEnabled:                         "true",
	}}
	gateway := &OpenAIGatewayService{rateLimitService: &RateLimitService{
		settingService: NewSettingService(repo, &config.Config{}),
	}}
	require.False(t, gateway.isOpenAIAdvancedSchedulerStickyWeightedEnabled(context.Background()))
	require.False(t, gateway.isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(context.Background()))

	repo.values[SettingKeyOpenAIPriorityDrainEnabled] = "false"
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	require.True(t, gateway.isOpenAIAdvancedSchedulerStickyWeightedEnabled(context.Background()))
	require.True(t, gateway.isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(context.Background()))
}

func newOpenAIPriorityDrainBindingTestService(
	t *testing.T,
	store *openAIPriorityDrainStateStoreStub,
) (*OpenAIGatewayService, *schedulerTestGatewayCache, *OpenAIAccountOrderingHook, Account, Account) {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	groupID := int64(77)
	sticky := Account{
		ID: 7701, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 2, Priority: 100, GroupIDs: []int64{groupID},
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
	}
	preferred := Account{
		ID: 7702, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Concurrency: 2, Priority: 0, GroupIDs: []int64{groupID},
	}
	cache := &schedulerTestGatewayCache{}
	repo := &openAIAdvancedSchedulerSettingRepoStub{values: map[string]string{
		openAIAdvancedSchedulerSettingKey:                      "true",
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled: "true",
		SettingKeyOpenAIPriorityDrainEnabled:                   "true",
	}}
	cfg := newSchedulerTestOpenAIWSV2Config()
	service := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{sticky, preferred}},
		cache:              cache,
		cfg:                cfg,
		rateLimitService:   &RateLimitService{settingService: NewSettingService(repo, &config.Config{})},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	hook := &OpenAIAccountOrderingHook{stateStore: store}
	scheduler := newDefaultOpenAIAccountScheduler(service, nil)
	defaultScheduler, ok := scheduler.(*defaultOpenAIAccountScheduler)
	require.True(t, ok)
	defaultScheduler.priorityDrainHook = hook
	service.openaiScheduler = scheduler
	return service, cache, hook, sticky, preferred
}

func TestOpenAIPriorityDrainCooledSessionStickyReentersScheduling(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	store := &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		7701: {LastTTFTMs: 20_000, CooldownUntilUnixMs: time.Now().Add(time.Minute).UnixMilli()},
	}}
	service, cache, hook, sticky, preferred := newOpenAIPriorityDrainBindingTestService(t, store)
	cache.sessionBindings = map[string]int64{"openai:priority_drain_session": sticky.ID}
	groupID := int64(77)

	selection, decision, err := service.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "priority_drain_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, preferred.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, preferred.ID, cache.sessionBindings["openai:priority_drain_session"])
	require.Equal(t, 1, cache.deletedSessions["openai:priority_drain_session"])
	require.Equal(t, int64(1), hook.SnapshotMetrics().CooldownStickyEscapeTotal)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIPriorityDrainActiveSessionStickyKeepsBinding(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	store := &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		7701: {LastTTFTMs: 9_000},
	}}
	service, cache, hook, sticky, _ := newOpenAIPriorityDrainBindingTestService(t, store)
	cache.sessionBindings = map[string]int64{"openai:priority_drain_session": sticky.ID}
	groupID := int64(77)

	selection, decision, err := service.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "priority_drain_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, sticky.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.Equal(t, sticky.ID, cache.sessionBindings["openai:priority_drain_session"])
	require.Zero(t, cache.deletedSessions["openai:priority_drain_session"])
	require.Zero(t, hook.SnapshotMetrics().CooldownStickyEscapeTotal)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIPriorityDrainCooledMovablePreviousResponseReentersScheduling(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	store := &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		7701: {LastTTFTMs: 20_000, CooldownUntilUnixMs: time.Now().Add(time.Minute).UnixMilli()},
	}}
	service, cache, hook, sticky, preferred := newOpenAIPriorityDrainBindingTestService(t, store)
	ctx := context.Background()
	groupID := int64(77)
	require.NoError(t, service.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_priority_drain", sticky.ID, time.Hour))

	selection, decision, err := service.SelectAccountWithSchedulerForCapability(
		ctx, &groupID, "resp_priority_drain", "priority_drain_movable", "gpt-5.1", nil,
		OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, true, true, PlatformOpenAI,
	)
	require.NoError(t, err)
	require.Equal(t, preferred.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, preferred.ID, cache.sessionBindings["openai:priority_drain_movable"])
	require.Equal(t, int64(1), hook.SnapshotMetrics().CooldownPreviousResponseMovedTotal)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIPriorityDrainAllCooledPreviousResponseFallbackToSameAccountDoesNotRecordMove(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	cooldownUntil := time.Now().Add(time.Minute).UnixMilli()
	store := &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		7701: {LastTTFTMs: 1_000, CooldownUntilUnixMs: cooldownUntil},
		7703: {LastTTFTMs: 9_000, CooldownUntilUnixMs: cooldownUntil},
	}}
	service, _, hook, sticky, _ := newOpenAIPriorityDrainBindingTestService(t, store)
	groupID := int64(77)
	fallback := Account{
		ID: 7703, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 2, Priority: 0, GroupIDs: []int64{groupID},
	}
	service.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{sticky, fallback}}
	ctx := context.Background()
	require.NoError(t, service.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_priority_drain_all_cooled", sticky.ID, time.Hour))

	selection, decision, err := service.SelectAccountWithSchedulerForCapability(
		ctx, &groupID, "resp_priority_drain_all_cooled", "priority_drain_all_cooled", "gpt-5.1", nil,
		OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, true, true, PlatformOpenAI,
	)
	require.NoError(t, err)
	require.Equal(t, sticky.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	metrics := hook.SnapshotMetrics()
	require.Equal(t, int64(1), metrics.APIKeyAllCooledFallbackTotal)
	require.Zero(t, metrics.CooldownPreviousResponseMovedTotal)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIPriorityDrainFailedRerouteDoesNotRecordMove(t *testing.T) {
	hook := &OpenAIAccountOrderingHook{}
	scheduler := &defaultOpenAIAccountScheduler{priorityDrainHook: hook}

	scheduler.recordOpenAIPriorityDrainCooldownReroute(&openAIPriorityDrainCooldownReroute{
		accountID: 7701,
		layer:     openAIAccountScheduleLayerPreviousResponse,
	}, nil)

	require.Zero(t, hook.SnapshotMetrics().CooldownPreviousResponseMovedTotal)
}

func TestOpenAIPriorityDrainCooledNonMovablePreviousResponsePreservesBinding(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	store := &openAIPriorityDrainStateStoreStub{states: map[int64]OpenAIPriorityDrainTTFTState{
		7701: {LastTTFTMs: 20_000, CooldownUntilUnixMs: time.Now().Add(time.Minute).UnixMilli()},
	}}
	service, _, hook, sticky, _ := newOpenAIPriorityDrainBindingTestService(t, store)
	ctx := context.Background()
	groupID := int64(77)
	require.NoError(t, service.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_priority_drain", sticky.ID, time.Hour))

	selection, decision, err := service.SelectAccountWithScheduler(
		ctx, &groupID, "resp_priority_drain", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, sticky.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.Equal(t, int64(1), hook.SnapshotMetrics().CooldownPreviousResponsePreservedTotal)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIPriorityDrainRedisFailureKeepsSessionSticky(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	store := &openAIPriorityDrainStateStoreStub{getErr: errors.New("redis unavailable")}
	service, cache, hook, sticky, _ := newOpenAIPriorityDrainBindingTestService(t, store)
	cache.sessionBindings = map[string]int64{"openai:priority_drain_session": sticky.ID}
	groupID := int64(77)

	selection, decision, err := service.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "priority_drain_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, sticky.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, decision.Layer)
	require.Equal(t, int64(1), hook.SnapshotMetrics().RedisFallbackTotal)
	require.Zero(t, cache.deletedSessions["openai:priority_drain_session"])
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSettingServiceGlobalPriorityDrainTTFTChangeClearsAllState(t *testing.T) {
	repo := &priorityDrainValidationSettingRepo{values: map[string]string{
		SettingKeyOpenAIPriorityDrainTTFTThresholdSeconds:    "15",
		SettingKeyOpenAIPriorityDrainConsecutiveSlowCount:    "2",
		SettingKeyOpenAIPriorityDrainStatisticsWindowSeconds: "900",
		SettingKeyOpenAIPriorityDrainSoftCooldownSeconds:     "900",
	}}
	store := &openAIPriorityDrainStateStoreStub{}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetOpenAIPriorityDrainStateStore(store)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAIOAuthSchedulingRateMultiplier:        1,
		OpenAIPriorityDrainTTFTThresholdSeconds:    10,
		OpenAIPriorityDrainConsecutiveSlowCount:    2,
		OpenAIPriorityDrainStatisticsWindowSeconds: 900,
		OpenAIPriorityDrainSoftCooldownSeconds:     900,
	})

	require.NoError(t, err)
	require.Equal(t, 1, store.clearAllCalls)
	require.Equal(t, "threshold=10;count=2;window_ms=900000;cooldown_ms=900000", store.fencedGlobal)
}

func TestSettingServiceUnchangedGlobalPriorityDrainTTFTKeepsState(t *testing.T) {
	repo := &priorityDrainValidationSettingRepo{values: map[string]string{
		SettingKeyOpenAIPriorityDrainTTFTThresholdSeconds:    "15",
		SettingKeyOpenAIPriorityDrainConsecutiveSlowCount:    "2",
		SettingKeyOpenAIPriorityDrainStatisticsWindowSeconds: "900",
		SettingKeyOpenAIPriorityDrainSoftCooldownSeconds:     "900",
	}}
	store := &openAIPriorityDrainStateStoreStub{}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetOpenAIPriorityDrainStateStore(store)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAIOAuthSchedulingRateMultiplier:        1,
		OpenAIPriorityDrainTTFTThresholdSeconds:    15,
		OpenAIPriorityDrainConsecutiveSlowCount:    2,
		OpenAIPriorityDrainStatisticsWindowSeconds: 900,
		OpenAIPriorityDrainSoftCooldownSeconds:     900,
	})

	require.NoError(t, err)
	require.Zero(t, store.clearAllCalls)
}

func TestNewAdminServiceExposesOpenAIAccountSchedulerMetrics(t *testing.T) {
	provider := &priorityDrainMetricsProviderStub{snapshot: OpenAIAccountSchedulerMetricsSnapshot{
		SelectTotal: 7,
		PriorityDrain: OpenAIPriorityDrainMetricsSnapshot{
			RedisFallbackTotal: 3,
		},
	}}
	admin := NewAdminService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, provider, nil, nil, nil, nil)

	require.Equal(t, provider.snapshot, admin.GetOpenAIAccountSchedulerMetrics())
}

func TestPriorityDrainBulkUpdateRejectsTTFTOverrideThroughGenericExtra(t *testing.T) {
	repo := &priorityDrainValidationAccountRepo{}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		Extra: map[string]any{
			openAIPriorityDrainTTFTThresholdExtraKey: 10,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "dedicated bulk field")
	require.False(t, repo.bulkCalled)
}

func TestPriorityDrainRejectsTTFTOverrideThroughSingleAccountExtra(t *testing.T) {
	svc := &adminServiceImpl{}

	err := svc.UpdateAccountExtra(context.Background(), 1, map[string]any{
		openAIPriorityDrainTTFTThresholdExtraKey: 10,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "dedicated bulk field")
}

func TestPriorityDrainBulkTTFTOverrideWritesAndClearsState(t *testing.T) {
	repo := &priorityDrainValidationAccountRepo{targetAccounts: []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true},
	}}
	stateStore := &openAIPriorityDrainStateStoreStub{}
	svc := &adminServiceImpl{accountRepo: repo, openAIPriorityDrainStateStore: stateStore}
	threshold := 10

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:                              []int64{1},
		OpenAIPriorityDrainTTFTThresholdSeconds: &threshold,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Len(t, repo.bulkUpdates, 1)
	require.Equal(t, 10, repo.bulkUpdates[0].Extra[openAIPriorityDrainTTFTThresholdExtraKey])
	require.Equal(t, []int64{1}, stateStore.clearedIDs)
	require.Equal(t, map[int64]string{1: "threshold=10"}, stateStore.fencedAccounts)

	repo.bulkUpdates = nil
	stateStore.clearedIDs = nil
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:                            []int64{1},
		ClearOpenAIPriorityDrainTTFTThreshold: true,
	})
	require.NoError(t, err)
	require.Len(t, repo.bulkUpdates, 1)
	require.Equal(t, []string{openAIPriorityDrainTTFTThresholdExtraKey}, repo.bulkUpdates[0].ClearExtraKeys)
	require.Equal(t, []int64{1}, stateStore.clearedIDs)
	require.Equal(t, map[int64]string{1: "default"}, stateStore.fencedAccounts)
}

func TestPriorityDrainBulkTTFTOverrideRejectsInvalidTargetKinds(t *testing.T) {
	threshold := 10
	tests := []struct {
		name     string
		accounts []*Account
		ids      []int64
	}{
		{
			name: "OAuth",
			accounts: []*Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			},
			ids: []int64{1},
		},
		{
			name: "setup-token",
			accounts: []*Account{
				{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
			},
			ids: []int64{4},
		},
		{
			name: "mixed OpenAI account types",
			accounts: []*Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			},
			ids: []int64{1, 2},
		},
		{
			name: "non-OpenAI API Key",
			accounts: []*Account{
				{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
			},
			ids: []int64{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &priorityDrainValidationAccountRepo{targetAccounts: tt.accounts}
			svc := &adminServiceImpl{accountRepo: repo}
			_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs:                              tt.ids,
				OpenAIPriorityDrainTTFTThresholdSeconds: &threshold,
			})
			require.Error(t, err)
			require.ErrorContains(t, err, "only be changed for OpenAI API Key accounts")
			require.False(t, repo.bulkCalled)
		})
	}
}

func TestPriorityDrainBulkTTFTStateClearFailureIsFailOpen(t *testing.T) {
	repo := &priorityDrainValidationAccountRepo{targetAccounts: []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}}
	store := &openAIPriorityDrainStateStoreStub{clearErr: errors.New("redis unavailable")}
	svc := &adminServiceImpl{accountRepo: repo, openAIPriorityDrainStateStore: store}
	threshold := 10

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:                              []int64{1},
		OpenAIPriorityDrainTTFTThresholdSeconds: &threshold,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.True(t, repo.bulkCalled)
	require.Equal(t, []int64{1}, store.clearedIDs)
}

func candidateIDs(candidates []openAIAccountCandidateScore) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.account != nil {
			ids = append(ids, candidate.account.ID)
		}
	}
	return ids
}
