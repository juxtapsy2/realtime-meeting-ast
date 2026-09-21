# Meeting Intelligence Platform

## 1. Product Vision

Build a **real-time AI Meeting Intelligence Platform**.

The system listens to meeting audio in real time, transcribes conversations, understands the business/project context, continuously extracts structured information, and builds persistent organizational knowledge.

This is **not** an audio-upload summarizer.

Core flow:

```text
Live Meeting Audio
        ↓
Streaming Speech-to-Text
        ↓
Transcript Events
        ↓
Meeting Intelligence Engine
        ↓
Business Context Retrieval / RAG
        ↓
Structured Meeting State
        ↓
Live UI + Persistent Knowledge
```

The system should eventually behave like an **AI Meeting Copilot** capable of understanding what people are discussing rather than merely recording what they said.

---

# 2. Core Principles

1. Real-time first.
2. Business-context aware.
3. Transcript and AI analysis are separate concerns.
4. Do not send every sentence independently to an LLM.
5. Maintain incremental structured meeting state.
6. Preserve source transcript as evidence.
7. AI-generated conclusions must be traceable to transcript/context.
8. Project knowledge persists across meetings.
9. Architecture should support multiple simultaneous meetings.
10. Keep implementation simple until scale requires additional infrastructure.

Avoid premature microservices and unnecessary abstraction.

---

# 3. Initial Tech Stack

## Frontend

* React
* Vite
* TypeScript
* TailwindCSS
* WebSocket client
* Browser MediaRecorder / Web Audio APIs

Responsibilities:

* microphone capture
* meeting controls
* live transcript
* speaker display
* live AI insights
* meeting history
* project/workspace management
* knowledge search

## Backend

Primary backend:

* Go

Responsibilities:

* REST API
* WebSocket connections
* realtime meeting sessions
* audio stream coordination
* transcript event processing
* meeting state management
* AI orchestration
* RAG retrieval
* authentication/authorization
* persistence

Do not introduce additional backend languages unless a concrete technical requirement justifies them.

## Storage

### PostgreSQL

Primary persistent database.

Store:

* users
* organizations/workspaces
* projects
* meetings
* participants
* transcripts
* transcript segments
* decisions
* action items
* issues
* questions
* documents
* business entities
* meeting relationships

### pgvector

Store embeddings for semantic retrieval over:

* previous meetings
* transcript segments
* decisions
* tickets
* BRD/FSD
* SOPs
* uploaded business documents
* project knowledge

### Redis

Use only where ephemeral/realtime state benefits from it:

* active meeting state
* pub/sub
* temporary transcript buffers
* distributed coordination
* short-lived caches

Do not treat Redis as permanent storage.

## Object Storage

Store:

* optional meeting recordings
* uploaded documents
* generated artifacts

Use an S3-compatible interface where practical.

---

# 4. Domain Hierarchy

```text
Organization
    ↓
Workspace
    ↓
Project
    ├── Meetings
    ├── Documents
    ├── Business Glossary
    ├── People
    ├── Tickets
    ├── Decisions
    ├── SOPs
    ├── BRDs/FSDs
    └── Knowledge
```

Meetings should normally belong to a project so the AI can retrieve the correct business context.

---

# 5. Meeting Lifecycle

## Before Meeting

Load relevant project context:

```text
Meeting metadata
Project
Participants
Business terminology
Recent meetings
Open action items
Previous decisions
Relevant documents
Known tickets/issues
```

Do not load the entire knowledge base into the LLM context.

Retrieve only relevant information.

## During Meeting

```text
Microphone
    ↓
Audio chunks
    ↓
Streaming STT
    ↓
Transcript segments
    ↓
Semantic/event buffering
    ↓
Intelligence analysis
    ↓
MeetingState update
    ↓
WebSocket
    ↓
Live UI
```

## After Meeting

Finalize:

* transcript
* summary
* decisions
* action items
* discussed issues
* unresolved questions
* participants
* topics
* referenced business entities
* related documents/tickets
* searchable embeddings

The resulting knowledge becomes available to future meetings.

---

# 6. Realtime Audio

The browser captures microphone audio continuously.

Prefer an architecture supporting:

```text
Browser
   ↓
WebSocket/WebRTC
   ↓
Audio Gateway
   ↓
Streaming STT Provider
```

## AI & Speech Provider Strategy

Development is **cloud-first, provider-agnostic**.

Do not require developers to download or self-host AI models to run the initial system.

Initial development should use hosted APIs for:

* realtime Speech-to-Text
* LLM meeting analysis
* embeddings

However, application code must never depend directly on a specific AI vendor.

All external AI capabilities must be accessed through internal provider interfaces.

```text
Application
    │
    ├── Transcriber
    │      ├── CloudTranscriber
    │      └── LocalTranscriber [future]
    │
    ├── IntelligenceProvider
    │      ├── CloudLLM
    │      └── LocalLLM [future]
    │
    └── EmbeddingProvider
           ├── CloudEmbedding
           └── LocalEmbedding [future]
```

Example:

```go
type Transcriber interface {
    Start(ctx context.Context, config Config) error
    WriteAudio(chunk []byte) error
    Events() <-chan TranscriptEvent
    Close() error
}

type IntelligenceProvider interface {
    AnalyzeChunk(
        ctx context.Context,
        input AnalysisInput,
    ) (MeetingStatePatch, error)

    FinalizeMeeting(
        ctx context.Context,
        input FinalizationInput,
    ) (MeetingSummary, error)
}

type EmbeddingProvider interface {
    Embed(
        ctx context.Context,
        texts []string,
    ) ([][]float32, error)
}
```

Provider implementations belong in infrastructure/provider packages.

Business logic must depend on these interfaces, not vendor SDKs.

---

## Provider Configuration

Provider selection must be configurable through environment variables.

Example:

```env
STT_PROVIDER=cloud
LLM_PROVIDER=cloud
EMBEDDING_PROVIDER=cloud
```

Future local development may support:

```env
STT_PROVIDER=whisper_local
LLM_PROVIDER=ollama
EMBEDDING_PROVIDER=local
```

Provider-specific credentials/configuration should remain separate:

```env
STT_API_KEY=
LLM_API_KEY=
```

Never expose provider credentials to the frontend.

The frontend communicates only with the application backend.

---

## Local AI Support

Local AI is a future capability, not an MVP prerequisite.

Possible future technologies include:

```text
STT
├── whisper.cpp
├── faster-whisper
└── other compatible streaming STT servers

LLM
├── Ollama
├── llama.cpp
└── other OpenAI-compatible/local inference servers

Embeddings
└── local embedding models
```

Do not introduce Python/model-serving infrastructure into the initial architecture merely because some local models require it.

Add a local inference service only when implementing local-provider support.

The Go backend remains the primary application backend.

---

## Realtime STT Requirements

Realtime transcription must explicitly distinguish:

```text
audio chunk
      ↓
partial transcript
      ↓
revised partial transcript
      ↓
final transcript segment
```

Partial transcripts:

* are displayed immediately
* may change
* should not normally be persisted as canonical transcript
* must not trigger permanent meeting intelligence objects

Final transcript segments:

* are persisted
* become meeting evidence
* enter the intelligence pipeline
* may be embedded/indexed
* receive stable segment IDs

This distinction is critical because streaming STT providers frequently revise partial hypotheses.

---

## Audio Pipeline Boundary

Browser audio transport and STT provider transport are separate concerns.

```text
Browser microphone
        ↓
Application realtime connection
        ↓
Audio/session layer
        ↓
Transcriber interface
        ↓
External STT provider
```

Do not expose the STT provider directly to the browser unless a future architecture explicitly requires it.

This keeps:

* credentials server-side
* provider switching possible
* transcript normalization centralized
* business logic independent from STT vendors

---

## Canonical Transcript Format

Different STT providers return different response structures.

Normalize all provider responses into an internal model before the rest of the application consumes them.

Example:

```go
type TranscriptEvent struct {
    MeetingID string
    SegmentID string

    Text string

    SpeakerID *string

    StartTime float64
    EndTime   float64

    Confidence *float64

    Final bool
}
```

No downstream meeting-domain code should depend on vendor-specific transcript structures.

---

## Provider Failure Isolation

External AI providers must never control the lifecycle of a meeting.

```text
Meeting
   │
   ├── Audio stream
   │
   ├── Transcript
   │
   ├── Intelligence
   │
   └── Embeddings
```

Failure priority:

```text
LLM unavailable
→ transcription continues

Embedding unavailable
→ transcription and intelligence continue

Intelligence unavailable
→ transcript continues and remains persisted

STT unavailable
→ meeting session remains alive and reports degraded state
```

Where practical, failed downstream processing should be retryable.

---

## Development Modes

Support explicit development modes eventually.

### Cloud Development

```text
Browser
   ↓
Go
   ↓
Cloud STT
   ↓
Cloud LLM
   ↓
Cloud Embeddings
```

This is the initial supported development mode.

### Local AI Development

```text
Browser
   ↓
Go
   ├── Local STT
   ├── Local LLM
   └── Local Embeddings
```

This is optional and should be implemented after the primary meeting pipeline works.

### Mixed Development

Individual providers may be independently selected.

Example:

```env
STT_PROVIDER=cloud
LLM_PROVIDER=ollama
EMBEDDING_PROVIDER=local
```

The architecture must not assume that all AI capabilities come from the same vendor or execution environment.

---

## Implementation Priority Amendment

The early development order should be:

```text
1. Repository/bootstrap
2. PostgreSQL + migrations
3. Core meeting domain
4. Meeting REST API
5. Realtime WebSocket session
6. Browser microphone capture
7. Transcriber interface
8. First cloud streaming STT adapter
9. Transcript normalization
10. Partial/final transcript handling
11. Transcript persistence
12. Live transcript UI
13. Meeting lifecycle/reconnection handling
14. IntelligenceProvider interface
15. First cloud LLM adapter
16. Incremental MeetingState
17. Meeting finalization
18. Knowledge ingestion
19. EmbeddingProvider interface
20. pgvector/RAG
21. Business-context intelligence
22. Local AI providers
23. Advanced copilot capabilities
```

The agent must **not begin by installing local models**.

The first major engineering milestone is:

> Browser microphone → Go backend → streaming STT → normalized transcript events → live UI → persistent transcript.

Only after this pipeline is reliable should substantial LLM/RAG functionality be introduced.

---

## Development Philosophy

Treat AI models as replaceable infrastructure.

The application's valuable intellectual property should live in:

* meeting state management
* transcript processing
* business-context retrieval
* evidence tracking
* entity resolution
* decision/action detection
* organizational memory
* realtime UX

It should **not** live in assumptions about a particular model vendor.

A provider should be replaceable without rewriting meeting-domain logic.

The system should therefore remain:

> Cloud-first for simplicity, provider-agnostic by architecture, and local-capable when there is a concrete reason to self-host.


---

# 7. Transcript Event Model

Conceptually:

```json
{
  "meetingId": "...",
  "segmentId": "...",
  "speakerId": "...",
  "text": "We should prioritize EffectiveDate.",
  "startTime": 124.2,
  "endTime": 127.8,
  "isFinal": true,
  "confidence": 0.94
}
```

Transcript segments are immutable evidence where practical.

Later AI processing should reference their IDs.

---

# 8. Intelligence Engine

Do NOT invoke an expensive LLM independently for every transcript sentence.

Instead:

```text
Transcript Events
       ↓
Buffer
       ↓
Boundary Detection
       ↓
Meaningful Chunk
       ↓
Context Retrieval
       ↓
LLM Analysis
       ↓
MeetingState Patch
```

Possible boundaries:

* silence
* speaker transition
* topic change
* elapsed time
* token threshold
* explicit decision language

Start with a simple time/token buffer.

Improve semantic boundary detection later.

---

# 9. Meeting State

Maintain a continuously evolving structured state.

Example:

```json
{
  "currentTopic": "DLP pricing selection",
  "topics": [],
  "entities": [],
  "issues": [],
  "decisions": [],
  "proposals": [],
  "actionItems": [],
  "openQuestions": [],
  "risks": []
}
```

Important distinction:

```text
Proposal != Decision
Question != Action Item
Discussion != Conclusion
```

Never automatically convert suggestions into decisions.

A decision should have supporting evidence from the transcript.

---

# 10. Structured Entities

## Decision

```text
id
meetingId
title
description
status
confidence
sourceSegmentIds[]
createdAt
```

Possible status:

```text
proposed
confirmed
superseded
```

## Action Item

```text
id
meetingId
description
assignee
dueDate
status
sourceSegmentIds[]
```

## Issue

```text
id
meetingId
title
description
status
relatedEntities[]
sourceSegmentIds[]
```

## Open Question

```text
id
meetingId
question
status
sourceSegmentIds[]
```

Keep schemas practical. Add fields only when needed.

---

# 11. Business Knowledge / RAG

Business context is a core feature.

Example:

Someone says:

```text
"This looks like the issue from 158069."
```

The system may resolve:

```text
158069
    ↓
PADM2-158069
    ↓
Known project ticket
    ↓
Relevant historical context
```

The Intelligence Engine receives retrieved context together with the current transcript chunk.

Retrieval should consider:

* project
* entity names
* ticket numbers
* semantic similarity
* recent meetings
* existing decisions
* open issues

Use hybrid retrieval eventually:

```text
Exact identifiers
+
metadata filtering
+
semantic vector search
```

Exact business identifiers should take precedence over fuzzy semantic matching.

---

# 12. Business Glossary

Projects may define terminology such as:

```text
DMS
DLP
PACE
Sales Order
Delivery Note
CR
BRD
FSD
```

Glossary entries may contain:

```text
term
aliases
description
projectId
relatedEntities
```

The glossary improves:

* transcription correction
* entity recognition
* retrieval
* AI interpretation

Never silently rewrite the original transcript.

Corrections/normalized forms should be stored separately.

---

# 13. Contextual Intelligence

Eventually detect situations such as:

## Related Historical Information

```text
Current discussion mentions PADM2-158069.

→ Retrieve previous meetings and documents mentioning it.
```

## Contradiction

```text
Current discussion:
ModifiedDate determines pricing.

Previous confirmed decision:
EffectiveDate determines pricing.

→ Surface potential contradiction.
```

Do not state that something is definitively contradictory unless evidence supports it.

## Ambiguous Decision

Example:

```text
"We'll change the pricing priority."
```

The assistant may detect missing information:

```text
Priority field not specified.
Fallback behavior undefined.
Equal-value behavior undefined.
```

## Forgotten Action Item

If a previously assigned action item becomes relevant:

```text
Related open action:
Quang — verify DLP pricing selection.
```

---

# 14. Evidence First

AI output must be explainable.

Every important generated object should reference supporting transcript segments where possible.

UI example:

```text
Decision

Use EffectiveDate as primary pricing criterion.

Evidence:
[10:04:13] Linh:
"Let's use EffectiveDate first."

[10:04:21] Quang:
"Agreed."
```

Users should be able to inspect why the AI generated something.

---

# 15. Live UI

Primary meeting screen:

```text
┌───────────────────────────────────────────────────────┐
│ DMS Weekly Meeting                         ● Recording │
├──────────────────────────┬────────────────────────────┤
│                          │                            │
│ LIVE TRANSCRIPT          │ AI INSIGHTS                │
│                          │                            │
│ Quang 10:04              │ Current Topic              │
│ ...                      │ DLP Pricing                │
│                          │                            │
│ Linh 10:05               │ Decisions                  │
│ ...                      │ ...                        │
│                          │                            │
│                          │ Action Items               │
│                          │ ...                        │
│                          │                            │
│                          │ Open Questions             │
│                          │ ...                        │
│                          │                            │
└──────────────────────────┴────────────────────────────┘
```

Updates should arrive incrementally through WebSocket events.

Avoid repeatedly replacing the entire page state.

---

# 16. WebSocket Events

Prefer explicit typed events.

Examples:

```text
transcript.partial
transcript.final

meeting.topic.updated

decision.created
decision.updated

action.created
action.updated

issue.created
issue.updated

question.created
question.resolved

context.related
```

Generic events such as:

```json
{
  "type": "update",
  "data": {}
}
```

should be avoided.

---

# 17. Suggested Backend Modules

Start as a modular monolith.

```text
backend/
├── cmd/
├── internal/
│   ├── auth/
│   ├── users/
│   ├── workspace/
│   ├── projects/
│   ├── meetings/
│   ├── realtime/
│   ├── transcription/
│   ├── intelligence/
│   ├── knowledge/
│   ├── retrieval/
│   └── storage/
└── pkg/
```

Do NOT begin with microservices.

Modules can be extracted later if actual scaling boundaries emerge.

---

# 18. Frontend Structure

Example:

```text
src/
├── api/
├── components/
├── features/
│   ├── auth/
│   ├── projects/
│   ├── meetings/
│   ├── transcript/
│   ├── intelligence/
│   └── knowledge/
├── hooks/
├── stores/
├── types/
└── pages/
```

Prefer feature-oriented organization.

---

# 19. MVP Scope

## Phase 1 — Realtime Transcription

Implement:

```text
Create meeting
↓
Browser microphone
↓
Stream audio
↓
Realtime STT
↓
Live transcript
↓
Persist transcript
↓
Meeting history
```

Success criterion:

A user can start a meeting and watch an accurate transcript appear without uploading an audio file.

---

# 20. Phase 2 — Meeting Intelligence

Add extraction of:

* current topic
* decisions
* action items
* issues
* open questions

Maintain incremental `MeetingState`.

Success criterion:

The application produces useful structured information while the conversation is happening.

---

# 21. Phase 3 — Business Context

Add:

* projects
* documents
* business glossary
* embeddings
* pgvector
* contextual retrieval
* previous meeting retrieval
* ticket/entity recognition

Success criterion:

The same sentence can be interpreted differently depending on the project's stored knowledge.

---

# 22. Phase 4 — Meeting Copilot

Add:

* related historical decisions
* unresolved previous actions
* ambiguity detection
* possible contradiction detection
* contextual suggestions
* richer speaker understanding

The AI becomes an active meeting assistant.

---

# 23. Phase 5 — Organizational Memory

Allow queries such as:

```text
"What did we decide about DLP pricing?"

"Why was this change introduced?"

"Which meetings discussed PADM2-158069?"

"What actions assigned to Quang remain unresolved?"

"What decisions changed during the last three months?"
```

Responses must reference original evidence.

---

# 24. Future Integrations

Not MVP.

Potential integrations:

* Google Calendar
* Google Meet
* Microsoft Teams
* Zoom
* Jira
* Confluence
* Google Drive
* Slack
* email

Integrations should enrich the same core domain model rather than introduce separate business logic.

---

# 25. AI Design Rules

LLMs are probabilistic processors, not the source of truth.

Never allow the LLM to directly mutate persistent business data without validation.

Preferred flow:

```text
LLM
 ↓
Structured response
 ↓
Schema validation
 ↓
Application logic
 ↓
Database
```

Use strict structured output/schema validation.

Reject malformed outputs.

Never parse important LLM responses using fragile string manipulation.

---

# 26. AI Context Rules

Do not send the entire transcript on every analysis request.

Use:

```text
Current transcript chunk
+
Recent conversation window
+
Current MeetingState
+
Retrieved project context
```

Summarize/compress older context when necessary.

This keeps latency and token usage controlled.

---

# 27. Concurrency

Assume multiple meetings may happen simultaneously.

Every realtime object must be scoped by:

```text
organizationId
workspaceId
projectId
meetingId
```

Never rely on process-global meeting state.

Meeting processing should be independently cancellable.

Go contexts should propagate cancellation when meetings terminate.

---

# 28. Failure Handling

A meeting must not fail because the AI layer temporarily fails.

Priorities:

```text
Audio capture
    ↓
Transcript
    ↓
Persistence
    ↓
AI intelligence
```

If intelligence processing fails:

```text
meeting continues
transcription continues
transcript remains safe
AI processing may retry
```

Preserving meeting evidence is more important than producing live insights.

---

# 29. Privacy

Meeting data may contain sensitive business information.

Design for:

* workspace isolation
* project authorization
* encrypted transport
* secure credentials
* configurable recording retention
* deletion
* auditability

Never expose knowledge from another workspace through vector retrieval.

All retrieval queries must enforce authorization/tenant filters.

---

# 30. Performance Philosophy

Optimize architecture for perceived realtime behavior, not unnecessary theoretical scale.

Targets should eventually feel approximately like:

```text
Audio → partial transcript       near realtime
Final transcript                seconds
AI insight                      several seconds acceptable
Historical retrieval            interactive
```

Do not block transcription while waiting for AI analysis.

---

# 31. Engineering Rules

Coding agents must:

* prefer simple implementations
* avoid speculative abstractions
* avoid unnecessary dependencies
* keep modules focused
* use explicit types
* validate external data
* handle errors explicitly
* keep AI providers behind interfaces
* keep STT providers behind interfaces
* write migrations for database changes
* keep API contracts documented
* preserve backward compatibility where practical
* write tests around business-critical logic

Do not:

* create microservices prematurely
* create generic repositories for every model
* abstract code used once without reason
* add Kafka because realtime exists
* add Kubernetes because deployment exists
* add Redis where PostgreSQL is sufficient
* allow AI-generated data to bypass validation
* mix transcription-provider code with business logic
* overengineer the MVP

---

# 32. Initial Development Priority

Agents should work in this order:

```text
1. Project/bootstrap
2. Database/domain models
3. Meeting creation
4. WebSocket meeting session
5. Browser microphone capture
6. STT provider interface
7. Realtime transcription
8. Transcript persistence
9. Live transcript UI
10. Meeting history
11. Intelligence Engine
12. Structured MeetingState
13. Knowledge ingestion
14. Embeddings/RAG
15. Business-context intelligence
16. Copilot capabilities
```

Do not implement advanced AI features before the realtime transcript pipeline is reliable.

---

# 33. MVP Definition of Done

The first meaningful release is complete when:

```text
User creates project
        ↓
User starts meeting
        ↓
Browser captures microphone
        ↓
Speech appears as realtime transcript
        ↓
Transcript is persisted
        ↓
AI continuously identifies:
    - topics
    - decisions
    - action items
    - issues
    - questions
        ↓
Meeting ends
        ↓
Structured meeting summary is generated
        ↓
Meeting can be reopened later
```

No manual audio upload should be required.

---

# 34. Long-Term Goal

The final product should evolve from:

```text
Speech-to-Text
```

into:

```text
Meeting Notes
      ↓
Meeting Intelligence
      ↓
Project Memory
      ↓
Organizational Memory
      ↓
Realtime Business Copilot
```

The competitive/technical value is **not transcription itself**.

The core value is:

> Understanding conversations in the context of what the organization already knows, preserving that understanding across meetings, and making decisions, actions, issues, and historical reasoning retrievable and traceable.

Every architectural decision should support that direction without unnecessarily complicating the current development phase.
