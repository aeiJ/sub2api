-- Remove the retired upstream management and upstream account monitor modules.
-- Existing synced groups/accounts are intentionally left in place; only the
-- module-owned configuration tables and monitor plans are removed.

DELETE FROM scheduled_test_results
WHERE plan_id IN (
    SELECT id FROM scheduled_test_plans WHERE purpose = 'upstream_monitor'
);

DELETE FROM scheduled_test_plans
WHERE purpose = 'upstream_monitor';

DROP TABLE IF EXISTS upstream_sync_events;
DROP TABLE IF EXISTS upstream_keys;
DROP TABLE IF EXISTS upstream_key_pools;
DROP TABLE IF EXISTS upstream_platforms;
DROP TABLE IF EXISTS upstream_channels;
