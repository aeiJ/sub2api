ALTER TABLE scheduled_test_results
    ADD COLUMN IF NOT EXISTS ping_latency_ms BIGINT;
