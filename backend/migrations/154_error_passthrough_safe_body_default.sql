ALTER TABLE error_passthrough_rules
    ALTER COLUMN passthrough_body SET DEFAULT false;

UPDATE error_passthrough_rules
SET passthrough_body = false,
    custom_message = COALESCE(NULLIF(custom_message, ''), 'Upstream request failed'),
    updated_at = NOW()
WHERE passthrough_body = true;
