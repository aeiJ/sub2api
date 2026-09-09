-- Add upstream-side group multiplier to upstream key pools.

ALTER TABLE upstream_key_pools
  ADD COLUMN IF NOT EXISTS upstream_group_rate_multiplier NUMERIC(10,4) NOT NULL DEFAULT 1.0;

COMMENT ON COLUMN upstream_key_pools.upstream_group_rate_multiplier IS '上游分组倍率；独立于同步到本地 groups 的 group_rate_multiplier。';
