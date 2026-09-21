## 1. Purpose

This document describes the technical architecture of the Meeting Intelligence Platform.

Use:

* `context.md` for product vision, business requirements, domain concepts, and roadmap.
* `ARCHITECTURE.md` for system structure, component boundaries, runtime flows, protocols, and technical decisions.
* `AGENTS.md` for rules governing how AI coding agents modify the repository.

This document represents the **current intended architecture**, not every possible future architecture.

Update it when an implementation materially changes a system boundary or architectural decision.

---

# 2. Architectural Goals

The system should support:

* realtime meeting transcription
* incremental AI understanding
* business-context retrieval
* persistent organizational knowledge
* multiple concurrent meetings
* provider-independent AI integrations
* failure isolation
* tenant isolation
* evidence-backed AI output

The architecture should remain as simple as possible while supporting these goals.

---

# 3. System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                         Browser                             │
│                                                             │
│ React / TypeScript                                          │
│                                                             │
│ ┌─────────────┐ ┌──────────────┐ ┌───────────────────────┐ │
│ │ Microphone  │ │ Transcript   │ │ Meeting Intelligence  │ │
│ │ Capture     │ │ UI           │ │ UI                    │ │
│ └──────┬──────┘ └──────▲───────┘ └──────────▲────────────┘ │
└────────┼───────────────┼─────────────────────┼──────────────┘
         │ audio         │ events              │ events
         │               │                     │
         ▼               │                     │
┌─────────────────────────────────────────────────────────────┐
│                       Go Backend                            │
│                                                             │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ REST + Realtime Transport                               │ │
│ └───────────────────────┬─────────────────────────────────┘ │
│                         │                                   │
│ ┌───────────────────────▼─────────────────────────────────┐ │
│ │ Meeting Runtime                                         │ │
│ └───────┬───────────────────────────┬─────────────────────┘ │
│         │                           │                       │
│         ▼                           ▼                       │
│ ┌──────────────┐           ┌─────────────────────┐          │
│ │ Transcription│           │ Intelligence Engine │          │
│ └──────┬───────┘           └──────────┬──────────┘          │
│        │                              │                     │
│        │                              ▼                     │
│        │                    ┌─────────────────────┐          │
│        │                    │ Retrieval / RAG     │          │
│        │                    └──────────┬──────────┘          │
│        │                              │                     │
│ ┌──────▼──────────────────────────────▼───────────────────┐ │
│ │                   Persistence                           │ │
│ └────────────────────────┬────────────────────────────────┘ │
└──────────────────────────┼──────────────────────────────────┘
                           │
              ┌────────────▼────────────┐
              │ PostgreSQL + pgvector   │
              └─────────────────────────┘

External providers:

Go Backend
   ├── Streaming STT Provider
   ├── LLM Provider
   ├── Embedding Provider
   └── Object Storage
```

---

# 4. Architectural Style

Start as a **modular monolith**.

```text
Frontend
   │
   ▼
Go Application
   ├── Auth
   ├── Projects
   ├── Meetings
   ├── Realtime
   ├── Transcription
   ├── Intelligence
   ├── Knowledge
   ├── Retrieval
   └── Storage
         │
         ▼
     PostgreSQL
```

Modules represent logical boundaries but initially execute within the same backend process.

Do not create network boundaries where function/module boundaries are sufficient.

Microservices may be considered later only when demonstrated operational or scaling requirements justify them.

---

# 5. Repository Structure

Target structure:

```text
/
├── AGENTS.md
├── context.md
├── ARCHITECTURE.md
│
├── backend/
│   ├── cmd/
│   │   └── server/
│   │
│   ├── internal/
│   │   ├── auth/
│   │   ├── users/
│   │   ├── workspaces/
│   │   ├── projects/
│   │   ├── meetings/
│   │   ├── realtime/
│   │   ├── transcription/
│   │   ├── intelligence/
│   │   ├── knowledge/
│   │   ├── retrieval/
│   │   └── storage/
│   │
│   └── migrations/
│
├── frontend/
│   └── src/
│       ├── api/
│       ├── components/
│       ├── features/
│       ├── hooks/
│       ├── pages/
│       ├── stores/
│       └── types/
│
└── scripts/
```

Structure may evolve when implementation provides a concrete reason.

---

# 6. Core Domain Hierarchy

```text
Organization
    │
    ▼
Workspace
    │
    ▼
Project
    │
    ├── Meetings
    ├── Documents
    ├── Glossary
    ├── Tickets
    ├── Decisions
    ├── Issues
    └── Knowledge
```

`Project` is the primary business-context boundary.

`Workspace` is an authorization and knowledge-isolation boundary.

---

# 7. Meeting Runtime

An active meeting has an isolated runtime.

Conceptually:

```text
MeetingSession
├── identity
├── participants
├── audio stream
├── transcriber
├── transcript buffer
├── MeetingState
├── intelligence processor
└── cancellation context
```

Each meeting runtime must be independently cancellable.

No process-global active meeting state.

---

# 8. Meeting Lifecycle

```text
Create Meeting
      │
      ▼
Start Meeting
      │
      ▼
Create MeetingSession
      │
      ├── initialize STT
      ├── load project context
      └── establish realtime connection
      │
      ▼
ACTIVE
      │
      ├── receive audio
      ├── produce transcript
      ├── persist final segments
      ├── analyze chunks
      └── publish UI events
      │
      ▼
End Meeting
      │
      ├── stop audio/STT
      ├── flush transcript
      ├── finalize intelligence
      └── persist final state
      │
      ▼
COMPLETED
```

---

# 9. Realtime Audio Flow

```text
Browser Microphone
       │
       │ audio frames/chunks
       ▼
Realtime Connection
       │
       ▼
MeetingSession
       │
       ▼
Transcriber
       │
       ▼
Streaming STT Provider
```

The browser does not communicate directly with the STT provider in the initial architecture.

Reasons:

* credentials remain server-side
* provider can be replaced
* backend controls session lifecycle
* transcript events can be normalized
* business logic remains provider-independent

---

# 10. Transcript Flow

```text
STT Provider
     │
     ▼
Provider Adapter
     │
     ▼
TranscriptEvent
     │
     ├──────────── partial ───────────► Live UI
     │
     └──────────── final
                      │
                      ▼
                 Persistence
                      │
                      ▼
               Intelligence Buffer
```

The canonical internal transcript event is provider-independent.

Conceptually:

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

Provider-specific response structures must not escape the transcription adapter.

---

# 11. Partial vs Final Transcript

This distinction is fundamental.

```text
PARTIAL

"I think we should use Modif—"

          ↓ provider revises

"I think we should use EffectiveDate."
```

Partial transcript:

* volatile
* displayed in UI
* replaceable
* not canonical evidence
* does not create permanent intelligence objects

Final transcript:

* stable
* persisted
* assigned stable segment ID
* becomes evidence
* enters intelligence processing

---

# 12. Intelligence Pipeline

Final transcript events enter a buffer.

```text
Final Transcript
      │
      ▼
Transcript Buffer
      │
      ▼
Boundary Detection
      │
      ▼
Analysis Chunk
      │
      ├───────────────┐
      ▼               ▼
Recent Context    Knowledge Retrieval
      │               │
      └───────┬───────┘
              ▼
      IntelligenceProvider
              │
              ▼
      MeetingStatePatch
              │
              ▼
       Schema Validation
              │
              ▼
       Business Validation
              │
              ▼
         MeetingState
              │
        ┌─────┴─────┐
        ▼           ▼
    Persistence   Live UI
```

LLM analysis must not block transcription.

---

# 13. Meeting State

`MeetingState` represents the application's current understanding of the meeting.

Conceptually:

```text
MeetingState
├── currentTopic
├── topics
├── entities
├── proposals
├── decisions
├── actionItems
├── issues
├── openQuestions
└── risks
```

Updates should preferably be patches rather than complete state regeneration.

Example:

```text
MeetingState
     +
MeetingStatePatch
     ↓
Validated Updated State
```

This reduces unnecessary model output and prevents unrelated state from being accidentally overwritten.

---

# 14. Evidence Model

AI-generated business objects should reference transcript evidence.

```text
Decision
   │
   ├── sourceSegmentId #124
   └── sourceSegmentId #128
```

This enables:

```text
AI Insight
    ↓
Evidence
    ↓
Original Transcript
```

The transcript remains the primary meeting evidence.

AI interpretation is derived data.

---

# 15. Knowledge Architecture

Knowledge originates from:

```text
Meetings
Documents
Tickets
BRDs
FSDs
SOPs
Business glossary
Decisions
Issues
```

Ingestion:

```text
Source
  │
  ▼
Parse
  │
  ▼
Normalize
  │
  ▼
Chunk
  │
  ▼
Metadata
  │
  ▼
EmbeddingProvider
  │
  ▼
pgvector
```

Original source information must remain identifiable.

---

# 16. Retrieval Architecture

Retrieval should eventually combine:

```text
Exact identifiers
       +
Metadata filtering
       +
Semantic similarity
       +
Recency/context
```

Example:

```text
"issue 158069"
       │
       ▼
Identifier detection
       │
       ▼
PADM2-158069
       │
       ├── ticket
       ├── related meetings
       ├── previous decisions
       └── documents
```

Do not use vector search when exact structured lookup is more reliable.

---

# 17. Tenant-Safe Retrieval

Every retrieval operation must be scoped before results are returned.

Conceptually:

```sql
WHERE workspace_id = ?
AND project_id = ?
```

then apply relevant retrieval logic.

Never:

```text
search everything
      ↓
retrieve unauthorized data
      ↓
filter afterward
```

Vector retrieval follows the same isolation rules as normal database queries.

---

# 18. Provider Architecture

External AI systems are adapters.

```text
                  ┌── Cloud STT
Transcriber ──────┤
                  └── Local STT [future]


                         ┌── Cloud LLM
IntelligenceProvider ────┤
                         └── Local LLM [future]


                       ┌── Cloud embeddings
EmbeddingProvider ─────┤
                       └── Local embeddings [future]
```

Application logic depends on interfaces.

Provider implementations depend on vendor SDKs/APIs.

---

# 19. Provider Configuration

Example:

```env
STT_PROVIDER=cloud
LLM_PROVIDER=cloud
EMBEDDING_PROVIDER=cloud
```

Future:

```env
STT_PROVIDER=whisper_local
LLM_PROVIDER=ollama
EMBEDDING_PROVIDER=local
```

Mixed configurations must remain possible.

```env
STT_PROVIDER=cloud
LLM_PROVIDER=ollama
EMBEDDING_PROVIDER=local
```

---

# 20. Persistence Architecture

Primary persistent store:

```text
PostgreSQL
    +
pgvector
```

PostgreSQL contains authoritative application state.

Examples:

```text
users
organizations
workspaces
projects
meetings
participants
transcript_segments
decisions
action_items
issues
questions
documents
knowledge_chunks
embeddings
```

Exact schema belongs in migrations/code rather than this document.

---

# 21. Redis

Redis is optional infrastructure.

Introduce it only when a demonstrated requirement exists.

Potential uses:

```text
ephemeral active meeting state
distributed pub/sub
short-lived cache
distributed coordination
```

Do not use Redis as canonical storage.

A single-instance MVP should not require Redis unless implementation demonstrates a concrete need.

---

# 22. Object Storage

Object storage may eventually contain:

* meeting recordings
* uploaded documents
* generated artifacts

Store metadata/references in PostgreSQL.

Do not store large binary recordings directly in PostgreSQL without a specific reason.

---

# 23. Failure Isolation

The architecture must degrade gracefully.

```text
                  Meeting
                     │
          ┌──────────┼──────────┐
          ▼          ▼          ▼
         STT     Intelligence  Embeddings
          │          │          │
          │          X          X
          │
          ▼
      Transcript
          │
          ▼
      Persistence
```

LLM failure:

```text
transcription continues
```

Embedding failure:

```text
meeting continues
```

Retrieval failure:

```text
analysis may continue with reduced context
```

Intelligence failure:

```text
transcript remains safe
```

STT failure:

```text
meeting remains alive
client receives degraded-state/error event
recovery may be attempted
```

---

# 24. Concurrency Model

Each active meeting is logically independent.

Go should use:

* goroutines where concurrent processing is appropriate
* channels where ownership/event flow benefits from them
* `context.Context` for cancellation
* bounded queues/buffers where backpressure matters

Do not create goroutines without lifecycle ownership.

Every long-running goroutine must have a termination mechanism.

---

# 25. Backpressure

Audio/transcript processing must not create unbounded memory growth.

Conceptually:

```text
Producer
   │
   ▼
Bounded Buffer
   │
   ▼
Consumer
```

If downstream AI processing becomes slow, it must not block audio ingestion indefinitely.

Intelligence processing can lag behind transcription.

Meeting evidence must take priority over enrichment.

---

# 26. Realtime Client Events

Use typed semantic events.

Examples:

```text
meeting.started
meeting.ended
meeting.degraded

transcript.partial
transcript.final

topic.updated

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

Events should contain enough identity information to apply incremental frontend updates.

---

# 27. REST vs Realtime Responsibilities

Use REST for resource lifecycle and normal queries.

Examples:

```text
create project
get project
create meeting
get meeting history
get meeting details
search knowledge
```

Use realtime transport for active meeting events.

Examples:

```text
audio
partial transcript
final transcript
live decisions
live actions
meeting status
```

Do not force everything through WebSockets merely because the application contains realtime functionality.

---

# 28. Authentication and Authorization

Authentication establishes identity.

Authorization determines access to:

```text
organization
workspace
project
meeting
knowledge
```

Authorization must be checked server-side.

Realtime connections require the same authorization guarantees as REST endpoints.

A valid WebSocket connection must not imply access to arbitrary meeting IDs.

---

# 29. Security Boundaries

```text
Browser
   │
   │ untrusted input
   ▼
Backend
   │
   ├── authorization boundary
   ├── validation boundary
   ├── provider credential boundary
   └── tenant isolation boundary
```

Provider API keys never belong in frontend bundles.

LLM output is also untrusted input.

It must pass validation before affecting application state.

---

# 30. Deployment — Initial

Initial deployment should remain simple.

```text
Frontend
    │
    └── static/web hosting

Go Backend
    │
    └── container/application host

PostgreSQL
    │
    └── managed or containerized DB

External AI Providers
```

Do not introduce Kubernetes for the initial system.

---

# 31. Local Development

Initial local development:

```text
localhost
│
├── React/Vite
├── Go backend
└── PostgreSQL
       +
    pgvector
```

External cloud APIs provide STT/LLM/embeddings.

A developer should not need a local GPU or downloaded AI model to start the project.

---

# 32. Future Local AI Architecture

Optional later:

```text
React
   ↓
Go
   ├── Local STT service
   ├── Local LLM service
   └── Local embedding service
```

Potential implementations may include Whisper-compatible STT and Ollama/llama.cpp-style inference.

These are provider adapters, not changes to the core business architecture.

---

# 33. Scaling Direction

Do not implement this architecture prematurely.

If scale eventually requires distribution:

```text
                     Load Balancer
                          │
              ┌───────────┴───────────┐
              ▼                       ▼
          Go Instance             Go Instance
              │                       │
              └───────────┬───────────┘
                          ▼
                     PostgreSQL
```

Realtime session coordination may then justify shared ephemeral infrastructure such as Redis.

Only after demonstrated requirements should individual processing components be extracted.

Possible future extraction:

```text
Transcription workers
Intelligence workers
Knowledge ingestion workers
```

Extraction should preserve existing module/provider contracts where possible.

---

# 34. Architectural Non-Goals

The initial architecture is intentionally NOT:

* microservice-based
* Kubernetes-dependent
* Kafka-dependent
* GPU-dependent
* local-model-dependent
* event-sourcing based
* CQRS-heavy
* serverless-first
* multi-database
* distributed by default

These approaches are not forbidden forever.

They require a concrete problem that they solve.

---

# 35. Architecture Decision Rule

When considering a new architectural component, answer:

```text
What current problem does it solve?

Why can't the existing architecture solve it?

What complexity does it introduce?

What happens if we postpone it?
```

If there is no concrete current problem, postpone the component.

---

# 36. Architecture Evolution

Architecture is expected to evolve.

Changes should be driven by:

```text
observed requirements
measured bottlenecks
reliability problems
security requirements
product requirements
operational experience
```

not speculative scale.

When a major architectural decision changes, update this document.

For decisions requiring historical reasoning, record an Architecture Decision Record.

Suggested structure:

```text
docs/
└── adr/
    ├── 0001-modular-monolith.md
    ├── 0002-postgresql-pgvector.md
    └── 0003-server-mediated-stt.md
```

An ADR explains **why a decision was made**.

`ARCHITECTURE.md` explains **what the architecture currently is**.

---

# 37. Current Critical Path

The current architectural priority is:

```text
Browser Microphone
        ↓
Realtime Transport
        ↓
MeetingSession
        ↓
Transcriber
        ↓
Cloud Streaming STT
        ↓
TranscriptEvent
        ↓
Partial ─────────────► UI
        ↓
Final
        ↓
PostgreSQL
        ↓
Intelligence Buffer
        ↓
IntelligenceProvider
        ↓
MeetingStatePatch
        ↓
Live UI
```

Get this path reliable before expanding the architecture.

---

# 38. Architectural Principle

The architecture should make the important parts stable and the replaceable parts replaceable.

Stable:

```text
Meeting domain
Transcript model
Evidence model
MeetingState
Business knowledge
Authorization boundaries
```

Replaceable:

```text
STT provider
LLM provider
Embedding provider
Object storage provider
Deployment infrastructure
```

The system's value should remain in its business and meeting intelligence architecture rather than in dependency on any individual AI vendor.
