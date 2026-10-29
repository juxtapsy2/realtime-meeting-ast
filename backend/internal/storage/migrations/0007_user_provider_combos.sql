-- Saved STT/LLM combos: named snapshots of a user's provider configuration
-- (providers, model, encrypted keys) that can be switched with one request
-- instead of retyping everything.
--
-- API keys are the same AES-256-GCM ciphertexts used in
-- user_provider_settings (field label as AAD), copied verbatim server-side
-- when a combo is applied, and never returned to clients.

CREATE TABLE IF NOT EXISTS user_provider_combos (
    user_email VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    stt_provider VARCHAR(50) NOT NULL DEFAULT '',
    stt_api_key_enc TEXT NOT NULL DEFAULT '',
    llm_provider VARCHAR(50) NOT NULL DEFAULT '',
    llm_model VARCHAR(120) NOT NULL DEFAULT '',
    llm_api_key_enc TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_email, name)
);
