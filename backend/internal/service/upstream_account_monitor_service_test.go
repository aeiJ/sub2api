package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAccountMonitorEnableAllIgnoresScheduledTestPlans(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	scheduledPlan := &ScheduledTestPlan{
		ID:             10,
		AccountID:      account.ID,
		Purpose:        ScheduledTestPlanPurposeScheduledTest,
		CronExpression: "0 * * * *",
		Enabled:        false,
	}
	planRepo := newUpstreamMonitorPlanRepo(scheduledPlan)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		planRepo,
		nil,
		nil,
	)

	resp, err := svc.EnableAll(ctx, UpstreamAccountMonitorBatchParams{})

	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	require.Equal(t, 1, resp.Created)
	require.Equal(t, 1, resp.Enabled)
	require.False(t, scheduledPlan.Enabled)

	monitorPlans := planRepo.byAccountAndPurpose(account.ID, ScheduledTestPlanPurposeUpstreamMonitor)
	require.Len(t, monitorPlans, 1)
	require.True(t, monitorPlans[0].Enabled)
	require.False(t, monitorPlans[0].AutoRecover)
	require.Equal(t, defaultUpstreamAccountMonitorInterval, monitorPlans[0].IntervalMinutes)
}

func TestUpstreamAccountMonitorDisableAllOnlyTouchesMonitorPlans(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	scheduledPlan := &ScheduledTestPlan{
		ID:             10,
		AccountID:      account.ID,
		Purpose:        ScheduledTestPlanPurposeScheduledTest,
		CronExpression: "0 * * * *",
		Enabled:        true,
	}
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         true,
	}
	planRepo := newUpstreamMonitorPlanRepo(scheduledPlan, monitorPlan)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		planRepo,
		nil,
		nil,
	)

	resp, err := svc.DisableAll(ctx, UpstreamAccountMonitorBatchParams{})

	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	require.Equal(t, 1, resp.Updated)
	require.Equal(t, 1, resp.Disabled)
	require.True(t, scheduledPlan.Enabled)
	require.False(t, monitorPlan.Enabled)
}

func TestUpstreamAccountMonitorEnableAllRefreshesExistingMonitorNextRun(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	oldNextRun := time.Now().Add(-time.Hour)
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         false,
		NextRunAt:       &oldNextRun,
	}
	planRepo := newUpstreamMonitorPlanRepo(monitorPlan)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		planRepo,
		nil,
		nil,
	)

	startedAt := time.Now()
	resp, err := svc.EnableAll(ctx, UpstreamAccountMonitorBatchParams{})

	require.NoError(t, err)
	require.Equal(t, 1, resp.Updated)
	require.Equal(t, 1, resp.Enabled)
	require.True(t, monitorPlan.Enabled)
	require.NotNil(t, monitorPlan.NextRunAt)
	require.True(t, monitorPlan.NextRunAt.After(startedAt))
}

func TestUpstreamAccountMonitorUpdateSettingsKeepsDisabledMonitorDisabledAndDoesNotRenameAccount(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         false,
	}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		newUpstreamMonitorPlanRepo(monitorPlan),
		nil,
		nil,
	)

	item, err := svc.UpdateSettings(ctx, account.ID, UpstreamAccountMonitorSettingsRequest{
		ModelID:         "gpt-test",
		IntervalMinutes: 30,
		JitterSeconds:   10,
		AutoRecover:     true,
	})

	require.NoError(t, err)
	require.Equal(t, "apikey-1", item.AccountName)
	require.False(t, item.MonitorEnabled)
	require.True(t, item.AutoRecover)
	require.False(t, monitorPlan.Enabled)
	require.True(t, monitorPlan.AutoRecover)
	require.Equal(t, "gpt-test", monitorPlan.ModelID)
	require.Equal(t, 30, monitorPlan.IntervalMinutes)
	require.Equal(t, 10, monitorPlan.JitterSeconds)
}

func TestUpstreamAccountMonitorUpdateSettingsCanToggleMonitorEnabled(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         false,
	}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		newUpstreamMonitorPlanRepo(monitorPlan),
		nil,
		nil,
	)
	enabled := true

	item, err := svc.UpdateSettings(ctx, account.ID, UpstreamAccountMonitorSettingsRequest{
		ModelID:         "gpt-test",
		IntervalMinutes: 30,
		JitterSeconds:   10,
		AutoRecover:     true,
		MonitorEnabled:  &enabled,
	})

	require.NoError(t, err)
	require.True(t, item.MonitorEnabled)
	require.True(t, monitorPlan.Enabled)
}

func TestUpstreamAccountMonitorUpdateSettingsRejectsDisablingSchedulableAccount(t *testing.T) {
	ctx := context.Background()
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         true,
	}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		newUpstreamMonitorPlanRepo(monitorPlan),
		nil,
		nil,
	)
	enabled := false

	item, err := svc.UpdateSettings(ctx, account.ID, UpstreamAccountMonitorSettingsRequest{
		ModelID:         "gpt-test",
		IntervalMinutes: 30,
		JitterSeconds:   10,
		AutoRecover:     true,
		MonitorEnabled:  &enabled,
	})

	require.Error(t, err)
	require.Nil(t, item)
	require.Contains(t, err.Error(), "monitoring is required")
	require.True(t, monitorPlan.Enabled)
}

func TestUpstreamAccountMonitorUpdateSettingsRejectsDisablingRuntimeCooledScheduledAccount(t *testing.T) {
	ctx := context.Background()
	cooldownUntil := time.Now().Add(time.Hour)
	account := Account{
		ID:                      1,
		Name:                    "apikey-1",
		Type:                    AccountTypeAPIKey,
		Platform:                PlatformOpenAI,
		Status:                  StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &cooldownUntil,
		TempUnschedulableReason: "high latency cooldown",
	}
	require.False(t, account.IsSchedulable(), "precondition: runtime cooldown should make the account temporarily unschedulable")
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         true,
	}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		newUpstreamMonitorPlanRepo(monitorPlan),
		nil,
		nil,
	)
	enabled := false

	item, err := svc.UpdateSettings(ctx, account.ID, UpstreamAccountMonitorSettingsRequest{
		ModelID:         "gpt-test",
		IntervalMinutes: 30,
		JitterSeconds:   10,
		AutoRecover:     true,
		MonitorEnabled:  &enabled,
	})

	require.Error(t, err)
	require.Nil(t, item)
	require.Contains(t, err.Error(), "monitoring is required")
	require.True(t, monitorPlan.Enabled)
}

func TestUpstreamAccountMonitorListReturns7dAnd15dAvailability(t *testing.T) {
	ctx := context.Background()
	pingLatency := int64(12)
	account := Account{ID: 1, Name: "apikey-1", Type: AccountTypeAPIKey, Platform: "openai"}
	monitorPlan := &ScheduledTestPlan{
		ID:              11,
		AccountID:       account.ID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         true,
	}
	resultRepo := &upstreamMonitorResultRepo{
		statsSequence: []map[int64]*ScheduledTestPlanStats{
			{monitorPlan.ID: {PlanID: monitorPlan.ID, Total: 4, Success: 3, Availability7d: 75}},
			{monitorPlan.ID: {PlanID: monitorPlan.ID, Total: 10, Success: 5, Availability7d: 50}},
		},
		latest: map[int64]*ScheduledTestResult{
			monitorPlan.ID: {PlanID: monitorPlan.ID, Status: "success", LatencyMs: 123, PingLatencyMs: &pingLatency},
		},
		recent: map[int64][]*ScheduledTestResult{
			monitorPlan.ID: {{PlanID: monitorPlan.ID, Status: "success", LatencyMs: 123, PingLatencyMs: &pingLatency}},
		},
	}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{account}},
		newUpstreamMonitorPlanRepo(monitorPlan),
		resultRepo,
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	require.NotNil(t, resp.Items[0].Availability7d)
	require.NotNil(t, resp.Items[0].Availability15d)
	require.Equal(t, 75.0, *resp.Items[0].Availability7d)
	require.Equal(t, 50.0, *resp.Items[0].Availability15d)
	require.NotNil(t, resp.Items[0].LatestResult)
	require.Equal(t, int64(123), resp.Items[0].LatestResult.LatencyMs)
	require.NotNil(t, resp.Items[0].LatestResult.PingLatencyMs)
	require.Equal(t, pingLatency, *resp.Items[0].LatestResult.PingLatencyMs)
	require.Len(t, resp.Items[0].Timeline, 1)
	require.NotNil(t, resp.Items[0].Timeline[0].PingLatencyMs)
	require.Equal(t, pingLatency, *resp.Items[0].Timeline[0].PingLatencyMs)
	require.Len(t, resultRepo.statsSince, 2)
	require.True(t, resultRepo.statsSince[1].Before(resultRepo.statsSince[0]))
}

func TestUpstreamAccountMonitorEnableAllRespectsFilters(t *testing.T) {
	ctx := context.Background()
	groupOne := &Group{ID: 1, Name: "group-one"}
	groupTwo := &Group{ID: 2, Name: "group-two"}
	accountOne := Account{ID: 1, Name: "openai-one", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Groups: []*Group{groupOne}}
	accountTwo := Account{ID: 2, Name: "openai-two", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Groups: []*Group{groupTwo}}
	planOne := &ScheduledTestPlan{ID: 11, AccountID: accountOne.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: false}
	planTwo := &ScheduledTestPlan{ID: 12, AccountID: accountTwo.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: false}
	accountRepo := &upstreamMonitorAccountRepo{accounts: []Account{accountOne, accountTwo}}
	svc := NewUpstreamAccountMonitorService(
		accountRepo,
		newUpstreamMonitorPlanRepo(planOne, planTwo),
		nil,
		nil,
	)

	resp, err := svc.EnableAll(ctx, UpstreamAccountMonitorBatchParams{GroupID: groupOne.ID})

	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	require.Equal(t, 1, resp.Enabled)
	require.True(t, planOne.Enabled)
	require.False(t, planTwo.Enabled)
	require.NotEmpty(t, accountRepo.calls)
	require.Equal(t, groupOne.ID, accountRepo.calls[0].GroupID)
}

func TestUpstreamAccountMonitorDisableAllSkipsSchedulableAccounts(t *testing.T) {
	ctx := context.Background()
	schedulable := Account{ID: 1, Name: "openai-one", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	paused := Account{ID: 2, Name: "openai-two", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false}
	schedulablePlan := &ScheduledTestPlan{ID: 11, AccountID: schedulable.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: true}
	pausedPlan := &ScheduledTestPlan{ID: 12, AccountID: paused.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: true}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{schedulable, paused}},
		newUpstreamMonitorPlanRepo(schedulablePlan, pausedPlan),
		nil,
		nil,
	)

	resp, err := svc.DisableAll(ctx, UpstreamAccountMonitorBatchParams{})

	require.NoError(t, err)
	require.Equal(t, 2, resp.Total)
	require.Equal(t, 1, resp.Skipped)
	require.Equal(t, 1, resp.Disabled)
	require.True(t, schedulablePlan.Enabled)
	require.False(t, pausedPlan.Enabled)
}

func TestUpstreamAccountMonitorListReportsGlobalMonitorStateTotals(t *testing.T) {
	ctx := context.Background()
	group := &Group{ID: 1, Name: "filtered"}
	accountOne := Account{ID: 1, Name: "openai-one", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Groups: []*Group{group}}
	accountTwo := Account{ID: 2, Name: "openai-two", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Groups: []*Group{group}}
	other := Account{ID: 3, Name: "gemini-one", Type: AccountTypeAPIKey, Platform: PlatformGemini, Status: StatusActive}
	planOne := &ScheduledTestPlan{ID: 11, AccountID: accountOne.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: true}
	planTwo := &ScheduledTestPlan{ID: 12, AccountID: accountTwo.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: false}
	planOther := &ScheduledTestPlan{ID: 13, AccountID: other.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: true}
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{accountOne, accountTwo, other}},
		newUpstreamMonitorPlanRepo(planOne, planTwo, planOther),
		&upstreamMonitorResultRepo{},
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{
		Page:     1,
		PageSize: 1,
		UpstreamAccountMonitorBatchParams: UpstreamAccountMonitorBatchParams{
			Platform: PlatformOpenAI,
			GroupID:  group.ID,
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(2), resp.Total)
	require.Len(t, resp.Items, 1)
	require.Equal(t, 2, resp.MonitorEnabledTotal)
	require.Equal(t, 1, resp.MonitorDisabledTotal)
}

func TestUpstreamAccountMonitorListEnforcesSchedulableAccountMonitoring(t *testing.T) {
	ctx := context.Background()
	normal := Account{ID: 1, Name: "normal", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	monitorPlan := &ScheduledTestPlan{ID: 11, AccountID: normal.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: false}
	planRepo := newUpstreamMonitorPlanRepo(monitorPlan)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{normal}},
		planRepo,
		&upstreamMonitorResultRepo{},
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, 1, resp.MonitorEnabledTotal)
	require.Zero(t, resp.MonitorDisabledTotal)
	require.Len(t, resp.Items, 1)
	require.True(t, resp.Items[0].MonitorRequired)
	require.True(t, resp.Items[0].MonitorEnabled)
	require.True(t, monitorPlan.Enabled)
	require.NotNil(t, monitorPlan.NextRunAt)
}

func TestUpstreamAccountMonitorListKeepsRuntimeCooledScheduledAccountEnabled(t *testing.T) {
	ctx := context.Background()
	cooldownUntil := time.Now().Add(time.Hour)
	normal := Account{
		ID:                      1,
		Name:                    "normal",
		Type:                    AccountTypeAPIKey,
		Platform:                PlatformOpenAI,
		Status:                  StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &cooldownUntil,
		TempUnschedulableReason: "high latency cooldown",
	}
	require.False(t, normal.IsSchedulable(), "precondition: runtime cooldown should make the account temporarily unschedulable")
	monitorPlan := &ScheduledTestPlan{ID: 11, AccountID: normal.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: false}
	planRepo := newUpstreamMonitorPlanRepo(monitorPlan)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{normal}},
		planRepo,
		&upstreamMonitorResultRepo{},
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, 1, resp.MonitorEnabledTotal)
	require.Zero(t, resp.MonitorDisabledTotal)
	require.Len(t, resp.Items, 1)
	require.True(t, resp.Items[0].MonitorRequired)
	require.True(t, resp.Items[0].MonitorEnabled)
	require.True(t, monitorPlan.Enabled)
}

func TestUpstreamAccountMonitorListInitializesMissingPlanFromAccountSchedulableState(t *testing.T) {
	ctx := context.Background()
	normal := Account{ID: 1, Name: "normal", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	paused := Account{ID: 2, Name: "paused", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false}
	existing := &ScheduledTestPlan{ID: 11, AccountID: paused.ID, Purpose: ScheduledTestPlanPurposeUpstreamMonitor, IntervalMinutes: defaultUpstreamAccountMonitorInterval, Enabled: true}
	planRepo := newUpstreamMonitorPlanRepo(existing)
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{normal, paused}},
		planRepo,
		&upstreamMonitorResultRepo{},
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, int64(2), resp.Total)
	require.Equal(t, 2, resp.MonitorEnabledTotal)
	require.Zero(t, resp.MonitorDisabledTotal)
	normalPlans := planRepo.byAccountAndPurpose(normal.ID, ScheduledTestPlanPurposeUpstreamMonitor)
	require.Len(t, normalPlans, 1)
	require.True(t, normalPlans[0].Enabled)
	require.True(t, existing.Enabled, "existing plan must not be overwritten by account scheduling state")
}

func TestUpstreamAccountMonitorListInitializesUnschedulableAccountDisabled(t *testing.T) {
	ctx := context.Background()
	paused := Account{ID: 1, Name: "paused", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: false}
	planRepo := newUpstreamMonitorPlanRepo()
	svc := NewUpstreamAccountMonitorService(
		&upstreamMonitorAccountRepo{accounts: []Account{paused}},
		planRepo,
		&upstreamMonitorResultRepo{},
		nil,
	)

	resp, err := svc.List(ctx, UpstreamAccountMonitorListParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, 0, resp.MonitorEnabledTotal)
	require.Equal(t, 1, resp.MonitorDisabledTotal)
	plans := planRepo.byAccountAndPurpose(paused.ID, ScheduledTestPlanPurposeUpstreamMonitor)
	require.Len(t, plans, 1)
	require.False(t, plans[0].Enabled)
}

type upstreamMonitorAccountRepo struct {
	AccountRepository
	accounts []Account
	calls    []upstreamMonitorAccountRepoCall
}

type upstreamMonitorAccountRepoCall struct {
	Platform string
	Status   string
	Search   string
	GroupID  int64
}

func (r *upstreamMonitorAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, sql.ErrNoRows
}

func (r *upstreamMonitorAccountRepo) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform string, accountType string, status string, search string, groupID int64, _ string) ([]Account, *pagination.PaginationResult, error) {
	r.calls = append(r.calls, upstreamMonitorAccountRepoCall{
		Platform: platform,
		Status:   status,
		Search:   search,
		GroupID:  groupID,
	})
	var filtered []Account
	for _, account := range r.accounts {
		if accountType != "" && account.Type != accountType {
			continue
		}
		if platform != "" && account.Platform != platform {
			continue
		}
		if status != "" && account.Status != status {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(account.Name), strings.ToLower(search)) {
			continue
		}
		if groupID != 0 && !accountHasGroup(account, groupID) {
			continue
		}
		filtered = append(filtered, account)
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = len(filtered)
	}
	total := len(filtered)
	pages := 0
	if params.PageSize > 0 {
		pages = (total + params.PageSize - 1) / params.PageSize
	}
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	return filtered[start:end], &pagination.PaginationResult{
		Page:     params.Page,
		PageSize: params.PageSize,
		Total:    int64(total),
		Pages:    pages,
	}, nil
}

func accountHasGroup(account Account, groupID int64) bool {
	if groupID == AccountListGroupUngrouped {
		return len(account.Groups) == 0 && len(account.GroupIDs) == 0
	}
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, group := range account.Groups {
		if group != nil && group.ID == groupID {
			return true
		}
	}
	return false
}

type upstreamMonitorPlanRepo struct {
	ScheduledTestPlanRepository
	nextID int64
	plans  map[int64]*ScheduledTestPlan
}

func newUpstreamMonitorPlanRepo(plans ...*ScheduledTestPlan) *upstreamMonitorPlanRepo {
	repo := &upstreamMonitorPlanRepo{
		nextID: 100,
		plans:  make(map[int64]*ScheduledTestPlan),
	}
	for _, plan := range plans {
		repo.plans[plan.ID] = plan
		if plan.ID >= repo.nextID {
			repo.nextID = plan.ID + 1
		}
	}
	return repo
}

func (r *upstreamMonitorPlanRepo) Create(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	if plan.Purpose == "" {
		plan.Purpose = ScheduledTestPlanPurposeScheduledTest
	}
	plan.ID = r.nextID
	r.nextID++
	r.plans[plan.ID] = plan
	return plan, nil
}

func (r *upstreamMonitorPlanRepo) GetByID(_ context.Context, id int64) (*ScheduledTestPlan, error) {
	plan := r.plans[id]
	if plan == nil {
		return nil, sql.ErrNoRows
	}
	return plan, nil
}

func (r *upstreamMonitorPlanRepo) ListByAccountID(_ context.Context, accountID int64) ([]*ScheduledTestPlan, error) {
	return r.byAccountAndPurpose(accountID, ScheduledTestPlanPurposeScheduledTest), nil
}

func (r *upstreamMonitorPlanRepo) ListByAccountIDs(_ context.Context, accountIDs []int64) (map[int64][]*ScheduledTestPlan, error) {
	out := make(map[int64][]*ScheduledTestPlan, len(accountIDs))
	for _, accountID := range accountIDs {
		for _, plan := range r.plans {
			if plan.AccountID == accountID {
				out[accountID] = append(out[accountID], plan)
			}
		}
	}
	return out, nil
}

func (r *upstreamMonitorPlanRepo) ListByAccountIDAndPurpose(_ context.Context, accountID int64, purpose string) ([]*ScheduledTestPlan, error) {
	return r.byAccountAndPurpose(accountID, purpose), nil
}

func (r *upstreamMonitorPlanRepo) ListByAccountIDsAndPurpose(_ context.Context, accountIDs []int64, purpose string) (map[int64][]*ScheduledTestPlan, error) {
	out := make(map[int64][]*ScheduledTestPlan, len(accountIDs))
	for _, accountID := range accountIDs {
		out[accountID] = r.byAccountAndPurpose(accountID, purpose)
	}
	return out, nil
}

func (r *upstreamMonitorPlanRepo) ListDue(_ context.Context, _ time.Time) ([]*ScheduledTestPlan, error) {
	return nil, nil
}

func (r *upstreamMonitorPlanRepo) Update(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	if plan.Purpose == "" {
		plan.Purpose = ScheduledTestPlanPurposeScheduledTest
	}
	r.plans[plan.ID] = plan
	return plan, nil
}

func (r *upstreamMonitorPlanRepo) Delete(_ context.Context, id int64) error {
	delete(r.plans, id)
	return nil
}

func (r *upstreamMonitorPlanRepo) UpdateAfterRun(_ context.Context, _ int64, _ time.Time, _ time.Time) error {
	return nil
}

func (r *upstreamMonitorPlanRepo) byAccountAndPurpose(accountID int64, purpose string) []*ScheduledTestPlan {
	var out []*ScheduledTestPlan
	for _, plan := range r.plans {
		if plan.AccountID == accountID && plan.Purpose == purpose {
			out = append(out, plan)
		}
	}
	return out
}

type upstreamMonitorResultRepo struct {
	ScheduledTestResultRepository
	statsSequence []map[int64]*ScheduledTestPlanStats
	statsSince    []time.Time
	latest        map[int64]*ScheduledTestResult
	recent        map[int64][]*ScheduledTestResult
}

func (r *upstreamMonitorResultRepo) ListLatestByPlanIDs(_ context.Context, planIDs []int64) (map[int64]*ScheduledTestResult, error) {
	if r.latest != nil {
		return r.latest, nil
	}
	return make(map[int64]*ScheduledTestResult, len(planIDs)), nil
}

func (r *upstreamMonitorResultRepo) ListRecentByPlanIDs(_ context.Context, planIDs []int64, _ int) (map[int64][]*ScheduledTestResult, error) {
	if r.recent != nil {
		return r.recent, nil
	}
	return make(map[int64][]*ScheduledTestResult, len(planIDs)), nil
}

func (r *upstreamMonitorResultRepo) Stats7dByPlanIDs(_ context.Context, _ []int64, since time.Time) (map[int64]*ScheduledTestPlanStats, error) {
	r.statsSince = append(r.statsSince, since)
	if len(r.statsSequence) == 0 {
		return map[int64]*ScheduledTestPlanStats{}, nil
	}
	stats := r.statsSequence[0]
	r.statsSequence = r.statsSequence[1:]
	return stats, nil
}
