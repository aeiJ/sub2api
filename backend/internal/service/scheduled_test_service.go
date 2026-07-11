package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/robfig/cron/v3"
)

var scheduledTestCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ScheduledTestService provides CRUD operations for scheduled test plans and results.
type ScheduledTestService struct {
	planRepo   ScheduledTestPlanRepository
	resultRepo ScheduledTestResultRepository
}

// NewScheduledTestService creates a new ScheduledTestService.
func NewScheduledTestService(
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
) *ScheduledTestService {
	return &ScheduledTestService{
		planRepo:   planRepo,
		resultRepo: resultRepo,
	}
}

// CreatePlan validates the cron expression, computes next_run_at, and persists the plan.
func (s *ScheduledTestService) CreatePlan(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	ensureScheduledTestPurpose(plan)
	nextRun, err := computeScheduledTestNextRun(plan, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	plan.NextRunAt = &nextRun

	if plan.MaxResults <= 0 {
		plan.MaxResults = 50
	}

	return s.planRepo.Create(ctx, plan)
}

// GetPlan retrieves a plan by ID.
func (s *ScheduledTestService) GetPlan(ctx context.Context, id int64) (*ScheduledTestPlan, error) {
	return s.planRepo.GetByID(ctx, id)
}

// ListPlansByAccount returns all plans for a given account.
func (s *ScheduledTestService) ListPlansByAccount(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error) {
	return s.planRepo.ListByAccountID(ctx, accountID)
}

// UpdatePlan validates cron and updates the plan.
func (s *ScheduledTestService) UpdatePlan(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	ensureScheduledTestPurpose(plan)
	nextRun, err := computeScheduledTestNextRun(plan, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	plan.NextRunAt = &nextRun

	return s.planRepo.Update(ctx, plan)
}

// DeletePlan removes a plan and its results (via CASCADE).
func (s *ScheduledTestService) DeletePlan(ctx context.Context, id int64) error {
	return s.planRepo.Delete(ctx, id)
}

// ListResults returns the most recent results for a plan.
func (s *ScheduledTestService) ListResults(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.resultRepo.ListByPlanID(ctx, planID, limit)
}

// SaveResult inserts a result and prunes old entries beyond maxResults.
func (s *ScheduledTestService) SaveResult(ctx context.Context, planID int64, maxResults int, result *ScheduledTestResult) error {
	result.PlanID = planID
	if _, err := s.resultRepo.Create(ctx, result); err != nil {
		return err
	}
	return s.resultRepo.PruneOldResults(ctx, planID, maxResults)
}

func computeNextRun(cronExpr string, from time.Time) (time.Time, error) {
	sched, err := scheduledTestCronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}

func computeNextRunWithJitter(cronExpr string, from time.Time, jitterSeconds int) (time.Time, error) {
	next, err := computeNextRun(cronExpr, from)
	if err != nil {
		return time.Time{}, err
	}
	if jitterSeconds <= 0 {
		return next, nil
	}
	return next.Add(time.Duration(rand.IntN(jitterSeconds+1)) * time.Second), nil
}

func computeScheduledTestNextRun(plan *ScheduledTestPlan, from time.Time) (time.Time, error) {
	if plan != nil && plan.IntervalMinutes > 0 {
		next := from.Add(time.Duration(plan.IntervalMinutes) * time.Minute)
		if plan.JitterSeconds > 0 {
			offsetSeconds := rand.IntN(plan.JitterSeconds*2+1) - plan.JitterSeconds
			next = next.Add(time.Duration(offsetSeconds) * time.Second)
		}
		return next, nil
	}
	if plan == nil {
		return time.Time{}, fmt.Errorf("scheduled test plan is nil")
	}
	return computeNextRunWithJitter(plan.CronExpression, from, plan.JitterSeconds)
}

func ensureScheduledTestPurpose(plan *ScheduledTestPlan) {
	if plan != nil && plan.Purpose == "" {
		plan.Purpose = ScheduledTestPlanPurposeScheduledTest
	}
}
