package service

import (
	"context"
	"fmt"
	"sort"
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
	upstreamAccountMonitorDegradedLatencyMs = 6000
)

type UpstreamAccountMonitorListParams struct {
	Page               int
	PageSize           int
	SortBy             string
	SortOrder          string
	AvailabilityWindow string
	UpstreamAccountMonitorBatchParams
}

type UpstreamAccountMonitorBatchParams struct {
	Platform      string
	Status        string
	MonitorStatus string
	Search        string
	GroupID       int64
}

type UpstreamAccountMonitorBatchSettingsRequest struct {
	MonitorEnabled  *bool `json:"monitor_enabled,omitempty"`
	IntervalMinutes *int  `json:"interval_minutes,omitempty"`
	JitterSeconds   *int  `json:"jitter_seconds,omitempty"`
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

type UpstreamAccountMonitorRunAllStreamEvent struct {
	Type      string               `json:"type"`
	Total     int                  `json:"total,omitempty"`
	AccountID int64                `json:"account_id,omitempty"`
	PlanID    int64                `json:"plan_id,omitempty"`
	Result    *ScheduledTestResult `json:"result,omitempty"`
	Error     string               `json:"error,omitempty"`
	Success   int                  `json:"success,omitempty"`
	Failed    int                  `json:"failed,omitempty"`
	Skipped   int                  `json:"skipped,omitempty"`
}

type UpstreamAccountMonitorService struct {
	accountRepo AccountRepository
	planRepo    ScheduledTestPlanRepository
	resultRepo  ScheduledTestResultRepository
	testSvc     upstreamAccountMonitorTester
	recoverer   upstreamAccountRecoverer
}

func NewUpstreamAccountMonitorService(
	accountRepo AccountRepository,
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
	testSvc upstreamAccountMonitorTester,
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

type upstreamAccountMonitorTester interface {
	RunTestBackgroundWithEndpointPing(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
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
	pageSize = pagination.PaginationParams{PageSize: pageSize}.Limit()
	now := time.Now()
	listData, err := s.listMonitorAccounts(ctx, params, page, pageSize, now)
	if err != nil {
		return nil, err
	}

	planIDs := monitorPlanIDsForAccounts(listData.Accounts, listData.PrimaryPlans)
	stats7dByPlan := listData.Stats7dByPlan
	if stats7dByPlan == nil {
		stats7dByPlan, err = s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -7))
		if err != nil {
			return nil, err
		}
	}
	stats15dByPlan := listData.Stats15dByPlan
	if stats15dByPlan == nil {
		stats15dByPlan, err = s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -15))
		if err != nil {
			return nil, err
		}
	}
	timelineByPlan, err := s.resultRepo.ListRecentByPlanIDs(ctx, planIDs, upstreamAccountMonitorTimelineLimit)
	if err != nil {
		return nil, err
	}
	enabledTotal, disabledTotal, err := s.monitorStateTotals(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]UpstreamAccountMonitorItem, 0, len(listData.Accounts))
	for _, account := range listData.Accounts {
		item := UpstreamAccountMonitorItem{
			AccountID:       account.ID,
			AccountName:     account.Name,
			Platform:        account.Platform,
			AccountStatus:   account.Status,
			ErrorMessage:    account.ErrorMessage,
			MonitorRequired: monitorRequired(&account),
			Groups:          monitorGroupViews(account.Groups),
			MonitorEnabled:  hasEnabledPlan(listData.PlansByAccount[account.ID]),
		}
		if plan := listData.PrimaryPlans[account.ID]; plan != nil {
			item.PlanID = &plan.ID
			item.ModelID = plan.ModelID
			item.CronExpression = plan.CronExpression
			item.IntervalMinutes = monitorIntervalMinutes(plan)
			item.JitterSeconds = plan.JitterSeconds
			item.AutoRecover = plan.AutoRecover
			item.LastRunAt = plan.LastRunAt
			item.NextRunAt = plan.NextRunAt
			item.LatestResult = listData.LatestByPlan[plan.ID]
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
		Total:                listData.PageResult.Total,
		Page:                 listData.PageResult.Page,
		PageSize:             listData.PageResult.PageSize,
		Pages:                listData.PageResult.Pages,
		MonitorEnabledTotal:  enabledTotal,
		MonitorDisabledTotal: disabledTotal,
	}, nil
}

type upstreamMonitorAccountListData struct {
	Accounts       []Account
	PageResult     *pagination.PaginationResult
	PlansByAccount map[int64][]*ScheduledTestPlan
	PrimaryPlans   map[int64]*ScheduledTestPlan
	LatestByPlan   map[int64]*ScheduledTestResult
	Stats7dByPlan  map[int64]*ScheduledTestPlanStats
	Stats15dByPlan map[int64]*ScheduledTestPlanStats
}

func (s *UpstreamAccountMonitorService) listMonitorAccounts(ctx context.Context, params UpstreamAccountMonitorListParams, page int, pageSize int, now time.Time) (*upstreamMonitorAccountListData, error) {
	monitorStatus := strings.TrimSpace(params.MonitorStatus)
	sortByAvailability := strings.TrimSpace(params.SortBy) == "availability"
	if monitorStatus == "" && !sortByAvailability {
		accounts, pageResult, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortBy:    "name",
			SortOrder: pagination.SortOrderAsc,
		}, params.Platform, AccountTypeAPIKey, params.Status, params.Search, params.GroupID, "")
		if err != nil {
			return nil, err
		}
		plansByAccount, primaryPlans, planIDs, err := s.loadPrimaryMonitorPlans(ctx, accounts)
		if err != nil {
			return nil, err
		}
		latestByPlan, err := s.resultRepo.ListLatestByPlanIDs(ctx, planIDs)
		if err != nil {
			return nil, err
		}
		return &upstreamMonitorAccountListData{
			Accounts:       accounts,
			PageResult:     pageResult,
			PlansByAccount: plansByAccount,
			PrimaryPlans:   primaryPlans,
			LatestByPlan:   latestByPlan,
		}, nil
	}

	accounts, err := s.listAllAPIKeyAccounts(ctx, params.UpstreamAccountMonitorBatchParams)
	if err != nil {
		return nil, err
	}
	plansByAccount, primaryPlans, planIDs, err := s.loadPrimaryMonitorPlans(ctx, accounts)
	if err != nil {
		return nil, err
	}
	latestByPlan, err := s.resultRepo.ListLatestByPlanIDs(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	accounts = filterAccountsByMonitorStatusData(accounts, primaryPlans, latestByPlan, monitorStatus)

	data := &upstreamMonitorAccountListData{
		PlansByAccount: plansByAccount,
		PrimaryPlans:   primaryPlans,
		LatestByPlan:   latestByPlan,
	}
	if sortByAvailability {
		window := normalizeUpstreamMonitorAvailabilityWindow(params.AvailabilityWindow)
		availabilityStats, err := s.loadAvailabilityStats(ctx, monitorPlanIDsForAccounts(accounts, primaryPlans), window, now)
		if err != nil {
			return nil, err
		}
		sortUpstreamMonitorAccountsByAvailability(accounts, primaryPlans, availabilityStats, params.SortOrder)
		if window == "15d" {
			data.Stats15dByPlan = availabilityStats
		} else {
			data.Stats7dByPlan = availabilityStats
		}
	}
	data.PageResult = upstreamMonitorPaginationResult(int64(len(accounts)), page, pageSize)
	data.Accounts = paginateUpstreamMonitorAccounts(accounts, page, pageSize)
	return data, nil
}

func (s *UpstreamAccountMonitorService) loadPrimaryMonitorPlans(ctx context.Context, accounts []Account) (map[int64][]*ScheduledTestPlan, map[int64]*ScheduledTestPlan, []int64, error) {
	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		accountIDs = append(accountIDs, accounts[i].ID)
	}
	plansByAccount, err := s.planRepo.ListByAccountIDsAndPurpose(ctx, accountIDs, ScheduledTestPlanPurposeUpstreamMonitor)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := s.ensureDefaultPlansForAccounts(ctx, accounts, plansByAccount); err != nil {
		return nil, nil, nil, err
	}

	primaryPlans := make(map[int64]*ScheduledTestPlan, len(accounts))
	planIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		plan := selectMonitorPlan(plansByAccount[account.ID])
		if plan == nil {
			continue
		}
		primaryPlans[account.ID] = plan
		planIDs = append(planIDs, plan.ID)
	}
	return plansByAccount, primaryPlans, planIDs, nil
}

func monitorPlanIDsForAccounts(accounts []Account, primaryPlans map[int64]*ScheduledTestPlan) []int64 {
	planIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		if plan := primaryPlans[account.ID]; plan != nil {
			planIDs = append(planIDs, plan.ID)
		}
	}
	return planIDs
}

func filterAccountsByMonitorStatusData(accounts []Account, primaryPlans map[int64]*ScheduledTestPlan, latestByPlan map[int64]*ScheduledTestResult, monitorStatus string) []Account {
	monitorStatus = strings.TrimSpace(monitorStatus)
	if monitorStatus == "" {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		plan := primaryPlans[account.ID]
		var result *ScheduledTestResult
		if plan != nil {
			result = latestByPlan[plan.ID]
		}
		if upstreamAccountMonitorHealthStatus(plan, result) == monitorStatus {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func upstreamAccountMonitorHealthStatus(plan *ScheduledTestPlan, result *ScheduledTestResult) string {
	if plan == nil || result == nil {
		return ""
	}
	if result.Status != "success" {
		return MonitorStatusFailed
	}
	if result.LatencyMs >= upstreamAccountMonitorDegradedLatencyMs {
		return MonitorStatusDegraded
	}
	return MonitorStatusOperational
}

func (s *UpstreamAccountMonitorService) loadAvailabilityStats(ctx context.Context, planIDs []int64, window string, now time.Time) (map[int64]*ScheduledTestPlanStats, error) {
	if window == "15d" {
		return s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -15))
	}
	return s.resultRepo.Stats7dByPlanIDs(ctx, planIDs, now.AddDate(0, 0, -7))
}

func normalizeUpstreamMonitorAvailabilityWindow(window string) string {
	if strings.TrimSpace(window) == "15d" {
		return "15d"
	}
	return "7d"
}

func sortUpstreamMonitorAccountsByAvailability(accounts []Account, primaryPlans map[int64]*ScheduledTestPlan, statsByPlan map[int64]*ScheduledTestPlanStats, sortOrder string) {
	order := pagination.NormalizeSortOrder(sortOrder, pagination.SortOrderAsc)
	sort.SliceStable(accounts, func(i, j int) bool {
		leftAvailability, leftOK := upstreamMonitorAccountAvailability(accounts[i], primaryPlans, statsByPlan)
		rightAvailability, rightOK := upstreamMonitorAccountAvailability(accounts[j], primaryPlans, statsByPlan)
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && leftAvailability != rightAvailability {
			if order == pagination.SortOrderDesc {
				return leftAvailability > rightAvailability
			}
			return leftAvailability < rightAvailability
		}
		return upstreamMonitorAccountTieLess(accounts[i], accounts[j])
	})
}

func upstreamMonitorAccountAvailability(account Account, primaryPlans map[int64]*ScheduledTestPlan, statsByPlan map[int64]*ScheduledTestPlanStats) (float64, bool) {
	plan := primaryPlans[account.ID]
	if plan == nil {
		return 0, false
	}
	stats := statsByPlan[plan.ID]
	if stats == nil || stats.Total <= 0 {
		return 0, false
	}
	return stats.Availability7d, true
}

func upstreamMonitorAccountTieLess(left Account, right Account) bool {
	leftName := strings.ToLower(left.Name)
	rightName := strings.ToLower(right.Name)
	if leftName != rightName {
		return leftName < rightName
	}
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	return left.ID < right.ID
}

func upstreamMonitorPaginationResult(total int64, page int, pageSize int) *pagination.PaginationResult {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	pages := 0
	if pageSize > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &pagination.PaginationResult{Total: total, Page: page, PageSize: pageSize, Pages: pages}
}

func paginateUpstreamMonitorAccounts(accounts []Account, page int, pageSize int) []Account {
	if page < 1 {
		page = 1
	}
	pageSize = pagination.PaginationParams{PageSize: pageSize}.Limit()
	start := (page - 1) * pageSize
	if start >= len(accounts) {
		return []Account{}
	}
	end := start + pageSize
	if end > len(accounts) {
		end = len(accounts)
	}
	return accounts[start:end]
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
	return s.updateAllMonitoring(ctx, params, true)
}

func (s *UpstreamAccountMonitorService) DisableAll(ctx context.Context, params UpstreamAccountMonitorBatchParams) (*UpstreamAccountMonitorBatchResponse, error) {
	return s.updateAllMonitoring(ctx, params, false)
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
	accounts, err := s.listFilteredBatchAccounts(ctx, params)
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

func (s *UpstreamAccountMonitorService) StreamRunAll(ctx context.Context, params UpstreamAccountMonitorBatchParams) (<-chan UpstreamAccountMonitorRunAllStreamEvent, error) {
	accounts, err := s.listFilteredBatchAccounts(ctx, params)
	if err != nil {
		return nil, err
	}

	events := make(chan UpstreamAccountMonitorRunAllStreamEvent, upstreamAccountMonitorRunConcurrency+1)
	go func() {
		defer close(events)
		if !sendUpstreamAccountMonitorRunEvent(ctx, events, UpstreamAccountMonitorRunAllStreamEvent{
			Type:  "started",
			Total: len(accounts),
		}) {
			return
		}

		resp := &UpstreamAccountMonitorBatchResponse{Total: len(accounts)}
		var mu sync.Mutex
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(upstreamAccountMonitorRunConcurrency)
	submitLoop:
		for _, account := range accounts {
			select {
			case <-gctx.Done():
				break submitLoop
			default:
			}
			acc := account
			g.Go(func() error {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				event := s.runOneAccountMonitor(gctx, acc)
				mu.Lock()
				if event.Error != "" || event.Result == nil || event.Result.Status != "success" {
					resp.Failed++
				} else {
					resp.Success++
				}
				mu.Unlock()
				if !sendUpstreamAccountMonitorRunEvent(gctx, events, event) {
					return gctx.Err()
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil || ctx.Err() != nil {
			return
		}
		mu.Lock()
		done := UpstreamAccountMonitorRunAllStreamEvent{
			Type:    "done",
			Total:   resp.Total,
			Success: resp.Success,
			Failed:  resp.Failed,
			Skipped: resp.Skipped,
		}
		mu.Unlock()
		_ = sendUpstreamAccountMonitorRunEvent(ctx, events, done)
	}()
	return events, nil
}

func sendUpstreamAccountMonitorRunEvent(ctx context.Context, events chan<- UpstreamAccountMonitorRunAllStreamEvent, event UpstreamAccountMonitorRunAllStreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}

func (s *UpstreamAccountMonitorService) runOneAccountMonitor(ctx context.Context, account Account) UpstreamAccountMonitorRunAllStreamEvent {
	event := UpstreamAccountMonitorRunAllStreamEvent{
		Type:      "item",
		AccountID: account.ID,
	}
	plan, _, err := s.ensureDefaultPlan(ctx, account.ID, defaultMonitorEnabled(&account))
	if err != nil {
		event.Error = err.Error()
		return event
	}
	if plan != nil {
		event.PlanID = plan.ID
	}
	result, err := s.runAndSave(ctx, plan)
	event.Result = result
	if err != nil {
		event.Error = err.Error()
	}
	return event
}

func (s *UpstreamAccountMonitorService) listFilteredBatchAccounts(ctx context.Context, params UpstreamAccountMonitorBatchParams) ([]Account, error) {
	accounts, err := s.listAllAPIKeyAccounts(ctx, params)
	if err != nil {
		return nil, err
	}
	return s.filterAccountsByMonitorStatus(ctx, accounts, params.MonitorStatus)
}

func (s *UpstreamAccountMonitorService) filterAccountsByMonitorStatus(ctx context.Context, accounts []Account, monitorStatus string) ([]Account, error) {
	monitorStatus = strings.TrimSpace(monitorStatus)
	if monitorStatus == "" {
		return accounts, nil
	}
	_, primaryPlans, planIDs, err := s.loadPrimaryMonitorPlans(ctx, accounts)
	if err != nil {
		return nil, err
	}
	latestByPlan, err := s.resultRepo.ListLatestByPlanIDs(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	return filterAccountsByMonitorStatusData(accounts, primaryPlans, latestByPlan, monitorStatus), nil
}

func (s *UpstreamAccountMonitorService) updateAllMonitoring(ctx context.Context, params UpstreamAccountMonitorBatchParams, enabled bool) (*UpstreamAccountMonitorBatchResponse, error) {
	accounts, err := s.listFilteredBatchAccounts(ctx, params)
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

func (s *UpstreamAccountMonitorService) BatchUpdateSettings(ctx context.Context, params UpstreamAccountMonitorBatchParams, req UpstreamAccountMonitorBatchSettingsRequest) (*UpstreamAccountMonitorBatchResponse, error) {
	if req.MonitorEnabled == nil && req.IntervalMinutes == nil && req.JitterSeconds == nil {
		return nil, fmt.Errorf("at least one batch setting is required")
	}
	if req.IntervalMinutes != nil && (*req.IntervalMinutes < upstreamAccountMonitorMinInterval || *req.IntervalMinutes > upstreamAccountMonitorMaxInterval) {
		return nil, fmt.Errorf("interval_minutes must be between %d and %d", upstreamAccountMonitorMinInterval, upstreamAccountMonitorMaxInterval)
	}
	if req.JitterSeconds != nil && *req.JitterSeconds < 0 {
		return nil, fmt.Errorf("jitter_seconds must be >= 0")
	}

	accounts, err := s.listFilteredBatchAccounts(ctx, params)
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
	now := time.Now()
	for _, account := range accounts {
		plan := selectMonitorPlan(plansByAccount[account.ID])
		created := false
		if plan == nil {
			defaultEnabled := defaultMonitorEnabled(&account)
			if req.MonitorEnabled != nil {
				defaultEnabled = *req.MonitorEnabled
			}
			if monitorRequired(&account) {
				defaultEnabled = true
			}
			plan, created, err = s.ensureDefaultPlan(ctx, account.ID, defaultEnabled)
			if err != nil {
				return nil, err
			}
			if created {
				resp.Created++
			}
		}
		if plan == nil {
			resp.Skipped++
			continue
		}

		changed := created
		if req.IntervalMinutes != nil && plan.IntervalMinutes != *req.IntervalMinutes {
			plan.IntervalMinutes = *req.IntervalMinutes
			plan.CronExpression = cronFromIntervalMinutes(*req.IntervalMinutes)
			changed = true
		}
		effectiveInterval := monitorIntervalMinutes(plan)
		if req.JitterSeconds != nil {
			if *req.JitterSeconds >= effectiveInterval*60 {
				return nil, fmt.Errorf("jitter_seconds must be >= 0 and less than interval seconds")
			}
			if plan.JitterSeconds != *req.JitterSeconds {
				plan.JitterSeconds = *req.JitterSeconds
				changed = true
			}
		}
		if req.MonitorEnabled != nil {
			if !*req.MonitorEnabled && monitorRequired(&account) {
				if !plan.Enabled {
					plan.Enabled = true
					changed = true
				}
				resp.Skipped++
			} else if created {
				if plan.Enabled {
					resp.Enabled++
				}
			} else if plan.Enabled != *req.MonitorEnabled {
				plan.Enabled = *req.MonitorEnabled
				changed = true
				if *req.MonitorEnabled {
					resp.Enabled++
				} else {
					resp.Disabled++
				}
			} else if !changed {
				resp.Skipped++
			}
		} else if monitorRequired(&account) && !plan.Enabled {
			plan.Enabled = true
			changed = true
			resp.Enabled++
		}

		if !changed {
			continue
		}
		nextRun, err := computeScheduledTestNextRun(plan, now)
		if err != nil {
			return nil, err
		}
		plan.NextRunAt = &nextRun
		if _, err := s.planRepo.Update(ctx, plan); err != nil {
			return nil, err
		}
		if !created {
			resp.Updated++
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
