# Realtime Meeting Intelligence Platform

A real-time AI Meeting Intelligence Platform that listens to meeting audio, transcribes conversations, understands business context, and extracts structured information.

## Architecture

This project follows the architecture defined in `ARCHITECTURE.md`:

- **Frontend**: React + TypeScript + Vite + TailwindCSS
- **Backend**: Go (modular monolith)
- **Database**: PostgreSQL with pgvector (for future RAG)
- **Realtime**: WebSocket for live audio and transcript streaming
- **AI Providers**: Cloud-first, provider-agnostic (Deepgram for STT, OpenAI for intelligence)

## Project Structure

```
/
├── backend/
│   ├── cmd/server/          # Application entry point
│   ├── internal/
│   │   ├── meetings/        # Meeting domain logic
│   │   ├── realtime/        # WebSocket hub and connections
│   │   ├── transcription/   # STT provider interfaces
│   │   ├── intelligence/    # AI analysis providers
│   │   ├── providerconfig/  # Per-user STT/LLM provider + key resolution
│   │   ├── secretbox/       # AES-256-GCM encryption for stored user API keys
│   │   ├── sealedbox/       # ECDH envelope for API keys sent to the backend
│   │   ├── settings/        # Runtime platform settings + admin roles
│   │   └── storage/         # Database layer and versioned migrations
│       └── migrations/      # Numbered SQL migrations, embedded and applied on boot
├── frontend/
│   └── src/
│       ├── api/             # API client functions
│       ├── components/      # Reusable components
│       ├── features/        # Feature-oriented modules
│       │   ├── meetings/
│       │   ├── transcript/
│       │   └── intelligence/
│       ├── hooks/           # Custom React hooks
│       ├── lib/             # Browser seal box for API keys in transit
│       └── types/           # TypeScript types
└── scripts/
```

## Features

### MVP (Phase 1 - Realtime Transcription)

- Create and manage meetings
- Browser microphone capture
- Real-time speech-to-text transcription
- Live transcript display
- Transcript persistence
- Meeting history

### Phase 2 - Meeting Intelligence

- Real-time topic detection
- Decision extraction
- Action item identification
- Issue tracking
- Open question detection
- Meeting state management

## Getting Started

### Prerequisites

- Go 1.21+
- Node.js 18+
- PostgreSQL 14+
- Deepgram API key (for STT)
- OpenAI API key (for intelligence)

### Environment Variables

```bash
# Server
PORT=8080
DATABASE_URL=postgres://localhost:5432/meeting_ast?sslmode=disable

# AI Providers (platform defaults, from environment/deployment secrets)
STT_PROVIDER=google
STT_API_KEY=your_stt_api_key
LLM_PROVIDER=groq
LLM_API_KEY=your_llm_api_key

# Access control: comma-separated allowlist of emails allowed to use the
# service. Empty means the gate is disabled. API keys are NEVER stored here or
# in the database: each user either uses the platform keys above, or their own
# keys, which the superadmin stores encrypted from the Admin page.
ALLOWED_EMAILS=you@example.com,teammate@example.com

# Single superadmin, configured ONLY through the environment. Sits above the
# database-managed admins and is implicitly allowlisted so it can never be
# locked out. Only this role can change settings, admins, or user API keys.
SUPERADMIN_EMAIL=you@example.com

# Signs session cookies AND derives the AES-256-GCM key that encrypts per-user
# API keys in the database. If empty, sessions reset on restart and storing
# user keys is disabled. Changing it makes stored user keys undecryptable.
AUTH_HMAC_SECRET=change_me_to_a_long_random_string

# Optional: path to a business glossary JSON file ({ "terms": [{ "term": "CR", "expansion": "Change Request", ... }] }).
# Defaults to an embedded glossary. The glossary feeds BOTH the AI summary (LLM
# prompt normalization) and speech recognition (phrase biasing + normalization).
BUSINESS_GLOSSARY_PATH=/path/to/glossary.json
```

The glossary drives two layers:

1. **LLM summarization** — terms/aliases/expansions are injected into the
   summary prompt so MOM output uses canonical forms.
2. **Speech recognition (Google STT)** — each term is biased via
   SpeechAdaptation phrase hints. Boost tiers: exact identifiers (digits) = 20,
   short all-caps acronyms (e.g. `POSM`) = 18, other terms = 15, spelled-out
   expansions = 10.

Optional per-term STT fields:

```json
{
  "term": "POSM",
  "expansion": "Point of Sale Material",
  "stt_normalize": ["positive", "padsam"]
}
```

- `stt_hints`: extra spellings to bias toward (e.g. exact identifiers like
  `"PADM2-158069"` or unusually-pronounced vendor names). Biasing only.
- `stt_normalize`: transcript forms rewritten to the canonical `term` after
  recognition (stable partials/finals only). Curate carefully — replacement is
  a literal substring rewrite, so never add short or common words
  (e.g. do NOT add `"CA"` to normalize to `"CR"`; it would corrupt ordinary
  text).

### Database Setup

```bash
# Create database
createdb meeting_ast

# The application will run migrations automatically on startup
# (backend/internal/storage/migrations/*.sql, tracked in schema_migrations)
```

### Running the Application

**Backend:**

```bash
cd backend
go mod tidy
go run cmd/server/main.go
```

**Frontend:**

```bash
cd frontend
npm install
npm run dev
```

The frontend will be available at `http://localhost:3000` and will proxy API requests to the backend at `http://localhost:8080`.

## API Endpoints

### REST API

- `GET /api/meetings` - List all meetings
- `POST /api/meetings` - Create a new meeting
- `GET /api/meetings/:id` - Get meeting by ID
- `PUT /api/meetings/:id` - Update meeting
- `DELETE /api/meetings/:id` - Delete meeting

### WebSocket

- `ws://localhost:8080/ws/meeting/:id` - WebSocket connection for real-time events

#### WebSocket Events

**Client to Server:**
- `start_meeting` - Start the meeting
- `end_meeting` - End the meeting
- Binary audio data - Stream audio to transcriber

**Server to Client:**
- `transcript.partial` - Partial transcript (may change)
- `transcript.final` - Final transcript segment
- `meeting.started` - Meeting started
- `meeting.ended` - Meeting ended
- `state.updated` - Meeting state update

## STT Providers

### Deepgram (Default)

Real-time streaming STT with WebSocket support.

### OpenAI Whisper

Batch processing STT (audio is buffered and sent in chunks).

## Intelligence Providers

### OpenAI GPT-4

Analyzes transcript chunks and extracts:
- Current topic
- Decisions
- Action items
- Issues
- Open questions

## Development

### Adding New STT Provider

1. Create a new file in `backend/internal/transcription/`
2. Implement the `Transcriber` interface
3. Add provider to `NewProvider` factory function

### Adding New Intelligence Provider

1. Create a new file in `backend/internal/intelligence/`
2. Implement the `IntelligenceProvider` interface
3. Add provider to `NewProvider` factory function

## License

MIT
