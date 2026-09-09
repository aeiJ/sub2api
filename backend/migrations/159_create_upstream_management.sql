-- Create upstream management tables.
-- Upstream management is a configuration workspace only. It syncs into existing
-- groups/accounts and does not participate in request routing or billing logic.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS upstream_channels (
    id          BIGSERIAL    PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    status      VARCHAR(20)  NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_channels_name ON upstream_channels (name);
CREATE INDEX IF NOT EXISTS idx_upstream_channels_status ON upstream_channels (status);

CREATE TABLE IF NOT EXISTS upstream_platforms (
    id           BIGSERIAL    PRIMARY KEY,
    channel_id   BIGINT       NOT NULL REFERENCES upstream_channels(id) ON DELETE CASCADE,
    provider     VARCHAR(50)  NOT NULL,
    display_name VARCHAR(100) NOT NULL DEFAULT '',
    base_url     TEXT         NOT NULL DEFAULT '',
    status       VARCHAR(20)  NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_platforms_channel_provider
    ON upstream_platforms (channel_id, provider);
CREATE INDEX IF NOT EXISTS idx_upstream_platforms_channel_id ON upstream_platforms (channel_id);
CREATE INDEX IF NOT EXISTS idx_upstream_platforms_provider ON upstream_platforms (provider);

CREATE TABLE IF NOT EXISTS upstream_key_pools (
    id                      BIGSERIAL    PRIMARY KEY,
    platform_id             BIGINT       NOT NULL REFERENCES upstream_platforms(id) ON DELETE CASCADE,
    name                    VARCHAR(100) NOT NULL,
    group_name              VARCHAR(100) NOT NULL,
    group_rate_multiplier   NUMERIC(10,4) NOT NULL DEFAULT 1.0,
    account_rate_multiplier NUMERIC(10,4) NOT NULL DEFAULT 1.0,
    load_factor             INTEGER      NOT NULL DEFAULT 0,
    concurrency             INTEGER      NOT NULL DEFAULT 3,
    status                  VARCHAR(20)  NOT NULL DEFAULT 'active',
    synced_group_id         BIGINT       REFERENCES groups(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_upstream_key_pools_platform_id ON upstream_key_pools (platform_id);
CREATE INDEX IF NOT EXISTS idx_upstream_key_pools_synced_group_id ON upstream_key_pools (synced_group_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_key_pools_platform_name
    ON upstream_key_pools (platform_id, name);

CREATE TABLE IF NOT EXISTS upstream_keys (
    id                   BIGSERIAL    PRIMARY KEY,
    pool_id              BIGINT       NOT NULL REFERENCES upstream_key_pools(id) ON DELETE CASCADE,
    name                 VARCHAR(100) NOT NULL,
    encrypted_api_key    TEXT         NOT NULL DEFAULT '',
    api_key_fingerprint  VARCHAR(64)  NOT NULL DEFAULT '',
    status               VARCHAR(20)  NOT NULL DEFAULT 'active',
    synced_account_id    BIGINT       REFERENCES accounts(id) ON DELETE SET NULL,
    last_test_latency_ms INTEGER,
    last_test_status     VARCHAR(20)  NOT NULL DEFAULT '',
    last_test_message    TEXT         NOT NULL DEFAULT '',
    last_tested_at       TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_upstream_keys_pool_id ON upstream_keys (pool_id);
CREATE INDEX IF NOT EXISTS idx_upstream_keys_synced_account_id ON upstream_keys (synced_account_id);
CREATE INDEX IF NOT EXISTS idx_upstream_keys_fingerprint ON upstream_keys (api_key_fingerprint);

CREATE TABLE IF NOT EXISTS upstream_sync_events (
    id         BIGSERIAL   PRIMARY KEY,
    channel_id BIGINT      NOT NULL REFERENCES upstream_channels(id) ON DELETE CASCADE,
    action     VARCHAR(50) NOT NULL,
    summary    JSONB       NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_upstream_sync_events_channel_id ON upstream_sync_events (channel_id);
CREATE INDEX IF NOT EXISTS idx_upstream_sync_events_created_at ON upstream_sync_events (created_at DESC);

COMMENT ON TABLE upstream_channels IS '上游管理渠道：集中维护平台、分组 key 池和 API Key 配置';
COMMENT ON TABLE upstream_platforms IS '上游渠道中的平台配置，如 anthropic/openai';
COMMENT ON TABLE upstream_key_pools IS '上游平台下的分组 key 池，同步到 groups';
COMMENT ON TABLE upstream_keys IS '上游 key 池中的 API Key，同步到 accounts';
COMMENT ON TABLE upstream_sync_events IS '上游同步操作摘要审计';
