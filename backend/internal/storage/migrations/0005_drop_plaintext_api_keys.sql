-- Remove API keys that earlier builds stored in app_settings in plaintext.
--
-- Platform STT/LLM keys now come from deployment secrets only, and per-user
-- keys live encrypted in user_provider_settings, so these rows must not remain
-- in the database. Deleting them is safe: dropping the key falls back to the
-- platform provider, which is the documented behaviour for unconfigured users.

DELETE FROM app_settings WHERE key IN ('STT_API_KEY', 'LLM_API_KEY');
