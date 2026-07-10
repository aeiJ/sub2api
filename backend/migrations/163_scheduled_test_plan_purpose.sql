-- 163: Add scheduled test plan purpose for upstream account monitors.

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'scheduled_test';

CREATE INDEX IF NOT EXISTS idx_stp_account_purpose
    ON scheduled_test_plans(account_id, purpose);
