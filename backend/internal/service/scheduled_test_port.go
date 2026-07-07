package service

import (
	"context"
	"time"
)

const (
	ScheduledTestPlanPurposeScheduledTest   = "scheduled_test"
	ScheduledTestPlanPurposeUpstreamMonitor = "upstream_monitor"
)

// ScheduledTestPlan represents a scheduled test plan domain model.
type ScheduledTestPlan struct {
	ID              int64      `json:"id"`
	AccountID       int64      `json:"account_id"`
	Purpose         string     `json:"purpose"`
	ModelID         string     `json:"model_id"`
	CronExpression  string     `json:"cron_expression"`
	IntervalMinutes int        `json:"interval_minutes"`
	Enabled         bool       `json:"enabled"`
	MaxResults      int        `json:"max_results"`
	AutoRecover     bool       `json:"auto_recover"`
	JitterSeconds   int        `json:"jitter_seconds"`
	LastRunAt       *time.Time `json:"last_run_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ScheduledTestResult represents a single test execution result.
type ScheduledTestResult struct {
	ID            int64     `json:"id"`
	PlanID        int64     `json:"plan_id"`
	Status        string    `json:"status"`
	ResponseText  string    `json:"response_text"`
	ErrorMessage  string    `json:"error_message"`
	LatencyMs     int64     `json:"latency_ms"`
	PingLatencyMs *int64    `json:"ping_latency_ms"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// ScheduledTestPlanStats summarizes recent results for one test plan.
type ScheduledTestPlanStats struct {
	PlanID         int64   `json:"plan_id"`
	Total          int64   `json:"total"`
	Success        int64   `json:"success"`
	Availability7d float64 `json:"availability_7d"`
}

// ScheduledTestPlanRepository defines the data access interface for test plans.
type ScheduledTestPlanRepository interface {
	Create(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	GetByID(ctx context.Context, id int64) (*ScheduledTestPlan, error)
	ListByAccountID(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error)
	ListByAccountIDs(ctx context.Context, accountIDs []int64) (map[int64][]*ScheduledTestPlan, error)
	ListByAccountIDAndPurpose(ctx context.Context, accountID int64, purpose string) ([]*ScheduledTestPlan, error)
	ListByAccountIDsAndPurpose(ctx context.Context, accountIDs []int64, purpose string) (map[int64][]*ScheduledTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]*ScheduledTestPlan, error)
	Update(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	Delete(ctx context.Context, id int64) error
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error
}

// ScheduledTestResultRepository defines the data access interface for test results.
type ScheduledTestResultRepository interface {
	Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error)
	ListByPlanID(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error)
	ListLatestByPlanIDs(ctx context.Context, planIDs []int64) (map[int64]*ScheduledTestResult, error)
	ListRecentByPlanIDs(ctx context.Context, planIDs []int64, limit int) (map[int64][]*ScheduledTestResult, error)
	Stats7dByPlanIDs(ctx context.Context, planIDs []int64, since time.Time) (map[int64]*ScheduledTestPlanStats, error)
	PruneOldResults(ctx context.Context, planID int64, keepCount int) error
}
