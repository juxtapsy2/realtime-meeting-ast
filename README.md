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
│   │   └── storage/         # Database layer
│   └── migrations/
├── frontend/
│   └── src/
│       ├── api/             # API client functions
│       ├── components/      # Reusable components
│       ├── features/        # Feature-oriented modules
│       │   ├── meetings/
│       │   ├── transcript/
│       │   └── intelligence/
│       ├── hooks/           # Custom React hooks
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

# AI Providers
STT_PROVIDER=google
STT_API_KEY=your_stt_api_key
LLM_PROVIDER=groq
LLM_API_KEY=your_llm_api_key

# Optional: path to a business glossary JSON file ({ "terms": [{ "term": "CR", "expansion": "Change Request", ... }] }).
# Defaults to an embedded glossary.
BUSINESS_GLOSSARY_PATH=/path/to/glossary.json
```

### Database Setup

```bash
# Create database
createdb meeting_ast

# The application will run migrations automatically on startup
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
