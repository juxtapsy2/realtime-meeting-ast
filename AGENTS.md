## Purpose

This file defines how AI coding agents must operate inside this repository.

`context.md` defines **what the product is and where it is going**.
`ARCHITECTURE.md` for system structure, component boundaries, runtime flows, protocols, and technical decisions.
`AGENTS.md` defines **how agents work on it**.

All agents must read this file before making changes.

For product architecture, domain concepts, roadmap, and product intent, read:

```text
context.md
```

If implementation decisions conflict with `context.md`, do not silently override the product architecture. Identify the conflict and choose the smallest solution consistent with the existing direction.

---

# 1. Core Working Principle

Operate as an autonomous software engineer, not a code generator.

For every task:

```text
Understand
    ↓
Inspect
    ↓
Plan
    ↓
Implement
    ↓
Validate
    ↓
Review
    ↓
Complete
```

Do not immediately start writing code after receiving a task.

First understand the requirement and inspect the relevant existing implementation.

---

# 2. Before Starting Any Task

Always:

1. Read `AGENTS.md`.
2. Read relevant sections of `context.md`.
3. Inspect relevant files.
4. Understand existing patterns before introducing new ones.
5. Determine the smallest implementation that satisfies the requirement.
6. Identify affected modules and data flows.
7. Consider failure cases.
8. Establish how the result will be validated.

Do not scan or redesign the entire repository when the task affects only a small area.

---

# 3. Scope Discipline

Implement only what is required for the current task.

Do not turn:

```text
"Add meeting creation"
```

into:

```text
meeting creation
+ calendar integration
+ notification system
+ scheduling engine
+ recurring meetings
+ permissions redesign
```

unless those additions are necessary for the requested feature.

Prefer:

> smallest complete solution

over:

> largest theoretically reusable solution

---

# 4. Architecture Rules

The application starts as a **modular monolith**.

Do not introduce microservices unless explicitly required.

Primary architecture:

```text
React / TypeScript
        ↓
Go Backend
        ↓
PostgreSQL
        ↓
pgvector when semantic retrieval is introduced
```

Redis and object storage should be introduced only when required by concrete features.

Do not introduce infrastructure because it may theoretically be useful later.

Examples of technologies that must NOT be introduced without clear justification:

```text
Kafka
RabbitMQ
Kubernetes
service mesh
distributed workflow engines
multiple backend languages
complex event infrastructure
```

Realtime functionality alone is not sufficient justification for Kafka.

---

# 5. Backend Rules

The primary backend language is Go.

Prefer:

* standard library where practical
* explicit dependencies
* explicit error handling
* small interfaces
* dependency injection through constructors
* context propagation
* clear domain boundaries
* typed request/response models

Avoid:

* unnecessary reflection
* hidden global state
* giant service objects
* generic abstractions without real reuse
* unnecessary framework-like internal code
* deeply nested package hierarchies

Prefer code that another engineer can understand quickly.

---

# 6. Frontend Rules

Frontend:

```text
React
TypeScript
Vite
TailwindCSS
```

Prefer:

* functional components
* typed API contracts
* feature-oriented organization
* reusable components when actual reuse exists
* explicit loading/error states
* incremental realtime updates

Avoid:

* giant components
* excessive global state
* unnecessary state libraries
* duplicated API models
* premature component abstraction
* storing server state in multiple places

Realtime events should update only the affected state.

Do not refetch the entire meeting whenever one transcript event arrives.

---

# 7. AI Provider Boundaries

AI models are infrastructure dependencies.

Business logic must not depend directly on a specific vendor.

Use provider boundaries such as:

```text
Transcriber
IntelligenceProvider
EmbeddingProvider
```

Vendor SDKs must remain inside provider implementations.

Application/domain code should consume normalized internal types.

Do not leak vendor response objects through the application.

---

# 8. Cloud-First Development

Initial development is cloud-first.

Agents must NOT begin by:

* downloading Hugging Face models
* configuring CUDA
* configuring local inference clusters
* installing Ollama
* building Python model servers
* optimizing local inference

unless the current task explicitly requires local AI support.

Initial priority:

```text
Browser microphone
    ↓
Go backend
    ↓
Cloud streaming STT
    ↓
Normalized transcript
    ↓
Live UI
```

Local models are future provider implementations.

---

# 9. Realtime Rules

Realtime processing must distinguish:

```text
partial transcript
        ≠
final transcript
```

Partial transcripts:

* may change
* are primarily UI state
* must not create permanent decisions/actions
* should not normally become canonical transcript records

Final transcripts:

* are stable evidence
* are persisted
* receive stable IDs
* enter intelligence processing

Never treat repeated partial STT hypotheses as separate transcript messages.

---

# 10. Meeting Reliability Priority

Meeting evidence is more important than AI enrichment.

Priority:

```text
1. Meeting session
2. Audio/transcription
3. Transcript persistence
4. Intelligence
5. Embeddings
6. Additional AI features
```

If an LLM fails, transcription must continue.

If embedding generation fails, the meeting must continue.

If AI analysis fails, preserve the transcript and allow analysis to be retried.

Do not couple meeting availability to optional AI features.

---

# 11. LLM Rules

Never treat LLM output as trusted application data.

Required flow:

```text
LLM
 ↓
Structured output
 ↓
Schema validation
 ↓
Business validation
 ↓
Persistence
```

Do not parse critical AI output using fragile string matching.

Prefer structured schemas.

LLM-generated decisions, actions, issues, and questions should reference supporting transcript segments where possible.

Never silently fabricate business context.

---

# 12. Context Management

Do not repeatedly send the entire meeting transcript to an LLM.

Prefer:

```text
Current transcript chunk
+
Recent transcript window
+
Current MeetingState
+
Relevant retrieved knowledge
```

Older information should eventually be summarized or retrieved on demand.

Optimize context for relevance rather than raw quantity.

---

# 13. Business Knowledge Rules

Exact business identifiers should be resolved before semantic search when possible.

Example:

```text
PADM2-158069
```

should first be treated as a potential exact identifier.

Preferred retrieval:

```text
exact identifier
        ↓
metadata lookup
        ↓
semantic retrieval
```

Do not rely entirely on embeddings for structured business identifiers.

---

# 14. Multi-Tenant Safety

Every relevant query must respect ownership boundaries.

Typical hierarchy:

```text
organization
    ↓
workspace
    ↓
project
    ↓
meeting
```

Never perform vector retrieval across unauthorized workspaces.

Tenant filtering must happen during retrieval, not after retrieving potentially unauthorized information.

---

# 15. Database Rules

Use migrations for schema changes.

Never rely on manual database edits.

Prefer:

* explicit constraints
* foreign keys where appropriate
* indexes justified by queries
* timestamps
* clear ownership relationships

Avoid speculative columns.

Do not create database fields merely because they might someday be useful.

Before adding a field, identify which current requirement consumes it.

---

# 16. API Rules

APIs should have predictable resource-oriented contracts.

Validate all external input.

Return meaningful errors.

Do not expose internal errors, credentials, provider payloads, or stack traces to clients.

Maintain clear separation:

```text
HTTP/WebSocket transport
        ↓
application/service logic
        ↓
domain/storage/provider
```

Transport handlers should not contain substantial business logic.

---

# 17. WebSocket Rules

Use explicit event names.

Prefer:

```text
transcript.partial
transcript.final

meeting.topic.updated

decision.created
decision.updated

action.created
action.updated

issue.created
question.created
```

Avoid generic:

```text
update
message
data
event
```

when the event has known semantics.

WebSocket payloads must have typed contracts shared/documented between backend and frontend.

---

# 18. Error Handling

Errors must not be silently ignored.

For every failure determine whether it is:

```text
fatal
retryable
degraded
user-visible
internal
```

Example:

```text
LLM timeout
→ degraded/retryable

invalid meeting ID
→ user-visible

database unavailable
→ potentially fatal

embedding generation failure
→ degraded/retryable
```

Log enough context to diagnose failures without exposing sensitive information.

---

# 19. Logging

Prefer structured logging.

Include useful identifiers where available:

```text
meetingId
projectId
workspaceId
userId
provider
operation
```

Never log:

* API keys
* access tokens
* passwords
* authorization headers
* unnecessary raw sensitive meeting content

---

# 20. Security

Never:

* hardcode credentials
* expose provider API keys to frontend code
* commit `.env`
* trust client-provided ownership identifiers
* bypass authorization because an endpoint is internal
* allow unrestricted cross-workspace retrieval

All secrets must come from environment/configuration mechanisms.

---

# 21. Dependency Discipline

Before adding a dependency, ask:

1. Can the standard library reasonably handle this?
2. Does an existing dependency already solve it?
3. Is the dependency actively maintained?
4. Does its value justify additional complexity?

Do not add libraries for trivial helpers.

Do not replace working dependencies without a concrete reason.

---

# 22. Abstraction Rule

Do not abstract based on imagined future requirements.

Bad:

```text
GenericRepository<T>
GenericProviderFactory<T>
UniversalEventProcessor<T>
AbstractMeetingStrategyFactory
```

created before multiple implementations actually require them.

Good:

```text
Transcriber
```

because multiple STT providers are an intentional architectural requirement.

Create abstractions at real boundaries, not everywhere.

---

# 23. Refactoring Rule

Do not perform unrelated large refactors while implementing a feature.

If existing code requires small restructuring to implement the task safely, perform the minimum necessary refactor.

Keep:

```text
feature changes
```

and

```text
unrelated cleanup
```

conceptually separate.

---

# 24. Testing

Test business-critical behavior.

Prioritize tests for:

* meeting lifecycle
* transcript normalization
* partial/final transcript handling
* authorization
* tenant isolation
* MeetingState updates
* AI structured-output validation
* retrieval filtering
* provider failure behavior

Do not write meaningless tests purely to increase coverage.

Prefer behavior-oriented tests.

---

# 25. Validation

Before declaring a task complete, run relevant validation.

Backend where applicable:

```bash
gofmt
go vet ./...
go test ./...
```

Frontend where applicable:

```bash
npm run lint
npm run test
npm run build
```

Use the repository's actual package manager/scripts if they differ.

Do not claim validation succeeded unless the command was actually executed successfully.

---

# 26. Failure During Validation

If validation fails because of your changes:

```text
inspect
↓
fix
↓
rerun
```

Do not stop at the first failure.

If validation fails because of an unrelated pre-existing problem, report:

* failing command
* failure
* why it appears unrelated
* whether your changed area was otherwise validated

Do not silently modify unrelated code merely to make every repository check green.

---

# 27. Review Your Own Work

Before finishing, inspect the final diff.

Check for:

* accidental changes
* duplicated logic
* debug code
* commented-out code
* unused imports
* temporary files
* exposed secrets
* unnecessary dependencies
* overengineering
* missing error handling
* missing validation
* architecture violations

Ask:

> Is every changed line necessary for the requested task?

If not, simplify.

---

# 28. Documentation

Update documentation when behavior, configuration, architecture, or developer setup materially changes.

Do not produce large documentation updates for trivial implementation details.

When introducing environment variables, document them.

When introducing an architectural boundary, update relevant architectural documentation.

---

# 29. Git Rules

AI agents must not perform repository history operations unless explicitly requested.

Do not automatically:

```text
git commit
git push
git merge
git rebase
git reset
git tag
```

The user controls version history.

Agents may inspect:

```text
git status
git diff
git log
```

when useful for understanding or reviewing work.

Never discard user changes.

---

# 30. Existing User Work

Assume uncommitted changes may belong to the user or another agent.

Do not overwrite or revert changes merely because you did not create them.

When encountering unexpected modifications:

1. inspect them
2. determine whether they conflict with the task
3. preserve them whenever possible
4. work around them safely

Never use destructive commands to obtain a clean working tree.

---

# 31. Task Planning

For non-trivial work, establish a short implementation plan before editing.

The plan should identify:

```text
what changes
where
why
how it will be validated
```

Do not create elaborate plans for trivial fixes.

Planning exists to reduce mistakes, not create ceremony.

---

# 32. Decision Making

When several solutions are valid, prefer in this order:

```text
correctness
↓
simplicity
↓
maintainability
↓
consistency with existing architecture
↓
performance
↓
theoretical flexibility
```

Performance moves higher only when the task is explicitly performance-sensitive.

---

# 33. When Requirements Are Ambiguous

Do not immediately stop for every minor ambiguity.

If a safe, reversible interpretation is obvious and consistent with `context.md`, proceed.

Ask for clarification when the ambiguity would materially affect:

* product behavior
* architecture
* security
* data model
* destructive operations
* major dependency choices

Do not invent business requirements.

---

# 34. Autonomous Problem Solving

Agents are expected to solve implementation-level problems independently.

Examples:

```text
compiler error
test failure
type mismatch
minor integration issue
lint failure
obvious edge case
```

Do not ask the user to solve problems that can be resolved by inspecting the repository.

Escalate when a decision genuinely requires product or architectural input.

---

# 35. Completion Criteria

A task is complete only when:

```text
Requirement implemented
        +
Relevant tests pass
        +
Build/validation passes
        +
Error cases considered
        +
Final diff reviewed
        +
No obvious temporary/debug code remains
```

"Code written" does not mean "task complete."

---

# 36. Final Agent Report

Keep completion reports concise.

Include:

```text
Implemented
- important changes

Validated
- commands/tests actually run

Notes
- meaningful limitations or follow-up concerns
```

Do not provide a long file-by-file narration unless requested.

Do not claim features, tests, or validation that were not actually completed.

---

# 37. Current Product Priority

Until `context.md` indicates otherwise, prioritize the foundation in this order:

```text
Meeting lifecycle
        ↓
Realtime connection
        ↓
Microphone/audio transport
        ↓
Streaming transcription
        ↓
Transcript persistence
        ↓
Reliable live UI
        ↓
Meeting intelligence
        ↓
Business knowledge/RAG
        ↓
Organizational memory
        ↓
Advanced copilot behavior
```

Do not jump ahead to advanced AI features while the underlying realtime meeting pipeline is unreliable.

---

# 38. Guiding Principle

The goal is not to create the most sophisticated codebase.

The goal is to create the **simplest reliable system that correctly implements the product described in `context.md` and can evolve without unnecessary rewrites.**

Agents should optimize for:

> Understandable code, explicit boundaries, reliable behavior, controlled autonomy, and incremental progress.

When uncertain, choose the solution that introduces the least unnecessary complexity while preserving the intended architecture.
