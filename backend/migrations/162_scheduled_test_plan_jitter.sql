-- 162: Add interval and jitter to scheduled test plans.

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS interval_minutes INTEGER NOT NULL DEFAULT 0;

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS jitter_seconds INTEGER NOT NULL DEFAULT 0;
