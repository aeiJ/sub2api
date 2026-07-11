-- 174: Restore scheduled-test fields required by custom upstream account monitors.

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS interval_minutes INTEGER NOT NULL DEFAULT 0;

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS jitter_seconds INTEGER NOT NULL DEFAULT 0;

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'scheduled_test';

CREATE INDEX IF NOT EXISTS idx_stp_account_purpose
    ON scheduled_test_plans(account_id, purpose);

ALTER TABLE scheduled_test_results
    ADD COLUMN IF NOT EXISTS ping_latency_ms BIGINT;
