package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"golang.org/x/sync/errgroup"
)

const (
	defaultUpstreamAccountMonitorCron       = "0 * * * *"
	defaultUpstreamAccountMonitorInterval   = 60
	defaultUpstreamAccountMonitorMaxResults = 200
	upstreamAccountMonitorBatchSize         = 1000
	upstreamAccountMonitorRunConcurrency    = 5
	upstreamAccountMonitorTimelineLimit     = 60
	upstreamAccountMonitorMinInterval       = 1
	upstreamAccountMonitorMaxInterval       = 1440
)

type UpstreamAccountMonitorListParams struct {
	Page     int
	PageSize int
	UpstreamAccountMonitorBatchParams
}

type UpstreamAccountMonitorBatchParams struct {
	Platform string
	Status   string
	Search   string
	GroupID  int64
}

type UpstreamAccountMonitorItem struct {
	AccountID       int64                      `json:"account_id"`
	AccountName     string                     `json:"account_name"`
	Platform        string                     `json:"platform"`
	AccountStatus   string                     `json:"account_status"`
	ErrorMessage    string                     `json:"error_message"`
	MonitorRequired bool                       `json:"monitor_required"`
	Groups          []UpstreamMonitorGroupView `json:"groups"`
	MonitorEnabled  bool                       `json:"monitor_enabled"`
	PlanID          *int64                     `json:"plan_id"`
	ModelID         string                     `json:"model_id"`
	CronExpression  string                     `json:"cron_expression"`
	IntervalMinutes int                        `json:"interval_minutes"`
	JitterSeconds   int                        `json:"jitter_seconds"`
	AutoRecover     bool                       `json:"auto_recover"`
	LastRunAt       *time.Time                 `json:"last_run_at"`
	NextRunAt       *time.Time                 `json:"next_run_at"`
	LatestResult    *ScheduledTestResult       `json:"latest_result"`
	Availability7d  *float64                   `json:"availability_7d"`
	Availability15d *float64                   `json:"availability_15d"`
	Timeline        []UpstreamMonitorTimeline  `json:"timeline"`
}

type UpstreamMonitorGroupView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type UpstreamMonitorTimeline struct {
	Status        string    `json:"status"`
	LatencyMs     *int64    `json:"latency_ms"`
	PingLatencyMs *int64    `json:"ping_latency_ms"`
	CheckedAt     time.Time `json:"checked_at"`
}

type UpstreamAccountMonitorListResponse struct {
	Items                []UpstreamAccountMonitorItem `json:"items"`
	Total                int64                        `json:"total"`
	Page                 int                          `json:"page"`
	PageSize             int                          `json:"page_size"`
	Pages                int                          `json:"pages"`
	MonitorEnabledTotal  int                          `json:"monitor_enabled_total"`
	MonitorDisabledTotal int                          `json:"monitor_disabled_total"`
}

type UpstreamAccountMonitorBatchResponse struct {
	Total    int `json:"total"`
	Created  int `json:"created,omitempty"`
	Updated  int `json:"updated,omitempty"`
	Enabled  int `json:"enabled,omitempty"`
	Disabled int `json:"disabled,omitempty"`
	Success  int `json:"success,omitempty"`
	Failed   int `json:"failed,omitempty"`
	Skipped  int `json:"skipped,omitempty"`
}

type UpstreamAccountMonitorSettingsRequest struct {
	ModelID         string `json:"model_id"`
	IntervalMinutes int    `json:"interval_minutes"`
	JitterSeconds   int    `json:"jitter_seconds"`
	AutoRecover     bool   `json:"auto_recover"`
	MonitorEnabled  *bool  `json:"monitor_enabled,omitempty"`
}

type UpstreamAccountMonitorRunResult struct {
	AccountID int64                `json:"account_id"`
	PlanID    int64                `json:"plan_id"`
	Result    *ScheduledTestResult `json:"result"`
	Error     string               `json:"error,omitempty"`
}

type UpstreamAccountMonitorService struct {
	accountRepo AccountRepository
	planRepo    ScheduledTestPlanRepository
	resultRepo  ScheduledTestResultRepository
	testSvc     *AccountTestService
	recoverer   upstreamAccountRecoverer
}

func NewUpstreamAccountMonitorService(
	accountRepo AccountRepository,
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
	testSvc *AccountTestService,
) *UpstreamAccountMonitorService {
	return &UpstreamAccountMonitorService{
		accountRepo: accountRepo,
		planRepo:    planRepo,
		resultRepo:  resultRepo,
		testSvc:     testSvc,
	}
}

type upstreamAccountRecoverer interface {
	RecoverAccountAfterSuccessfulTest(ctx context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error)
}

func ProvideUpstreamAccountMonitorService(
	accountRepo AccountRepository,
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
	testSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
) *UpstreamAccountMonitorService {
	svc := NewUpstreamAccountMonitorService(accountRepo, planRepo, resultRepo, testSvc)
	svc.SetRecoverer(rateLimitSvc)
	return svc
}

func (s *UpstreamAccountMonitorService) SetRecoverer(recoverer upstreamAccountRecoverer) {
	if s == nil {
		return
	}
	s.recoverer = recoverer
}

func (s *UpstreamAccountMonitorService) List(ctx context.Context, params UpstreamAccountMonitorListParams) (*UpstreamAccountMonitorListResponse, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	accounts, pageResult, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    "name",
		SortOrder: pagination.SortOrderAsc,
	}, params.Platform, AccountTypeAPIKey, params.Status, params.Search, params.GroupID, "")
	if err != nil {
		return nil, err
	}

	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	plansByAccount, err := s.planRepo.ListByAccountIDsAndPurpose(ctx, accountIDs, ScheduledTestPlanPurposeUpstreamMonitor)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDefaultPlansForAccounts(ctx, accounts, plansByAccount); err != nil {
		return nil, err
	}

	planIDs := make([]int64, 0, len(accounts))
	primaryPlans := make(map[int64]*ScheduledTestPlan, len(accounts))
	for _, account := range accounts {
		plan := selectMonitorPlan(plansByAccount[account.ID])
		if plan == nil {
			continue
		}
		primaryPlans[account.ID] = plan
		planIDs = append(planIDs, plan.ID)
	}

	latestByPlan, err := s.resultRepo.ListLatestByPlanIDs(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	stats7dByPlan, err := s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -7))
	if err != nil {
		return nil, err
	}
	stats15dByPlan, err := s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -15))
	if err != nil {
		return nil, err
	}
	timelineByPlan, err := s.resultRepo.ListRecentByPlanIDs(ctx, planIDs, upstreamAccountMonitorTimelineLimit)
	if err != nil {
		return nil, err
	}
	enabledTotal, disabledTotal, err := s.monitorStateTotals(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]UpstreamAccountMonitorItem, 0, len(accounts))
	for _, account := range accounts {
		item := UpstreamAccountMonitorItem{
			AccountID:       account.ID,
			AccountName:     account.Name,
			Platform:        account.Platform,
			AccountStatus:   account.Status,
			ErrorMessage:    account.ErrorMessage,
			MonitorRequired: monitorRequired(&account),
			Groups:          monitorGroupViews(account.Groups),
			MonitorEnabled:  hasEnabledPlan(plansByAccount[account.ID]),
		}
		if plan := primaryPlans[account.ID]; plan != nil {
			item.PlanID = &plan.ID
			item.ModelID = plan.ModelID
			item.CronExpression = plan.CronExpression
			item.IntervalMinutes = monitorIntervalMinutes(plan)
			item.JitterSeconds = plan.JitterSeconds
			item.AutoRecover = plan.AutoRecover
			item.LastRunAt = plan.LastRunAt
			item.NextRunAt = plan.NextRunAt
			item.LatestResult = latestByPlan[plan.ID]
			if stats := stats7dByPlan[plan.ID]; stats != nil && stats.Total > 0 {
				availability := stats.Availability7d
				item.Availability7d = &availability
			}
			if stats := stats15dByPlan[plan.ID]; stats != nil && stats.Total > 0 {
				availability := stats.Availability7d
				item.Availability15d = &availability
			}
			item.Timeline = monitorTimelineViews(timelineByPlan[plan.ID])
		}
		items = append(items, item)
	}

	return &UpstreamAccountMonitorListResponse{
		Items:                items,
		Total:                pageResult.Total,
		Page:                 pageResult.Page,
		PageSize:             pageResult.PageSize,
		Pages:                pageResult.Pages,
		MonitorEnabledTotal:  enabledTotal,
		MonitorDisabledTotal: disabledTotal,
	}, nil
}

func (s *UpstreamAccountMonitorService) monitorStateTotals(ctx context.Context) (enabled int, disabled int, err error) {
	accounts, err := s.listAllAPIKeyAccounts(ctx, UpstreamAccountMonitorBatchParams{})
	if err != nil {
		return 0, 0, err
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	plansByAccount, err := s.planRepo.ListByAccountIDsAndPurpose(ctx, accountIDs, ScheduledTestPlanPurposeUpstreamMonitor)
	if err != nil {
		return 0, 0, err
	}
	if err := s.ensureDefaultPlansForAccounts(ctx, accounts, plansByAccount); err != nil {
		return 0, 0, err
	}
	for _, account := range accounts {
		if hasEnabledPlan(plansByAccount[account.ID]) {
			enabled++
		} else {
			disabled++
		}
	}
	return enabled, disabled, nil
}

func (s *UpstreamAccountMonitorService) EnableAll(ctx context.Context, params UpstreamAccountMonitorBatchParams) (*UpstreamAccountMonitorBatchResponse, error) {
	return s.updateAllMonitoring(ctx, UpstreamAccountMonitorBatchParams{}, true)
}

func (s *UpstreamAccountMonitorService) DisableAll(ctx context.Context, params UpstreamAccountMonitorBatchParams) (*UpstreamAccountMonitorBatchResponse, error) {
	return s.updateAllMonitoring(ctx, UpstreamAccountMonitorBatchParams{}, false)
}

func (s *UpstreamAccountMonitorService) RunOne(ctx context.Context, accountID int64) (*UpstreamAccountMonitorRunResult, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("account is not an API key account")
	}
	plan, _, err := s.ensureDefaultPlan(ctx, account.ID, defaultMonitorEnabled(account))
	if err != nil {
		return nil, err
	}
	result, err := s.runAndSave(ctx, plan)
	if err != nil {
		return &UpstreamAccountMonitorRunResult{AccountID: account.ID, PlanID: plan.ID, Result: result, Error: err.Error()}, nil
	}
	return &UpstreamAccountMonitorRunResult{AccountID: account.ID, PlanID: plan.ID, Result: result}, nil
}

func (s *UpstreamAccountMonitorService) UpdateSettings(ctx context.Context, accountID int64, req UpstreamAccountMonitorSettingsRequest) (*UpstreamAccountMonitorItem, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Type != AccountTypeAPIKey {
		return nil, fmt.Errorf("account is not an API key account")
	}
	if req.IntervalMinutes < upstreamAccountMonitorMinInterval || req.IntervalMinutes > upstreamAccountMonitorMaxInterval {
		return nil, fmt.Errorf("interval_minutes must be between %d and %d", upstreamAccountMonitorMinInterval, upstreamAccountMonitorMaxInterval)
	}
	if req.JitterSeconds < 0 || req.JitterSeconds >= req.IntervalMinutes*60 {
		return nil, fmt.Errorf("jitter_seconds must be >= 0 and less than interval seconds")
	}
	if req.MonitorEnabled != nil && !*req.MonitorEnabled && monitorRequired(account) {
		return nil, fmt.Errorf("account monitoring is required while account scheduling is enabled")
	}

	plan, _, err := s.ensureDefaultPlan(ctx, account.ID, defaultMonitorEnabled(account))
	if err != nil {
		return nil, err
	}
	plan.ModelID = strings.TrimSpace(req.ModelID)
	plan.CronExpression = cronFromIntervalMinutes(req.IntervalMinutes)
	plan.IntervalMinutes = req.IntervalMinutes
	plan.JitterSeconds = req.JitterSeconds
	plan.AutoRecover = req.AutoRecover
	if req.MonitorEnabled != nil {
		plan.Enabled = *req.MonitorEnabled
	}
	if monitorRequired(account) {
		plan.Enabled = true
	}
	nextRun, err := computeScheduledTestNextRun(plan, time.Now())
	if err != nil {
		return nil, err
	}
	plan.NextRunAt = &nextRun
	if _, err := s.planRepo.Update(ctx, plan); err != nil {
		return nil, err
	}

	return &UpstreamAccountMonitorItem{
		AccountID:       account.ID,
		AccountName:     account.Name,
		Platform:        account.Platform,
		AccountStatus:   account.Status,
		ErrorMessage:    account.ErrorMessage,
		MonitorRequired: monitorRequired(account),
		Groups:          monitorGroupViews(account.Groups),
		MonitorEnabled:  plan.Enabled,
		PlanID:          &plan.ID,
		ModelID:         plan.ModelID,
		CronExpression:  plan.CronExpression,
		IntervalMinutes: monitorIntervalMinutes(plan),
		JitterSeconds:   plan.JitterSeconds,
		AutoRecover:     plan.AutoRecover,
		LastRunAt:       plan.LastRunAt,
		NextRunAt:       plan.NextRunAt,
	}, nil
}

func (s *UpstreamAccountMonitorService) RunAll(ctx context.Context, params UpstreamAccountMonitorBatchParams) (*UpstreamAccountMonitorBatchResponse, error) {
	accounts, err := s.listAllAPIKeyAccounts(ctx, params)
	if err != nil {
		return nil, err
	}

	resp := &UpstreamAccountMonitorBatchResponse{Total: len(accounts)}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(upstreamAccountMonitorRunConcurrency)
	for _, account := range accounts {
		acc := account
		g.Go(func() error {
			plan, _, err := s.ensureDefaultPlan(gctx, acc.ID, defaultMonitorEnabled(&acc))
			if err != nil {
				mu.Lock()
				resp.Failed++
				mu.Unlock()
				return nil
			}
			result, err := s.runAndSave(gctx, plan)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || result == nil || result.Status != "success" {
				resp.Failed++
				return nil
			}
			resp.Success++
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *UpstreamAccountMonitorService) updateAllMonitoring(ctx context.Context, params UpstreamAccountMonitorBatchParams, enabled bool) (*UpstreamAccountMonitorBatchResponse, error) {
	accounts, err := s.listAllAPIKeyAccounts(ctx, params)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	plansByAccount, err := s.planRepo.ListByAccountIDsAndPurpose(ctx, accountIDs, ScheduledTestPlanPurposeUpstreamMonitor)
	if err != nil {
		return nil, err
	}

	resp := &UpstreamAccountMonitorBatchResponse{Total: len(accounts)}
	for _, account := range accounts {
		plans := plansByAccount[account.ID]
		if !enabled && monitorRequired(&account) {
			if plan, _, err := s.ensureDefaultPlan(ctx, account.ID, true); err != nil {
				return nil, err
			} else if plan != nil && !plan.Enabled {
				plan.Enabled = true
				nextRun, err := computeScheduledTestNextRun(plan, time.Now())
				if err != nil {
					return nil, err
				}
				plan.NextRunAt = &nextRun
				if _, err := s.planRepo.Update(ctx, plan); err != nil {
					return nil, err
				}
			}
			resp.Skipped++
			continue
		}
		if len(plans) == 0 {
			if !enabled {
				resp.Skipped++
				continue
			}
			if _, _, err := s.ensureDefaultPlan(ctx, account.ID, true); err != nil {
				return nil, err
			}
			resp.Created++
			resp.Enabled++
			continue
		}
		changed := false
		for _, plan := range plans {
			if plan.Enabled == enabled {
				continue
			}
			plan.Enabled = enabled
			if enabled {
				nextRun, err := computeScheduledTestNextRun(plan, time.Now())
				if err != nil {
					return nil, err
				}
				plan.NextRunAt = &nextRun
			}
			if _, err := s.planRepo.Update(ctx, plan); err != nil {
				return nil, err
			}
			changed = true
			resp.Updated++
		}
		if enabled && changed {
			resp.Enabled++
		}
		if !enabled && changed {
			resp.Disabled++
		}
		if !changed {
			resp.Skipped++
		}
	}
	return resp, nil
}

func (s *UpstreamAccountMonitorService) ensureDefaultPlansForAccounts(ctx context.Context, accounts []Account, plansByAccount map[int64][]*ScheduledTestPlan) error {
	for i := range accounts {
		account := &accounts[i]
		plan := selectMonitorPlan(plansByAccount[account.ID])
		if plan != nil {
			if monitorRequired(account) && !hasEnabledPlan(plansByAccount[account.ID]) {
				plan.Enabled = true
				nextRun, err := computeScheduledTestNextRun(plan, time.Now())
				if err != nil {
					return err
				}
				plan.NextRunAt = &nextRun
				if _, err := s.planRepo.Update(ctx, plan); err != nil {
					return err
				}
			}
			continue
		}
		plan, _, err := s.ensureDefaultPlan(ctx, account.ID, defaultMonitorEnabled(account))
		if err != nil {
			return err
		}
		if plan != nil {
			plansByAccount[account.ID] = append(plansByAccount[account.ID], plan)
		}
	}
	return nil
}

func defaultMonitorEnabled(account *Account) bool {
	return monitorRequired(account)
}

func monitorRequired(account *Account) bool {
	if account == nil {
		return false
	}
	return account.Schedulable
}

func (s *UpstreamAccountMonitorService) ensureDefaultPlan(ctx context.Context, accountID int64, defaultEnabled bool) (*ScheduledTestPlan, bool, error) {
	plans, err := s.planRepo.ListByAccountIDAndPurpose(ctx, accountID, ScheduledTestPlanPurposeUpstreamMonitor)
	if err != nil {
		return nil, false, err
	}
	if plan := selectMonitorPlan(plans); plan != nil {
		return plan, false, nil
	}
	plan := &ScheduledTestPlan{
		AccountID:       accountID,
		Purpose:         ScheduledTestPlanPurposeUpstreamMonitor,
		ModelID:         "",
		CronExpression:  defaultUpstreamAccountMonitorCron,
		IntervalMinutes: defaultUpstreamAccountMonitorInterval,
		Enabled:         defaultEnabled,
		MaxResults:      defaultUpstreamAccountMonitorMaxResults,
		AutoRecover:     false,
		JitterSeconds:   0,
	}
	nextRun, err := computeScheduledTestNextRun(plan, time.Now())
	if err != nil {
		return nil, false, err
	}
	plan.NextRunAt = &nextRun
	created, err := s.planRepo.Create(ctx, plan)
	return created, true, err
}

func (s *UpstreamAccountMonitorService) runAndSave(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestResult, error) {
	result, err := s.testSvc.RunTestBackgroundWithEndpointPing(ctx, plan.AccountID, plan.ModelID)
	if result != nil {
		if _, saveErr := s.resultRepo.Create(ctx, &ScheduledTestResult{
			PlanID:        plan.ID,
			Status:        result.Status,
			ResponseText:  result.ResponseText,
			ErrorMessage:  result.ErrorMessage,
			LatencyMs:     result.LatencyMs,
			PingLatencyMs: result.PingLatencyMs,
			StartedAt:     result.StartedAt,
			FinishedAt:    result.FinishedAt,
		}); saveErr != nil {
			return result, saveErr
		}
		_ = s.resultRepo.PruneOldResults(ctx, plan.ID, plan.MaxResults)
		if result.Status == "success" && plan.AutoRecover {
			s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
		}
		nextRun, nextErr := computeScheduledTestNextRun(plan, time.Now())
		if nextErr == nil {
			_ = s.planRepo.UpdateAfterRun(ctx, plan.ID, time.Now(), nextRun)
		}
	}
	return result, err
}

func (s *UpstreamAccountMonitorService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s == nil || s.recoverer == nil {
		return
	}

	recovery, err := s.recoverer.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.upstream_account_monitor", "[UpstreamAccountMonitor] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}
	if recovery.ClearedError {
		logger.LegacyPrintf("service.upstream_account_monitor", "[UpstreamAccountMonitor] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.upstream_account_monitor", "[UpstreamAccountMonitor] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}

func (s *UpstreamAccountMonitorService) listAllAPIKeyAccounts(ctx context.Context, params UpstreamAccountMonitorBatchParams) ([]Account, error) {
	var all []Account
	for page := 1; ; page++ {
		accounts, result, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{
			Page:      page,
			PageSize:  upstreamAccountMonitorBatchSize,
			SortBy:    "name",
			SortOrder: pagination.SortOrderAsc,
		}, params.Platform, AccountTypeAPIKey, params.Status, params.Search, params.GroupID, "")
		if err != nil {
			return nil, err
		}
		all = append(all, accounts...)
		if result == nil || page >= result.Pages || len(accounts) == 0 {
			break
		}
	}
	return all, nil
}

func selectMonitorPlan(plans []*ScheduledTestPlan) *ScheduledTestPlan {
	if len(plans) == 0 {
		return nil
	}
	for _, plan := range plans {
		if plan != nil && plan.Enabled && plan.ModelID == "" {
			return plan
		}
	}
	for _, plan := range plans {
		if plan != nil && plan.Enabled {
			return plan
		}
	}
	for _, plan := range plans {
		if plan != nil && plan.ModelID == "" {
			return plan
		}
	}
	return plans[0]
}

func hasEnabledPlan(plans []*ScheduledTestPlan) bool {
	for _, plan := range plans {
		if plan != nil && plan.Enabled {
			return true
		}
	}
	return false
}

func monitorGroupViews(groups []*Group) []UpstreamMonitorGroupView {
	out := make([]UpstreamMonitorGroupView, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		out = append(out, UpstreamMonitorGroupView{ID: group.ID, Name: group.Name})
	}
	return out
}

func monitorTimelineViews(results []*ScheduledTestResult) []UpstreamMonitorTimeline {
	out := make([]UpstreamMonitorTimeline, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		latency := result.LatencyMs
		pingLatency := result.PingLatencyMs
		out = append(out, UpstreamMonitorTimeline{
			Status:        monitorTimelineStatus(result.Status),
			LatencyMs:     &latency,
			PingLatencyMs: pingLatency,
			CheckedAt:     result.CreatedAt,
		})
	}
	return out
}

func monitorTimelineStatus(status string) string {
	if status == "success" {
		return "operational"
	}
	return "failed"
}

func cronFromIntervalMinutes(minutes int) string {
	// Upstream monitor scheduling is driven by interval_minutes. Keep cron_expression
	// valid for legacy display/fallback paths without trying to encode intervals
	// that five-field cron cannot represent exactly, such as 90 or 1440 minutes.
	return defaultUpstreamAccountMonitorCron
}

func intervalMinutesFromCron(expr string) int {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return defaultUpstreamAccountMonitorInterval
	}
	if strings.HasPrefix(parts[0], "*/") && parts[1] == "*" {
		if n, err := strconv.Atoi(strings.TrimPrefix(parts[0], "*/")); err == nil && n > 0 {
			return n
		}
	}
	if parts[0] == "0" && strings.HasPrefix(parts[1], "*/") {
		if n, err := strconv.Atoi(strings.TrimPrefix(parts[1], "*/")); err == nil && n > 0 {
			return n * 60
		}
	}
	if parts[0] == "0" && parts[1] == "*" {
		return 60
	}
	return defaultUpstreamAccountMonitorInterval
}

func monitorIntervalMinutes(plan *ScheduledTestPlan) int {
	if plan != nil && plan.IntervalMinutes > 0 {
		return plan.IntervalMinutes
	}
	if plan == nil {
		return defaultUpstreamAccountMonitorInterval
	}
	return intervalMinutesFromCron(plan.CronExpression)
}
