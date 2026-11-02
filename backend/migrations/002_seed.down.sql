-- Remove all built-in tool rows and providers with the seeded codes. Provider
-- deletion is subject to the model_configs foreign key, so referenced providers
-- cannot be removed while dependent model configurations still exist.
DELETE FROM tools WHERE type = 'builtin';
DELETE FROM model_providers WHERE code IN ('openai','gemini','deepseek');
