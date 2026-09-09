package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// --- Plan Repository ---

type scheduledTestPlanRepository struct {
	db *sql.DB
}

func NewScheduledTestPlanRepository(db *sql.DB) service.ScheduledTestPlanRepository {
	return &scheduledTestPlanRepository{db: db}
}

func (r *scheduledTestPlanRepository) Create(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	normalizePlanPurpose(plan)
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_plans (account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
	`, plan.AccountID, plan.Purpose, plan.ModelID, plan.CronExpression, plan.IntervalMinutes, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.JitterSeconds, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) GetByID(ctx context.Context, id int64) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE id = $1
	`, id)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) ListByAccountID(ctx context.Context, accountID int64) ([]*service.ScheduledTestPlan, error) {
	return r.ListByAccountIDAndPurpose(ctx, accountID, service.ScheduledTestPlanPurposeScheduledTest)
}

func (r *scheduledTestPlanRepository) ListByAccountIDAndPurpose(ctx context.Context, accountID int64, purpose string) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE account_id = $1 AND purpose = $2
		ORDER BY created_at DESC
	`, accountID, normalizePurpose(purpose))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) ListByAccountIDs(ctx context.Context, accountIDs []int64) (map[int64][]*service.ScheduledTestPlan, error) {
	return r.listByAccountIDs(ctx, accountIDs, "")
}

func (r *scheduledTestPlanRepository) ListByAccountIDsAndPurpose(ctx context.Context, accountIDs []int64, purpose string) (map[int64][]*service.ScheduledTestPlan, error) {
	return r.listByAccountIDs(ctx, accountIDs, normalizePurpose(purpose))
}

func (r *scheduledTestPlanRepository) listByAccountIDs(ctx context.Context, accountIDs []int64, purpose string) (map[int64][]*service.ScheduledTestPlan, error) {
	result := make(map[int64][]*service.ScheduledTestPlan, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	query := `
		SELECT id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans
		WHERE account_id = ANY($1)`
	args := []any{pq.Array(accountIDs)}
	if purpose != "" {
		query += ` AND purpose = $2`
		args = append(args, purpose)
	}
	query += ` ORDER BY account_id ASC, created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		result[p.AccountID] = append(result[p.AccountID], p)
	}
	return result, rows.Err()
}

func (r *scheduledTestPlanRepository) ListDue(ctx context.Context, now time.Time) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans
		WHERE enabled = true AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) Update(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	normalizePlanPurpose(plan)
	row := r.db.QueryRowContext(ctx, `
		UPDATE scheduled_test_plans
		SET purpose = $2, model_id = $3, cron_expression = $4, interval_minutes = $5, enabled = $6, max_results = $7, auto_recover = $8, jitter_seconds = $9, next_run_at = $10, updated_at = NOW()
		WHERE id = $1
		RETURNING id, account_id, purpose, model_id, cron_expression, interval_minutes, enabled, max_results, auto_recover, jitter_seconds, last_run_at, next_run_at, created_at, updated_at
	`, plan.ID, plan.Purpose, plan.ModelID, plan.CronExpression, plan.IntervalMinutes, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.JitterSeconds, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_test_plans WHERE id = $1`, id)
	return err
}

func (r *scheduledTestPlanRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_plans SET last_run_at = $2, next_run_at = $3, updated_at = NOW() WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

// --- Result Repository ---

type scheduledTestResultRepository struct {
	db *sql.DB
}

func NewScheduledTestResultRepository(db *sql.DB) service.ScheduledTestResultRepository {
	return &scheduledTestResultRepository{db: db}
}

func (r *scheduledTestResultRepository) Create(ctx context.Context, result *service.ScheduledTestResult) (*service.ScheduledTestResult, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_results (plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id, plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at
	`, result.PlanID, result.Status, result.ResponseText, result.ErrorMessage, result.LatencyMs, result.PingLatencyMs, result.StartedAt, result.FinishedAt)

	return scanResult(row)
}

func (r *scheduledTestResultRepository) ListByPlanID(ctx context.Context, planID int64, limit int) ([]*service.ScheduledTestResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at
		FROM scheduled_test_results
		WHERE plan_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*service.ScheduledTestResult
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (r *scheduledTestResultRepository) ListLatestByPlanIDs(ctx context.Context, planIDs []int64) (map[int64]*service.ScheduledTestResult, error) {
	result := make(map[int64]*service.ScheduledTestResult, len(planIDs))
	if len(planIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (plan_id)
			id, plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at
		FROM scheduled_test_results
		WHERE plan_id = ANY($1)
		ORDER BY plan_id, created_at DESC
	`, pq.Array(planIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		result[r.PlanID] = r
	}
	return result, rows.Err()
}

func (r *scheduledTestResultRepository) ListRecentByPlanIDs(ctx context.Context, planIDs []int64, limit int) (map[int64][]*service.ScheduledTestResult, error) {
	result := make(map[int64][]*service.ScheduledTestResult, len(planIDs))
	if len(planIDs) == 0 || limit <= 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at
		FROM (
			SELECT
				id, plan_id, status, response_text, error_message, latency_ms, ping_latency_ms, started_at, finished_at, created_at,
				ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY created_at DESC) AS rn
			FROM scheduled_test_results
			WHERE plan_id = ANY($1)
		) ranked
		WHERE rn <= $2
		ORDER BY plan_id ASC, created_at DESC
	`, pq.Array(planIDs), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		result[r.PlanID] = append(result[r.PlanID], r)
	}
	return result, rows.Err()
}

func (r *scheduledTestResultRepository) Stats7dByPlanIDs(ctx context.Context, planIDs []int64, since time.Time) (map[int64]*service.ScheduledTestPlanStats, error) {
	result := make(map[int64]*service.ScheduledTestPlanStats, len(planIDs))
	if len(planIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			plan_id,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'success') AS success
		FROM scheduled_test_results
		WHERE plan_id = ANY($1) AND created_at >= $2
		GROUP BY plan_id
	`, pq.Array(planIDs), since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		stats := &service.ScheduledTestPlanStats{}
		if err := rows.Scan(&stats.PlanID, &stats.Total, &stats.Success); err != nil {
			return nil, err
		}
		if stats.Total > 0 {
			stats.Availability7d = float64(stats.Success) / float64(stats.Total) * 100
		}
		result[stats.PlanID] = stats
	}
	return result, rows.Err()
}

func (r *scheduledTestResultRepository) PruneOldResults(ctx context.Context, planID int64, keepCount int) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM scheduled_test_results
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY created_at DESC) AS rn
				FROM scheduled_test_results
				WHERE plan_id = $1
			) ranked
			WHERE rn > $2
		)
	`, planID, keepCount)
	return err
}

// --- scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanResult(row scannable) (*service.ScheduledTestResult, error) {
	r := &service.ScheduledTestResult{}
	var pingLatency sql.NullInt64
	if err := row.Scan(
		&r.ID, &r.PlanID, &r.Status, &r.ResponseText, &r.ErrorMessage,
		&r.LatencyMs, &pingLatency, &r.StartedAt, &r.FinishedAt, &r.CreatedAt,
	); err != nil {
		return nil, err
	}
	if pingLatency.Valid {
		r.PingLatencyMs = &pingLatency.Int64
	}
	return r, nil
}

func scanPlan(row scannable) (*service.ScheduledTestPlan, error) {
	p := &service.ScheduledTestPlan{}
	if err := row.Scan(
		&p.ID, &p.AccountID, &p.Purpose, &p.ModelID, &p.CronExpression, &p.IntervalMinutes, &p.Enabled, &p.MaxResults, &p.AutoRecover,
		&p.JitterSeconds, &p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	normalizePlanPurpose(p)
	return p, nil
}

func scanPlans(rows *sql.Rows) ([]*service.ScheduledTestPlan, error) {
	var plans []*service.ScheduledTestPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func normalizePlanPurpose(plan *service.ScheduledTestPlan) {
	if plan != nil {
		plan.Purpose = normalizePurpose(plan.Purpose)
	}
}

func normalizePurpose(purpose string) string {
	if purpose == "" {
		return service.ScheduledTestPlanPurposeScheduledTest
	}
	return purpose
}
