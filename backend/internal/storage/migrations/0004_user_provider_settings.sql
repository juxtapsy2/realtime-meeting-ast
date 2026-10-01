-- Per-user STT/LLM selection.
--
-- A user either rides the platform defaults (Google STT + Groq LLM, supplied
-- by deployment secrets) or brings their own providers/keys. Own keys are
-- stored encrypted (AES-256-GCM keyed from AUTH_HMAC_SECRET), never in
-- plaintext, and are never returned to clients.

CREATE TABLE IF NOT EXISTS user_provider_settings (
    user_email VARCHAR(255) PRIMARY KEY,
    use_own_keys BOOLEAN NOT NULL DEFAULT FALSE,
    stt_provider VARCHAR(50) NOT NULL DEFAULT '',
    stt_api_key_enc TEXT NOT NULL DEFAULT '',
    llm_provider VARCHAR(50) NOT NULL DEFAULT '',
    llm_model VARCHAR(120) NOT NULL DEFAULT '',
    llm_api_key_enc TEXT NOT NULL DEFAULT '',
    updated_by VARCHAR(255),
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Meetings record their owner so their provider selection can be resolved
-- when a session is created and when the meeting is summarized.
ALTER TABLE meetings ADD COLUMN IF NOT EXISTS owner_email VARCHAR(255);
CREATE INDEX IF NOT EXISTS idx_meetings_owner_email ON meetings (owner_email);
