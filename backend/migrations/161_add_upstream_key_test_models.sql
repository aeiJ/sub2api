-- Persist model discovery metadata for upstream-managed keys.

ALTER TABLE upstream_keys
  ADD COLUMN IF NOT EXISTS supported_models JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS last_test_model VARCHAR(128) NOT NULL DEFAULT '';

COMMENT ON COLUMN upstream_keys.supported_models IS '账号管理同步上游模型后写回的支持模型列表，用于上游管理展示。';
COMMENT ON COLUMN upstream_keys.last_test_model IS '最近一次通过账号管理测速实际使用的模型。';
