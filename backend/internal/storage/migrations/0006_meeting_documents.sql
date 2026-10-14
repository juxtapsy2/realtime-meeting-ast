-- Uploaded meeting documents (agenda, notes, reports) used as MoM evidence
-- when a meeting was never recorded or only partially transcribed.
--
-- Only the extracted text is kept, never the original file. The text is used
-- by summary generation and is never returned to clients: the API serializes
-- metadata only (name/mime/chars/uploaded_at).

ALTER TABLE meetings ADD COLUMN IF NOT EXISTS documents JSONB NOT NULL DEFAULT '[]';
