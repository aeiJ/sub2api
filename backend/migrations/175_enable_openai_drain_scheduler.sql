-- Enable the OpenAI advanced scheduler by default so OpenAI requests use the
-- custom drain scheduling path unless an operator disables it after this version.
INSERT INTO settings (key, value)
VALUES ('openai_advanced_scheduler_enabled', 'true')
ON CONFLICT (key) DO UPDATE
SET value = 'true'
WHERE settings.value = 'false';
