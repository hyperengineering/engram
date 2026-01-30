# Engram — Technical Design

> **Purpose:** Enable AI agents to accumulate, persist, and recall experiential lore across sessions, projects, and distributed development environments.

**Version:** 1.0
**Status:** Draft
**Author:** Clario (Architecture Agent)

---

## Naming Convention

| Term | Role | Description |
|------|------|-------------|
| **Engram** | Central service | Where lore is stored and synchronized |
| **Recall** | Local client | How agents retrieve and contribute lore |
| **Lore** | The knowledge | Individual learnings — the substance itself |

---

## 1. Problem Statement

AI agents performing technical design and implementation work generate valuable insights during their workflows:
- Architectural decisions and their outcomes
- Pattern applications that succeeded or failed
- Edge cases discovered late in implementation
- Interface designs that caused friction
- Testing strategies that proved effective
- Library and framework behavioral gotchas
- Performance characteristics discovered under load

Currently, these insights exist only within a single session context. When a session ends, the lore is lost. Agents cannot benefit from prior experience, leading to:
- Repeated mistakes across similar problems
- No institutional memory accumulation
- Inability to improve design quality over time

---

## 2. Solution Overview

The Engram system:
1. **Captures** structured lore during agent workflows
2. **Embeds** lore content for semantic retrieval
3. **Stores** lore in a local database for low-latency recall
4. **Synchronizes** lore to Engram (central) for cross-environment persistence
5. **Recalls** relevant lore based on semantic similarity to current context

---

## 3. Core Concepts

### 3.1 Lore

A discrete unit of experiential knowledge captured from agent activity.

| Attribute | Type | Description |
|-----------|------|-------------|
| id | Identifier | Globally unique identifier (ULID recommended) |
| content | Text | The lore itself — what was learned |
| context | Text | Where/when this was learned (story, epic, situation) |
| category | Enum | Classification of lore type |
| confidence | Float [0.0-1.0] | How validated/reliable this lore is |
| embedding | Vector | Semantic representation for similarity search |
| source_id | Identifier | Which environment generated this lore |
| created_at | Timestamp | When the lore was captured |
| updated_at | Timestamp | Last modification time |

### 3.2 Lore Categories

| Category | Description | Example | Primary Consumers |
|----------|-------------|---------|-------------------|
| ARCHITECTURAL_DECISION | System-level choices and rationale | "Chose event sourcing over CRUD for audit requirements" | Clario, Hon |
| PATTERN_OUTCOME | Results of applying a design pattern | "Repository pattern added unnecessary abstraction for simple CRUD" | Clario, Blade |
| INTERFACE_LESSON | Contract/API design insights | "Nullable return types caused downstream null checks to proliferate" | Clario, Spark |
| EDGE_CASE_DISCOVERY | Scenarios found during implementation/testing | "Empty collections require special handling in the serializer" | Clario, Spark, Blade |
| IMPLEMENTATION_FRICTION | Design-to-code translation difficulties | "The interface was correct but implementation required async which wasn't anticipated" | Clario, Spark |
| TESTING_STRATEGY | Testing approach insights and techniques | "Integration tests for queue consumers need idempotency verification" | Clario, Spark, Blade |
| DEPENDENCY_BEHAVIOR | Library/framework gotchas and requirements | "ORM generates N+1 queries for this relationship type unless eager loading configured" | Spark, Blade |
| PERFORMANCE_INSIGHT | Performance characteristics and optimizations | "In-memory processing failed at 10k records — streaming approach required" | Clario, Spark, Blade |

**Design note:** Categories are semantically distinct with no overlaps. Security-related learnings fold into existing categories (security edge cases → EDGE_CASE_DISCOVERY, secure patterns → PATTERN_OUTCOME) rather than warranting a separate category.

### 3.3 Confidence Model

Confidence represents how validated a lore entry is:

| Score | Meaning | State |
|-------|---------|-------|
| 0.0 - 0.3 | Hypothesis, unvalidated | Cold |
| 0.3 - 0.6 | Some evidence, limited validation | Warm |
| 0.6 - 0.8 | Validated in multiple contexts | Trusted |
| 0.8 - 1.0 | Repeatedly confirmed, high reliability | Authoritative |

**Confidence adjustments via feedback:**

| Feedback Type | Adjustment | Rationale |
|---------------|------------|-----------|
| `helpful` | +0.08 | Lore was useful and correct — reinforce |
| `not_relevant` | 0 | Lore didn't apply to this context — no penalty (context mismatch, not lore quality) |
| `incorrect` | -0.15 | Lore was wrong or misleading — asymmetric penalty because bad lore is costly |

**Other adjustments:**
- Initial capture: 0.5 - 0.7 (based on source reliability)
- Merge reinforcement: +0.10 (when semantically equivalent lore arrives from different source)
- Decay over time: -0.01 per month if unused and not validated (prevents stale lore)

**Trust signals exposed to agents:**
- `confidence`: The computed trust score
- `validation_count`: How many times marked helpful — high count = battle-tested
- `last_validated`: Recency of last positive feedback — recent = still relevant

---

## 4. System Architecture

### 4.1 Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CENTRAL SERVICE                                │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │    API Layer    │  │    Embedding    │  │        Lore Store           │  │
│  │                 │◀─│    Service      │◀─│    (Primary Database)       │  │
│  │  - Ingest       │  │                 │  │                             │  │
│  │  - Snapshot     │  │  - Generate     │  │  - Persistence              │  │
│  │  - Delta sync   │  │  - Batch        │  │  - Conflict resolution      │  │
│  │  - Health       │  │                 │  │  - Deduplication            │  │
│  └────────┬────────┘  └─────────────────┘  └─────────────────────────────┘  │
│           │                                                                  │
│           │           ┌─────────────────┐                                   │
│           └──────────▶│  Snapshot Store │  (Object storage for DB exports)  │
│                       └─────────────────┘                                   │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                    ════════════════╪════════════════
                         Network Boundary (HTTPS)
                    ════════════════╪════════════════
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
        ▼                           ▼                           ▼
┌───────────────────┐   ┌───────────────────┐   ┌───────────────────┐
│  Devcontainer A   │   │  Devcontainer B   │   │  Devcontainer N   │
│  ┌─────────────┐  │   │  ┌─────────────┐  │   │  ┌─────────────┐  │
│  │   Client    │  │   │  │   Client    │  │   │  │   Client    │  │
│  │   Library   │  │   │  │   Library   │  │   │  │   Library   │  │
│  ├─────────────┤  │   │  ├─────────────┤  │   │  ├─────────────┤  │
│  │   Local     │  │   │  │   Local     │  │   │  │   Local     │  │
│  │   Store     │  │   │  │   Store     │  │   │  │   Store     │  │
│  │  (Replica)  │  │   │  │  (Replica)  │  │   │  │  (Replica)  │  │
│  └─────────────┘  │   │  └─────────────┘  │   │  └─────────────┘  │
│        │          │   │        │          │   │        │          │
│  ┌─────▼───────┐  │   │  ┌─────▼───────┐  │   │  ┌─────▼───────┐  │
│  │   Agents    │  │   │  │   Agents    │  │   │  │   Agents    │  │
│  │  (Spark,    │  │   │  │  (Spark,    │  │   │  │  (Spark,    │  │
│  │   Clario)   │  │   │  │   Clario)   │  │   │  │   Clario)   │  │
│  └─────────────┘  │   │  └─────────────┘  │   │  └─────────────┘  │
└───────────────────┘   └───────────────────┘   └───────────────────┘
```

### 4.2 Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **Engram API Layer** | Authentication, request routing, rate limiting |
| **Embedding Service** | Convert text content to vector representations |
| **Engram Store (Central)** | Primary persistence, conflict resolution, deduplication |
| **Snapshot Store** | Periodic database exports for fast client bootstrap |
| **Recall (Client Library)** | Agent-facing API, local caching, sync orchestration |
| **Local Lore Store** | Low-latency read replica, offline support, pending write queue |

---

## 5. Data Model

### 5.1 Lore Record

```
Lore {
  id:               ULID (primary key)
  content:          TEXT (not null, max 4000 chars)
  context:          TEXT (nullable, max 1000 chars)
  category:         ENUM (not null)
  confidence:       DECIMAL(3,2) (not null, default 0.7)
  embedding:        BLOB (not null, packed float array)
  source_id:        TEXT (not null)
  sources:          TEXT[] (aggregated source IDs after merges)
  validation_count: INTEGER (not null, default 0)
  last_validated:   TIMESTAMP (nullable)
  created_at:       TIMESTAMP (not null)
  updated_at:       TIMESTAMP (not null)
  synced_at:        TIMESTAMP (nullable, local only)
}

Indexes:
  - PRIMARY KEY (id)
  - INDEX (category)
  - INDEX (confidence)
  - INDEX (last_validated)
  - INDEX (created_at)
  - INDEX (synced_at) -- local only, for sync queries
```

**Trust signal fields:**
- `validation_count`: Number of times this lore was marked helpful via feedback
- `last_validated`: When it was last confirmed as helpful — recency indicates continued relevance

### 5.2 Metadata Record

```
Metadata {
  key:    TEXT (primary key)
  value:  TEXT (not null)
}

Required keys:
  - schema_version: "1"
  - embedding_model: "text-embedding-3-small"
  - embedding_dimensions: "1536"
  - last_sync: ISO8601 timestamp (local only)
  - source_id: ULID (local only)
```

### 5.3 Sync Queue (Local Only)

```
SyncQueue {
  id:           INTEGER (primary key, auto-increment)
  lore_id:  ULID (foreign key)
  operation:    ENUM (INSERT, FEEDBACK)
  payload:      JSON
  queued_at:    TIMESTAMP
  attempts:     INTEGER (default 0)
  last_error:   TEXT (nullable)
}
```

---

## 6. Interface Contracts

### 6.1 Recall Client Interface

```
interface Recall {
  // Configuration
  configure(config: RecallConfig): void

  // Lifecycle
  initialize(): Result<void, Error>
  shutdown(): Result<void, Error>

  // Core operations
  record(params: RecordParams): Result<Lore, Error>
  query(params: QueryParams): Result<QueryResult, Error>
  feedback(params: FeedbackParams): Result<FeedbackResult, Error>

  // Session operations
  getSessionLore(): SessionLore[]  // Lore surfaced this session

  // Sync operations
  syncFromEngram(): Result<SyncStats, Error>
  syncToEngram(): Result<SyncStats, Error>

  // Diagnostics
  getStats(): StoreStats
  healthCheck(): HealthStatus
}

struct RecallConfig {
  lorePath:       FilePath        // Local lore database path
  engramUrl:      URL             // Engram central service URL
  apiKey:         Secret
  syncInterval:   Duration (default: 5 minutes)
  autoSync:       boolean (default: true)
  offlineMode:    boolean (default: false)
}

struct RecordParams {
  content:     string (required)
  context:     string (optional)
  category:    LoreCategory (required)
  confidence:  float (optional, default: 0.7)
}

struct QueryParams {
  query:          string (required)
  k:              integer (optional, default: 5)
  minConfidence:  float (optional, default: 0.5)
  categories:     LoreCategory[] (optional, default: all)
}

struct QueryResult {
  lore:         Lore[]
  sessionRefs:  map<string, LoreId>  // e.g., {"L1": "01HXK4...", "L2": "01HXK5..."}
}

struct FeedbackParams {
  helpful:      string[] (optional)  // Session refs (L1, L2), content snippets, or IDs
  not_relevant: string[] (optional)  // Surfaced but didn't apply to this context
  incorrect:    string[] (optional)  // Wrong or misleading — triggers confidence penalty
}

struct FeedbackResult {
  updated: FeedbackUpdate[]
}

struct FeedbackUpdate {
  id:              LoreId
  previous:        float
  current:         float
  validation_count: integer
}

struct SessionLore {
  sessionRef:  string      // L1, L2, etc.
  id:          LoreId
  content:     string      // First 100 chars for recognition
  category:    LoreCategory
  confidence:  float
  source:      string      // "passive" or "query"
}

struct SyncStats {
  pulled:   integer
  pushed:   integer
  merged:   integer
  errors:   integer
  duration: Duration
}
```

**Session tracking:** Recall maintains a list of all lore surfaced during the current session (via passive injection or active query). This enables the `feedback()` operation to reference lore by simple session refs (L1, L2) rather than requiring agents to track IDs manually.

### 6.2 Engram API (Central Service)

```
// Authentication: Bearer token in Authorization header

POST /api/v1/lore
  Request:
    {
      "source_id": "01HXK3...",
      "lore": [
        {
          "id": "01HXK4...",
          "content": "Queue consumers benefit from...",
          "context": "story-2.1 implementation",
          "category": "PATTERN_OUTCOME",
          "confidence": 0.75,
          "created_at": "2024-01-15T10:30:00Z"
        }
      ]
    }
  Response: 200 OK
    {
      "accepted": 3,
      "merged": 1,
      "rejected": 0,
      "errors": []
    }
  Errors:
    - 401 Unauthorized
    - 422 Unprocessable Entity (validation errors)
    - 429 Too Many Requests

GET /api/v1/lore/snapshot
  Response: 200 OK
    Content-Type: application/octet-stream
    Content-Disposition: attachment; filename="lore.db"
    Body: <binary database file>
  Errors:
    - 401 Unauthorized
    - 503 Service Unavailable (snapshot generation in progress)

GET /api/v1/lore/delta?since={timestamp}
  Response: 200 OK
    {
      "lore": [...],
      "deleted_ids": [...],
      "as_of": "2024-01-15T12:00:00Z"
    }
  Errors:
    - 401 Unauthorized
    - 400 Bad Request (invalid timestamp)

POST /api/v1/lore/feedback
  Description: Batch update confidence based on agent feedback
  Request:
    {
      "source_id": "01HXK3...",
      "feedback": [
        {
          "id": "01HXK4...",
          "outcome": "helpful"    // helpful | not_relevant | incorrect
        },
        {
          "id": "01HXK5...",
          "outcome": "helpful"
        },
        {
          "id": "01HXK6...",
          "outcome": "incorrect"
        }
      ]
    }
  Response: 200 OK
    {
      "updates": [
        { "id": "01HXK4...", "previous": 0.80, "current": 0.88, "validation_count": 5 },
        { "id": "01HXK5...", "previous": 0.75, "current": 0.83, "validation_count": 3 },
        { "id": "01HXK6...", "previous": 0.70, "current": 0.55, "validation_count": 2 }
      ]
    }
  Confidence adjustments:
    - helpful: +0.08
    - not_relevant: 0 (no change)
    - incorrect: -0.15
  Errors:
    - 401 Unauthorized
    - 404 Not Found (one or more IDs not found)
    - 422 Unprocessable Entity (invalid outcome value)

GET /api/v1/health
  Response: 200 OK
    {
      "status": "healthy",
      "version": "1.0.0",
      "embedding_model": "text-embedding-3-small",
      "lore_count": 1234,
      "last_snapshot": "2024-01-15T12:00:00Z"
    }
```

---

## 7. Sync Protocol

### 7.1 Bootstrap Sync (On Devcontainer Start)

```
┌─────────────┐                          ┌─────────────┐
│   Recall    │                          │   Engram    │
└──────┬──────┘                          └──────┬──────┘
       │                                        │
       │  GET /api/v1/health                    │
       │───────────────────────────────────────▶│
       │                                        │
       │  200 OK { embedding_model, version }   │
       │◀───────────────────────────────────────│
       │                                        │
       │  [Validate embedding model match]      │
       │                                        │
       │  GET /api/v1/lore/snapshot             │
       │───────────────────────────────────────▶│
       │                                        │
       │  200 OK <binary database>              │
       │◀───────────────────────────────────────│
       │                                        │
       │  [Replace local lore database]         │
       │  [Set last_sync timestamp]             │
       │                                        │
```

### 7.2 Incremental Sync (Periodic)

```
┌─────────────┐                          ┌─────────────┐
│   Recall    │                          │   Engram    │
└──────┬──────┘                          └──────┬──────┘
       │                                        │
       │  [Collect pending lore]                │
       │                                        │
       │  POST /api/v1/lore                     │
       │  { source_id, lore[] }                 │
       │───────────────────────────────────────▶│
       │                                        │
       │                    [Dedupe & merge]    │
       │                    [Generate embeddings]
       │                    [Store]             │
       │                                        │
       │  200 OK { accepted, merged, rejected } │
       │◀───────────────────────────────────────│
       │                                        │
       │  [Mark synced locally]                 │
       │                                        │
       │  GET /api/v1/lore/delta                │
       │  ?since={last_sync}                    │
       │───────────────────────────────────────▶│
       │                                        │
       │  200 OK { lore[], deleted_ids[] }      │
       │◀───────────────────────────────────────│
       │                                        │
       │  [Merge into local lore store]         │
       │  [Update last_sync]                    │
       │                                        │
```

### 7.3 Shutdown Sync (On Devcontainer Stop)

```
┌─────────────┐                          ┌─────────────┐
│   Recall    │                          │   Engram    │
└──────┬──────┘                          └──────┬──────┘
       │                                        │
       │  [Flush all pending immediately]       │
       │                                        │
       │  POST /api/v1/lore                     │
       │  { source_id, lore[], flush: true }    │
       │───────────────────────────────────────▶│
       │                                        │
       │  200 OK                                │
       │◀───────────────────────────────────────│
       │                                        │
       │  [Graceful shutdown complete]          │
       │                                        │
```

### 7.4 Reinforcement Flow (Feedback Loop)

```
┌─────────────────────────────────────────────────────────────────┐
│                    Session Reinforcement Flow                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. WORKFLOW START                                               │
│     └── Passive injection surfaces lore                         │
│         [L1] Queue consumer idempotency (0.80)                  │
│         [L2] Message broker confirmation (0.75)                 │
│         [L3] Batch memory limits (0.65)                         │
│     └── Recall tracks these as session lore                     │
│                                                                  │
│  2. DURING WORKFLOW                                              │
│     └── Agent actively queries more lore                        │
│         [L4] Retry backoff patterns (0.70)                      │
│     └── Recall adds to session tracking                         │
│                                                                  │
│  3. TASK COMPLETION                                              │
│     └── Agent identifies which lore helped                      │
│         L1, L2 were directly useful                             │
│         L3 wasn't relevant to this task                         │
│         L4 helped somewhat                                       │
│                                                                  │
│  4. FEEDBACK CALL                                                │
│     └── Agent calls:                                             │
│         recall_feedback(                                         │
│           helpful: ["L1", "L2", "L4"],                          │
│           not_relevant: ["L3"]                                  │
│         )                                                        │
│     └── Recall resolves session refs to IDs                     │
│     └── Recall updates local lore store:                        │
│         L1: 0.80 → 0.88, validation_count: 4 → 5                │
│         L2: 0.75 → 0.83, validation_count: 2 → 3                │
│         L4: 0.70 → 0.78, validation_count: 1 → 2                │
│         L3: unchanged                                            │
│     └── Updates queued for sync                                  │
│                                                                  │
│  5. SYNC TO ENGRAM                                               │
│     └── POST /api/v1/lore/feedback                              │
│     └── Central applies same adjustments                        │
│     └── All environments see updated confidence                 │
│                                                                  │
│  6. NEXT SESSION (any devcontainer)                             │
│     └── L1 recalled with confidence 0.88                        │
│     └── Trust signal is stronger                                 │
│     └── validation_count: 5 visible to agent                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Session reference resolution:** The client maintains a map of session refs (L1, L2, ...) to lore IDs. When `feedback()` is called, references are resolved automatically. Agents can also use content snippets ("queue consumer idempotency") which are fuzzy-matched to session lore.

**Workflow integration:** The natural point for feedback is the Feedback Loop (FL) workflow step, where agents already reflect on design-vs-implementation outcomes. Adding "rate recalled lore" is a minimal addition to existing workflow.

---

## 8. Conflict Resolution

### 8.1 Semantic Deduplication

When new lore arrives at Engram, check for semantic duplicates:

```
function ingestLore(incoming: Lore): IngestResult {
  // 1. Generate embedding if not present
  if (!incoming.embedding) {
    incoming.embedding = embeddingService.embed(incoming.content)
  }

  // 2. Find semantically similar existing lore
  candidates = store.findSimilar(
    embedding: incoming.embedding,
    threshold: 0.92,  // cosine similarity
    limit: 5
  )

  // 3. Check for near-duplicates
  for (candidate in candidates) {
    if (isSemanticallyEquivalent(incoming, candidate)) {
      return mergeWithExisting(candidate, incoming)
    }
  }

  // 4. No duplicate found, insert as new
  return store.insert(incoming)
}

function isSemanticallyEquivalent(a: Lore, b: Lore): boolean {
  // Same category required
  if (a.category != b.category) return false

  // High embedding similarity
  similarity = cosineSimilarity(a.embedding, b.embedding)
  return similarity >= 0.92
}
```

### 8.2 Merge Strategy

```
function mergeWithExisting(existing: Lore, incoming: Lore): IngestResult {
  // Boost confidence (reinforcement)
  existing.confidence = min(existing.confidence + 0.1, 1.0)

  // Append context if different
  if (incoming.context && !existing.context.contains(incoming.context)) {
    existing.context = existing.context + "\n---\n" + incoming.context
  }

  // Track all contributing sources
  existing.sources = unique(existing.sources + [incoming.source_id])

  // Update timestamp
  existing.updated_at = now()

  store.update(existing)
  return { status: MERGED, lore: existing }
}
```

---

## 9. Recall Algorithm

### 9.1 Local Recall (Primary Path)

```
function recall(params: RecallParams): Lore[] {
  // 1. Generate query embedding
  queryEmbedding = embeddingService.embed(params.query)

  // 2. Fetch all candidates matching filters
  candidates = store.query(
    minConfidence: params.minConfidence,
    categories: params.categories
  )

  // 3. Compute distances
  scored = candidates.map(lore => {
    distance: cosineDistance(queryEmbedding, lore.embedding),
    lore: lore
  })

  // 4. Sort by distance ascending (closer = more similar)
  scored.sortBy(s => s.distance)

  // 5. Return top-k
  return scored.take(params.k).map(s => s.lore)
}
```

### 9.2 Performance Optimization Tiers

| Lore Count | Strategy | Expected Latency |
|----------------|----------|------------------|
| < 1,000 | Brute-force scan | < 5ms |
| 1,000 - 5,000 | Brute-force with early termination | 5-20ms |
| 5,000 - 20,000 | Approximate NN index (HNSW) | < 10ms |
| > 20,000 | Dedicated vector database | < 5ms |

Initial implementation should use brute-force. Add indexing when metrics indicate need.

---

## 10. Embedding Strategy

### 10.1 Embedding Generation Location

| Location | Pros | Cons |
|----------|------|------|
| Client-side | Lower central load, works offline | Model version drift risk, API key exposure |
| Server-side | Guaranteed consistency, single API key | Higher central load, no offline embedding |

**Decision: Server-side embedding at central.**

Rationale:
- Guarantees all embeddings use identical model
- Simplifies client implementation
- Centralizes API key management
- Offline recall works (embeddings already stored locally)
- Only new lore needs network (not recalls)

### 10.2 Model Compatibility

```
// On client bootstrap
function validateCompatibility(central: HealthResponse): Result<void, Error> {
  localModel = metadata.get("embedding_model")

  if (localModel && localModel != central.embedding_model) {
    return Error(
      "Embedding model mismatch. " +
      "Local: ${localModel}, Central: ${central.embedding_model}. " +
      "Re-bootstrap required."
    )
  }

  return Ok()
}
```

### 10.3 Model Migration

When embedding model changes:

1. Engram generates new embeddings for all lore (background job)
2. Central increments schema version
3. Clients detect version mismatch on next sync
4. Clients perform full re-bootstrap (snapshot download)

---

## 11. Deployment Architecture

### 11.1 Central Service

```
┌────────────────────────────────────────────────────────────┐
│                    Container Platform                       │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                   Engram Service                       │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────────┐  │  │
│  │  │    API     │  │  Embedding │  │   Background   │  │  │
│  │  │   Server   │  │   Client   │  │    Workers     │  │  │
│  │  └─────┬──────┘  └─────┬──────┘  └───────┬────────┘  │  │
│  │        │               │                  │           │  │
│  │        └───────────────┼──────────────────┘           │  │
│  │                        │                              │  │
│  │                 ┌──────▼──────┐                       │  │
│  │                 │   SQLite    │                       │  │
│  │                 │  (Primary)  │                       │  │
│  │                 └──────┬──────┘                       │  │
│  │                        │                              │  │
│  └────────────────────────┼──────────────────────────────┘  │
│                           │                                  │
│                    ┌──────▼──────┐                          │
│                    │  Persistent │                          │
│                    │   Volume    │                          │
│                    └─────────────┘                          │
└────────────────────────────────────────────────────────────┘
                            │
                     ┌──────▼──────┐
                     │   Object    │
                     │   Storage   │
                     │ (Snapshots) │
                     └─────────────┘
```

### 11.2 Client Integration

```
┌─────────────────────────────────────────────────────────┐
│                    Devcontainer                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │                 Forge Application                  │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌───────────┐  │  │
│  │  │   Agent     │  │   Agent     │  │  Other    │  │  │
│  │  │   (Spark)   │  │  (Clario)   │  │  Agents   │  │  │
│  │  └──────┬──────┘  └──────┬──────┘  └─────┬─────┘  │  │
│  │         │                │               │         │  │
│  │         └────────────────┼───────────────┘         │  │
│  │                          │                         │  │
│  │                   ┌──────▼──────┐                  │  │
│  │                   │   Recall    │                  │  │
│  │                   │   Client    │                  │  │
│  │                   └──────┬──────┘                  │  │
│  │                          │                         │  │
│  │                   ┌──────▼──────┐                  │  │
│  │                   │   SQLite    │                  │  │
│  │                   │  (Replica)  │                  │  │
│  │                   └─────────────┘                  │  │
│  └───────────────────────────────────────────────────┘  │
│                                                          │
│  Lifecycle Hooks:                                        │
│    post-start → recall sync --pull                      │
│    pre-stop   → recall sync --push --flush              │
└─────────────────────────────────────────────────────────┘
```

---

## 12. Security Considerations

### 12.1 Authentication & Authorization

| Layer | Mechanism |
|-------|-----------|
| Client → Central | API key (per organization/team) |
| Central → Embedding API | Service API key (server-side only) |

### 12.2 Data Protection

| Concern | Mitigation |
|---------|------------|
| Lore may contain sensitive context | Organization-scoped isolation; no cross-org queries |
| API key exposure | Server-side embedding; keys never reach devcontainers |
| Transit security | HTTPS required for all central communication |
| Storage security | Encryption at rest for central database and snapshots |

### 12.3 Input Validation

| Field | Validation |
|-------|------------|
| content | Max 4000 chars, UTF-8, no null bytes |
| context | Max 1000 chars, UTF-8 |
| category | Must be valid enum value |
| confidence | Range [0.0, 1.0] |
| source_id | Valid ULID format |

---

## 13. Observability

### 13.1 Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `engram.recorded.total` | Counter | Total lore recorded |
| `engram.recalled.total` | Counter | Total recall operations |
| `engram.recall.latency_ms` | Histogram | Recall operation latency |
| `engram.sync.push.total` | Counter | Push sync operations |
| `engram.sync.pull.total` | Counter | Pull sync operations |
| `engram.sync.errors.total` | Counter | Sync failures |
| `engram.store.size` | Gauge | Number of lore entries in store |
| `engram.queue.pending` | Gauge | Pending sync queue size |

### 13.2 Logging

| Event | Level | Fields |
|-------|-------|--------|
| Lore recorded | INFO | id, category, source_id |
| Recall executed | DEBUG | query_length, k, results_count, latency_ms |
| Sync started | INFO | direction, source_id |
| Sync completed | INFO | direction, pushed, pulled, merged, duration_ms |
| Sync failed | ERROR | direction, error, attempt |
| Deduplication triggered | DEBUG | existing_id, incoming_id, similarity |

---

## 14. Capacity Planning

### 14.1 Storage Estimates

| Component | Per Lore Entry | 1,000 Lore Entries | 10,000 Lore Entries |
|-----------|--------------|-----------------|------------------|
| Content (avg 500 chars) | 500 B | 500 KB | 5 MB |
| Context (avg 200 chars) | 200 B | 200 KB | 2 MB |
| Embedding (1536 floats) | 6 KB | 6 MB | 60 MB |
| Metadata + indexes | 200 B | 200 KB | 2 MB |
| **Total** | ~7 KB | ~7 MB | ~70 MB |

### 14.2 Throughput Estimates

| Operation | Expected Rate | Target Latency |
|-----------|---------------|----------------|
| Record | 10-50 per day per devcontainer | < 100ms (local) |
| Recall | 50-200 per day per devcontainer | < 20ms (local) |
| Sync push | Every 5 min or 10 pending | < 2s |
| Sync pull (delta) | Every 5 min | < 1s |
| Sync pull (snapshot) | Once per boot | < 10s |

---

## 15. Proposed Technology Stack

### 15.1 Repository Architecture

Two separate repositories for clean separation:

| Repository | Purpose | URL |
|------------|---------|-----|
| **Engram** | Central lore service | `github.com/hyperengineering/engram` |
| **Recall** | General-purpose client library | `github.com/hyperengineering/recall` |

**Rationale:**
- Recall is a general-purpose library usable by any AI agent framework
- Minimal dependencies for Recall (no Chi, no OpenAI client)
- Independent versioning and release cycles
- Clean API contract enforcement

### 15.2 Recall Client Library (General-Purpose)

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | **Go** | Wide adoption, easy cross-compilation |
| Local database | **modernc.org/sqlite** | Pure Go, no CGO, portable |
| HTTP client | **net/http** | Standard library, no external deps |
| Embedding format | **binary (float32 array)** | Compact, fast read/write |
| ID generation | **github.com/oklog/ulid/v2** | Sortable, unique identifiers |

**Package structure:**
```
github.com/hyperengineering/recall/
├── client.go         // Core client interface
├── store.go          // Local lore SQLite operations
├── sync.go           // Engram sync protocol
├── session.go        // Session tracking for feedback
├── similarity.go     // Cosine distance calculations
├── types.go          // Lore, QueryParams, FeedbackParams
├── config.go         // Configuration management
├── cmd/
│   └── recall/       // CLI tool
│       └── main.go
├── mcp/              // Optional MCP tool adapters
│   └── tools.go
└── examples/
    ├── basic/
    └── forge/
```

### 15.3 Engram Central Service

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | **Go** | Consistency with ecosystem, team familiarity |
| Framework | **Chi** | Lightweight, fast, idiomatic |
| Database | **SQLite** | Simple, sufficient scale, single-file backup |
| Hosting | **Fly.io** | Persistent volumes, simple deploy, low cost |
| Embedding API | **OpenAI text-embedding-3-small** | Good quality/cost ratio, 1536 dimensions |

**Repository structure:**
```
github.com/hyperengineering/engram/
├── cmd/
│   └── engram/
│       ├── main.go
│       └── root.go
├── internal/
│   ├── api/
│   │   ├── handlers.go       // HTTP handlers
│   │   ├── middleware.go     // Auth, logging
│   │   └── routes.go
│   ├── store/
│   │   ├── sqlite.go         // Lore database operations
│   │   ├── dedup.go          // Semantic deduplication
│   │   └── snapshot.go       // Snapshot generation
│   ├── embedding/
│   │   └── openai.go         // Embedding service client
│   └── config/
│       └── config.go
├── docs/
│   ├── api.md
│   └── deployment.md
├── .devcontainer/
├── .github/workflows/
├── fly.toml
└── Makefile
```

### 15.4 MCP Tool Integration

Recall provides optional MCP adapters in the `mcp/` subpackage:

```go
// github.com/hyperengineering/recall/mcp/tools.go

package mcp

import "github.com/hyperengineering/recall"

// RegisterTools registers Recall tools with an MCP registry.
// This is optional — Recall can be used without MCP.
func RegisterTools(registry Registry, client *recall.Client) {
    registry.Register(Tool{
        Name:        "recall_query",
        Description: "Retrieve relevant lore based on semantic similarity",
        Parameters: Schema{
            "query":          {Type: "string", Required: true},
            "k":              {Type: "integer", Default: 5},
            "min_confidence": {Type: "number", Default: 0.5},
            "categories":     {Type: "array", Items: "string"},
        },
        Handler: makeQueryHandler(client),
    })

    registry.Register(Tool{
        Name:        "recall_record",
        Description: "Capture lore from current experience",
        Parameters: Schema{
            "content":    {Type: "string", Required: true},
            "category":   {Type: "string", Required: true},
            "context":    {Type: "string"},
            "confidence": {Type: "number", Default: 0.7},
        },
        Handler: makeRecordHandler(client),
    })

    registry.Register(Tool{
        Name:        "recall_feedback",
        Description: "Provide feedback on lore recalled this session",
        Parameters: Schema{
            "helpful":      {Type: "array", Items: "string"},
            "not_relevant": {Type: "array", Items: "string"},
            "incorrect":    {Type: "array", Items: "string"},
        },
        Handler: makeFeedbackHandler(client),
    })
}
```

**Usage in Forge:**
```go
import (
    "github.com/hyperengineering/recall"
    recallmcp "github.com/hyperengineering/recall/mcp"
)

client, _ := recall.New(cfg)
recallmcp.RegisterTools(mcpRegistry, client)
```

### 15.5 Passive Injection Mechanism

Lore is injected into agent context at workflow start:

```go
// Prepend to agent system prompt
func (a *Agent) BuildSystemPrompt(basePrompt string, lore []Lore) string {
    if len(lore) == 0 {
        return basePrompt
    }

    injection := "## Relevant Lore\n\n"
    for i, l := range lore {
        injection += fmt.Sprintf("[L%d] %s (confidence: %.2f, validated: %d times)\n%s\n\n",
            i+1, l.Category, l.Confidence, l.ValidationCount, l.Content)
    }

    return injection + "\n---\n\n" + basePrompt
}
```

### 15.6 Key Implementation Patterns

**Embedding storage (binary float32):**
```go
func packEmbedding(v []float32) []byte {
    buf := make([]byte, len(v)*4)
    for i, f := range v {
        binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
    }
    return buf
}

func unpackEmbedding(b []byte) []float32 {
    v := make([]float32, len(b)/4)
    for i := range v {
        v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
    }
    return v
}
```

**Cosine similarity (brute-force):**
```go
func cosineSimilarity(a, b []float32) float32 {
    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
```

**Session tracking:**
```go
type Session struct {
    mu      sync.Mutex
    lore    map[string]string  // L1 -> lore ID
    counter int
}

func (s *Session) Track(id string) string {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.counter++
    ref := fmt.Sprintf("L%d", s.counter)
    s.lore[ref] = id
    return ref
}

func (s *Session) Resolve(ref string) (string, bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    id, ok := s.lore[ref]
    return id, ok
}
```

### 15.7 Configuration

```yaml
# .forge/config.yaml (in devcontainer)
recall:
  enabled: true
  lore_path: "${FORGE_DATA_DIR}/lore.db"
  engram:
    url: "https://engram.forge.dev"
    api_key: "${ENGRAM_API_KEY}"
  sync:
    interval: "5m"
    on_boot: true
    on_shutdown: true
  passive_injection:
    enabled: true
    k: 5
    min_confidence: 0.6
```

### 15.8 Dependencies

**Recall Library (`github.com/hyperengineering/recall`):**
```go
// go.mod — minimal dependencies
module github.com/hyperengineering/recall

go 1.23

require (
    modernc.org/sqlite v1.28.0      // Pure Go SQLite
    github.com/oklog/ulid/v2 v2.1.0 // ULID generation
)
```

**Engram Service (`github.com/hyperengineering/engram`):**
```go
// go.mod — service dependencies
module github.com/hyperengineering/engram

go 1.23

require (
    github.com/go-chi/chi/v5 v5.0.11        // HTTP router
    modernc.org/sqlite v1.28.0               // Database
    github.com/sashabaranov/go-openai v1.17.9 // Embeddings
    github.com/oklog/ulid/v2 v2.1.0          // ID generation
)
```

**Forge Integration:**
```go
// In Forge's go.mod
require (
    github.com/hyperengineering/recall v0.1.0
)
```

### 15.9 Cost Estimate

| Component | Cost |
|-----------|------|
| Fly.io (shared-cpu-1x, 256MB) | ~$2/month |
| Persistent volume (1GB) | ~$0.15/month |
| OpenAI embeddings (text-embedding-3-small) | ~$0.02 per 1M tokens |

**Embedding cost projection:**
- Average lore entry: 500 chars ≈ 125 tokens
- 1,000 lore entries = 125,000 tokens ≈ $0.0025
- Negligible at expected scale

### 15.10 Alternative Considerations

| Decision Point | Alternative | When to Consider |
|----------------|-------------|------------------|
| Pure Go SQLite | CGO sqlite3 | If write performance becomes critical |
| Fly.io | Self-hosted | If data residency requirements exist |
| OpenAI embeddings | Voyage AI | If higher quality embeddings needed |
| Brute-force search | go-faiss | If >10k lore entries and latency matters |
| Chi framework | Standard net/http | If minimal dependencies preferred |

---

## 16. Implementation Phases

### Phase 1: Recall Library Foundation
**Repository:** `github.com/hyperengineering/recall`

- [ ] Core client interface and types
- [ ] Local SQLite store (record, query, feedback)
- [ ] Session tracking for recalled lore
- [ ] Brute-force similarity search
- [ ] Configuration management
- [ ] CLI tool (`recall record`, `recall query`, `recall sync`)
- [ ] Unit tests and examples

### Phase 2: Engram Central Service
**Repository:** `github.com/hyperengineering/engram`

- [ ] API server with Chi router
- [ ] Authentication middleware (API key)
- [ ] Server-side embedding integration (OpenAI)
- [ ] Lore storage with semantic deduplication
- [ ] Snapshot generation endpoint
- [ ] Delta sync endpoint
- [ ] Feedback/confidence update endpoint
- [ ] Fly.io deployment configuration

### Phase 3: Sync Protocol
**Both repositories**

- [ ] Recall: Sync orchestration (pull/push)
- [ ] Recall: Bootstrap sync (snapshot download)
- [ ] Recall: Incremental sync (delta)
- [ ] Engram: Conflict resolution and deduplication
- [ ] Integration tests between Recall and Engram

### Phase 4: Forge Integration
**Repository:** `github.com/forge/forge`

- [ ] Import Recall library
- [ ] MCP tool registration
- [ ] Passive injection at workflow start
- [ ] Devcontainer lifecycle hooks (post-start, pre-stop)
- [ ] Configuration in Forge config

### Phase 5: Ecosystem & Optimization
**Both repositories**

- [ ] Recall latency monitoring
- [ ] HNSW index (if >10k lore entries)
- [ ] Confidence decay background job (Engram)
- [ ] Snapshot caching
- [ ] Documentation for third-party integrations
- [ ] Example integrations (LangChain, etc.)

---

## 17. Open Questions

| Question | Options | Decision Needed By |
|----------|---------|-------------------|
| Embedding model selection | text-embedding-3-small vs ada-002 vs local model | Phase 2 start |
| Multi-tenancy model | Org isolation vs shared with access control | Phase 2 start |
| Snapshot format | Raw SQLite vs custom binary | Phase 2 |
| MCP adapter interface | Generic interface vs framework-specific | Phase 4 |

---

## 18. Appendix: Test Seed Scenarios

### Recording Lore

```
GIVEN an agent completes a technical design
WHEN the design includes a pattern decision
THEN lore is recorded with category PATTERN_OUTCOME

GIVEN an agent discovers a library behaves unexpectedly
WHEN the gotcha is recorded
THEN lore is recorded with category DEPENDENCY_BEHAVIOR

GIVEN an agent identifies a testing approach that worked well
WHEN the insight is recorded
THEN lore is recorded with category TESTING_STRATEGY

GIVEN an agent discovers performance characteristics under load
WHEN the insight is recorded
THEN lore is recorded with category PERFORMANCE_INSIGHT

GIVEN an agent records lore
WHEN the content exceeds 4000 characters
THEN the record operation fails with validation error

GIVEN an agent records lore with invalid category
WHEN the record operation is attempted
THEN the operation fails with validation error

GIVEN lore is recorded locally
WHEN sync to Engram succeeds
THEN the lore synced_at timestamp is set
```

### Querying Lore

```
GIVEN lore exists about "queue consumer patterns"
WHEN querying with "implementing message consumers"
THEN semantically similar lore is returned

GIVEN lore with varying confidence levels
WHEN querying with minConfidence 0.7
THEN only lore with confidence >= 0.7 is returned

GIVEN lore exists across multiple categories
WHEN querying with categories filter [DEPENDENCY_BEHAVIOR, PERFORMANCE_INSIGHT]
THEN only lore matching those categories is returned

GIVEN Spark agent is implementing code
WHEN querying with categories [IMPLEMENTATION_FRICTION, TESTING_STRATEGY, DEPENDENCY_BEHAVIOR]
THEN lore relevant to implementation is prioritized

GIVEN no lore exists
WHEN query is executed
THEN an empty list is returned (not an error)

GIVEN lore is recalled during a session
THEN each lore entry is assigned a session reference (L1, L2, ...)
AND session references are tracked for feedback
```

### Feedback and Reinforcement

```
GIVEN lore L1 and L2 were recalled during the session
WHEN agent calls recall_feedback(helpful: ["L1", "L2"])
THEN L1 confidence increases by 0.08
AND L2 confidence increases by 0.08
AND both validation_count fields increment by 1
AND both last_validated timestamps are updated

GIVEN lore L3 was recalled but wasn't relevant
WHEN agent calls recall_feedback(not_relevant: ["L3"])
THEN L3 confidence remains unchanged
AND L3 validation_count remains unchanged

GIVEN lore L4 was recalled and proved incorrect
WHEN agent calls recall_feedback(incorrect: ["L4"])
THEN L4 confidence decreases by 0.15
AND L4 validation_count remains unchanged

GIVEN agent references lore by content snippet
WHEN agent calls recall_feedback(helpful: ["queue consumer idempotency"])
THEN system fuzzy-matches to session lore with that content
AND confidence is updated for the matched lore

GIVEN feedback is submitted locally
WHEN sync to Engram occurs
THEN confidence updates are pushed to Engram
AND all devcontainers receive updated confidence on next sync

GIVEN lore has confidence 0.95
WHEN agent marks it as helpful
THEN confidence is capped at 1.0 (not 1.03)

GIVEN lore has confidence 0.10
WHEN agent marks it as incorrect
THEN confidence is floored at 0.0 (not -0.05)
```

### Sync Protocol

```
GIVEN a devcontainer boots for the first time
WHEN sync is initialized
THEN a full snapshot is downloaded from Engram

GIVEN pending lore exists locally
WHEN sync push is triggered
THEN lore is sent to Engram and marked synced

GIVEN Engram has new lore since last sync
WHEN delta sync is triggered
THEN new lore is merged into local store
```

### Conflict Resolution

```
GIVEN lore exists at Engram about "repository pattern"
WHEN semantically equivalent lore arrives from another source
THEN the existing lore confidence is boosted
AND the new source is added to sources list
AND no duplicate is created
```

---

## 19. Repository Setup Instructions

This section provides detailed instructions for setting up both repositories in the two-repo architecture.

---

### Part A: Recall Library

**Repository:** `github.com/hyperengineering/recall`

### 19.1 Recall Repository Structure

```
recall/
├── client.go                    # Core client interface
├── store.go                     # Local SQLite operations
├── sync.go                      # Engram sync protocol
├── session.go                   # Session tracking for feedback
├── similarity.go                # Cosine distance calculations
├── types.go                     # Lore, QueryParams, FeedbackParams
├── config.go                    # Configuration management
├── errors.go                    # Error types
├── cmd/
│   └── recall/                  # CLI tool
│       ├── main.go
│       ├── root.go
│       ├── record.go
│       ├── query.go
│       ├── sync.go
│       └── feedback.go
├── mcp/                         # Optional MCP adapters
│   └── tools.go
├── examples/
│   ├── basic/
│   │   └── main.go
│   └── forge/
│       └── main.go
├── docs/
│   ├── getting-started.md
│   ├── cli.md
│   ├── api.md
│   └── mcp-integration.md
├── .devcontainer/
│   ├── devcontainer.json
│   ├── docker-compose.yml
│   └── post-create.sh
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── .gitignore
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 19.2 Recall Go Module

```bash
# Create repository
mkdir recall && cd recall

# Initialize Go module
go mod init github.com/hyperengineering/recall

# Add minimal dependencies
go get modernc.org/sqlite
go get github.com/oklog/ulid/v2
go get github.com/spf13/cobra  # For CLI

# Download and tidy
go mod download
go mod tidy
```

**go.mod:**

```go
module github.com/hyperengineering/recall

go 1.23

require (
    github.com/oklog/ulid/v2 v2.1.0
    github.com/spf13/cobra v1.8.0
    modernc.org/sqlite v1.28.0
)
```

**Note:** Recall has minimal dependencies — no HTTP framework, no OpenAI client. This keeps it lightweight for consumers.

### 19.3 Recall Devcontainer

**.devcontainer/devcontainer.json:**

```json
{
  "name": "Recall",
  "dockerComposeFile": "docker-compose.yml",
  "service": "workspace",
  "workspaceFolder": "/workspaces/recall",
  "features": {
    "ghcr.io/devcontainers/features/go:1": {
      "version": "1.23"
    }
  },
  "customizations": {
    "vscode": {
      "extensions": ["golang.go"],
      "settings": {
        "go.useLanguageServer": true,
        "go.lintTool": "golangci-lint"
      }
    }
  },
  "postCreateCommand": "bash .devcontainer/post-create.sh"
}
```

**.devcontainer/docker-compose.yml:**

```yaml
version: '3.8'

services:
  workspace:
    image: mcr.microsoft.com/devcontainers/go:1-1.23-bookworm
    volumes:
      - ..:/workspaces/recall:cached
      - go-mod-cache:/go/pkg/mod
    command: sleep infinity
    environment:
      - RECALL_DB_PATH=/workspaces/recall/data/lore.db
      - ENGRAM_URL=${ENGRAM_URL:-http://localhost:8080}
      - ENGRAM_API_KEY=${ENGRAM_API_KEY}

volumes:
  go-mod-cache:
```

**.devcontainer/post-create.sh:**

```bash
#!/bin/bash
set -e

go mod download
mkdir -p /workspaces/recall/data
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

echo "=== Recall dev environment ready ==="
```

### 19.4 Recall Makefile

```makefile
BIN_DIR ?= dist
CLI_BIN := $(BIN_DIR)/recall

.PHONY: build clean test lint install

# Build CLI tool
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(CLI_BIN) ./cmd/recall

# Install CLI to GOPATH
install:
	go install ./cmd/recall

# Run unit tests
test:
	go test -v ./...

# Run tests with coverage
test-cover:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linter
lint:
	golangci-lint run ./...

# Clean
clean:
	rm -rf $(BIN_DIR) coverage.out coverage.html data/*.db

# Format and vet
fmt:
	go fmt ./...

vet:
	go vet ./...

# CI checks
ci: fmt vet lint test build
```

### 19.5 Recall CI Workflow

**.github/workflows/ci.yml:**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - run: go mod download
      - run: go vet ./...
      - run: go test -v -race ./...
      - run: go build -o bin/recall ./cmd/recall

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true
      - uses: golangci/golangci-lint-action@v4
```

**.github/workflows/release.yml:**

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Build binaries
        run: |
          GOOS=linux GOARCH=amd64 go build -o recall-linux-amd64 ./cmd/recall
          GOOS=darwin GOARCH=amd64 go build -o recall-darwin-amd64 ./cmd/recall
          GOOS=darwin GOARCH=arm64 go build -o recall-darwin-arm64 ./cmd/recall

      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          files: recall-*
```

### 19.6 Recall Core Types

**types.go:**

```go
package recall

import "time"

// Lore represents a single piece of experiential knowledge.
type Lore struct {
    ID              string    `json:"id"`
    Content         string    `json:"content"`
    Category        Category  `json:"category"`
    Context         string    `json:"context,omitempty"`
    Confidence      float64   `json:"confidence"`
    ValidationCount int       `json:"validation_count"`
    SourceID        string    `json:"source_id"`
    CreatedAt       time.Time `json:"created_at"`
    SyncedAt        *time.Time `json:"synced_at,omitempty"`
}

// Category classifies the type of lore.
type Category string

const (
    CategoryArchitecturalDecision Category = "ARCHITECTURAL_DECISION"
    CategoryPatternOutcome        Category = "PATTERN_OUTCOME"
    CategoryInterfaceLesson       Category = "INTERFACE_LESSON"
    CategoryEdgeCaseDiscovery     Category = "EDGE_CASE_DISCOVERY"
    CategoryImplementationFriction Category = "IMPLEMENTATION_FRICTION"
    CategoryTestingStrategy       Category = "TESTING_STRATEGY"
    CategoryDependencyBehavior    Category = "DEPENDENCY_BEHAVIOR"
    CategoryPerformanceInsight    Category = "PERFORMANCE_INSIGHT"
)

// QueryParams configures a lore query.
type QueryParams struct {
    Query         string     `json:"query"`
    K             int        `json:"k,omitempty"`
    MinConfidence float64    `json:"min_confidence,omitempty"`
    Categories    []Category `json:"categories,omitempty"`
}

// QueryResult contains query results with session tracking.
type QueryResult struct {
    Lore       []Lore            `json:"lore"`
    SessionRef map[string]string `json:"session_ref"` // L1 -> lore ID
}

// FeedbackParams provides feedback on recalled lore.
type FeedbackParams struct {
    Helpful     []string `json:"helpful,omitempty"`     // Session refs or content snippets
    NotRelevant []string `json:"not_relevant,omitempty"`
    Incorrect   []string `json:"incorrect,omitempty"`
}
```

### 19.7 Recall Client Interface

**client.go:**

```go
package recall

import (
    "context"
)

// Client is the main interface for interacting with lore.
type Client struct {
    store   *Store
    syncer  *Syncer
    session *Session
    config  Config
}

// Config configures the Recall client.
type Config struct {
    // LocalPath is the path to the local SQLite database.
    LocalPath string

    // EngramURL is the URL of the Engram central service.
    // If empty, operates in offline-only mode.
    EngramURL string

    // APIKey authenticates with Engram.
    APIKey string

    // SourceID identifies this client instance.
    // Defaults to hostname if not set.
    SourceID string
}

// New creates a new Recall client.
func New(cfg Config) (*Client, error) {
    store, err := NewStore(cfg.LocalPath)
    if err != nil {
        return nil, err
    }

    c := &Client{
        store:   store,
        session: NewSession(),
        config:  cfg,
    }

    if cfg.EngramURL != "" {
        c.syncer = NewSyncer(store, cfg.EngramURL, cfg.APIKey)
    }

    return c, nil
}

// Record captures new lore.
func (c *Client) Record(ctx context.Context, lore Lore) (*Lore, error) {
    return c.store.Record(lore)
}

// Query retrieves relevant lore based on semantic similarity.
func (c *Client) Query(ctx context.Context, params QueryParams) (*QueryResult, error) {
    lore, err := c.store.Query(params)
    if err != nil {
        return nil, err
    }

    // Track in session for feedback
    refs := make(map[string]string)
    for _, l := range lore {
        ref := c.session.Track(l.ID)
        refs[ref] = l.ID
    }

    return &QueryResult{Lore: lore, SessionRef: refs}, nil
}

// Feedback provides feedback on recalled lore.
func (c *Client) Feedback(ctx context.Context, params FeedbackParams) error {
    return c.store.ApplyFeedback(c.session, params)
}

// Sync synchronizes with Engram (if configured).
func (c *Client) Sync(ctx context.Context) error {
    if c.syncer == nil {
        return nil // Offline mode
    }
    return c.syncer.Sync(ctx)
}

// Close closes the client and flushes pending changes.
func (c *Client) Close() error {
    return c.store.Close()
}
```

### 19.8 Recall CLI

**cmd/recall/main.go:**

```go
package main

import (
    "os"

    "github.com/spf13/cobra"
)

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

var rootCmd = &cobra.Command{
    Use:   "recall",
    Short: "Recall - Lore management CLI",
}

func init() {
    rootCmd.AddCommand(recordCmd)
    rootCmd.AddCommand(queryCmd)
    rootCmd.AddCommand(syncCmd)
}
```

**cmd/recall/record.go:**

```go
package main

import (
    "context"
    "fmt"

    "github.com/hyperengineering/recall"
    "github.com/spf13/cobra"
)

var recordCmd = &cobra.Command{
    Use:   "record [content]",
    Short: "Record new lore",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        category, _ := cmd.Flags().GetString("category")
        ctx, _ := cmd.Flags().GetString("context")

        client, err := recall.New(loadConfig())
        if err != nil {
            return err
        }
        defer client.Close()

        lore, err := client.Record(context.Background(), recall.Lore{
            Content:  args[0],
            Category: recall.Category(category),
            Context:  ctx,
        })
        if err != nil {
            return err
        }

        fmt.Printf("Recorded: %s\n", lore.ID)
        return nil
    },
}

func init() {
    recordCmd.Flags().StringP("category", "c", "PATTERN_OUTCOME", "Lore category")
    recordCmd.Flags().String("context", "", "Additional context")
}
```

---

### Part B: Engram Central Service

**Repository:** `github.com/hyperengineering/engram`

### 19.9 Engram Repository Structure

```
engram/
├── cmd/
│   └── engram/
│       ├── main.go
│       └── root.go
├── internal/
│   ├── api/
│   │   ├── handlers.go
│   │   ├── middleware.go
│   │   └── routes.go
│   ├── config/
│   │   └── config.go
│   ├── embedding/
│   │   └── openai.go
│   ├── store/
│   │   ├── sqlite.go
│   │   ├── dedup.go
│   │   └── snapshot.go
│   └── types/
│       └── types.go
├── docs/
│   ├── api.md
│   └── deployment.md
├── .devcontainer/
│   ├── devcontainer.json
│   ├── docker-compose.yml
│   └── post-create.sh
├── .github/
│   └── workflows/
│       └── ci.yml
├── .env.example
├── .gitignore
├── fly.toml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 19.10 Engram Go Module

```bash
mkdir engram && cd engram

go mod init github.com/hyperengineering/engram

go get github.com/go-chi/chi/v5
go get github.com/sashabaranov/go-openai
go get modernc.org/sqlite
go get github.com/oklog/ulid/v2
go get github.com/spf13/cobra

go mod tidy
```

**go.mod:**

```go
module github.com/hyperengineering/engram

go 1.23

require (
    github.com/go-chi/chi/v5 v5.0.11
    github.com/oklog/ulid/v2 v2.1.0
    github.com/sashabaranov/go-openai v1.17.9
    github.com/spf13/cobra v1.8.0
    modernc.org/sqlite v1.28.0
)
```

### 19.11 Engram Configuration

**internal/config/config.go:**

```go
package config

import (
    "errors"
    "os"
)

type Config struct {
    Address             string
    DBPath              string
    OpenAIAPIKey        string
    OpenAIModel         string
    EmbeddingDimensions int
    APIKey              string
    LogLevel            string
}

func Load() (*Config, error) {
    cfg := &Config{
        Address:             getEnv("ENGRAM_ADDRESS", "0.0.0.0:8080"),
        DBPath:              getEnv("ENGRAM_DB_PATH", "./data/lore.db"),
        OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
        OpenAIModel:         getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
        EmbeddingDimensions: 1536,
        APIKey:              os.Getenv("ENGRAM_API_KEY"),
        LogLevel:            getEnv("ENGRAM_LOG_LEVEL", "info"),
    }

    if err := cfg.Validate(); err != nil {
        return nil, err
    }
    return cfg, nil
}

func (c *Config) Validate() error {
    if c.OpenAIAPIKey == "" {
        return errors.New("OPENAI_API_KEY is required")
    }
    if c.APIKey == "" {
        return errors.New("ENGRAM_API_KEY is required")
    }
    return nil
}

func getEnv(key, defaultValue string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return defaultValue
}
```

### 19.12 Engram Main Entry Point

**cmd/engram/main.go:**

```go
package main

import "os"

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

**cmd/engram/root.go:**

```go
package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/hyperengineering/engram/internal/api"
    "github.com/hyperengineering/engram/internal/config"
    "github.com/hyperengineering/engram/internal/embedding"
    "github.com/hyperengineering/engram/internal/store"
    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
    Use:   "engram",
    Short: "Engram - Central Lore Service",
    RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

    db, err := store.New(cfg.DBPath)
    if err != nil {
        return err
    }
    defer db.Close()

    embedder := embedding.NewOpenAI(cfg.OpenAIAPIKey, cfg.OpenAIModel)
    handler := api.NewHandler(db, embedder, cfg.APIKey)
    router := api.NewRouter(handler)

    server := &http.Server{Addr: cfg.Address, Handler: router}

    done := make(chan os.Signal, 1)
    signal.Notify(done, os.Interrupt, syscall.SIGTERM)

    go func() {
        slog.Info("Starting Engram", "address", cfg.Address)
        if err := server.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("Server error", "error", err)
        }
    }()

    <-done
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    return server.Shutdown(ctx)
}
```

### 19.13 Engram Deployment (Fly.io)

**fly.toml:**

```toml
app = "engram"
primary_region = "ord"

[build]
  builder = "paketobuildpacks/builder:base"

[env]
  ENGRAM_ADDRESS = "0.0.0.0:8080"
  ENGRAM_DB_PATH = "/data/lore.db"
  OPENAI_EMBEDDING_MODEL = "text-embedding-3-small"
  ENGRAM_LOG_LEVEL = "info"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = true
  auto_start_machines = true
  min_machines_running = 1

[mounts]
  source = "engram_data"
  destination = "/data"

[[vm]]
  cpu_kind = "shared"
  cpus = 1
  memory_mb = 256
```

**Deploy commands:**

```bash
fly apps create engram
fly volumes create engram_data --size 1 --region ord
fly secrets set OPENAI_API_KEY=sk-...
fly secrets set ENGRAM_API_KEY=...
fly deploy
```

---

### Part C: Forge Integration

### 19.14 Integrating Recall into Forge

**Add dependency to Forge's go.mod:**

```go
require (
    github.com/hyperengineering/recall v0.1.0
)
```

**Initialize in Forge:**

```go
import (
    "github.com/hyperengineering/recall"
    recallmcp "github.com/hyperengineering/recall/mcp"
)

// Initialize Recall client
client, err := recall.New(recall.Config{
    LocalPath: os.Getenv("FORGE_DATA_DIR") + "/lore.db",
    EngramURL: os.Getenv("ENGRAM_URL"),
    APIKey:    os.Getenv("ENGRAM_API_KEY"),
    SourceID:  hostname(),
})
if err != nil {
    return err
}
defer client.Close()

// Register MCP tools (optional)
recallmcp.RegisterTools(mcpRegistry, client)

// Sync on startup
if err := client.Sync(ctx); err != nil {
    slog.Warn("Initial sync failed", "error", err)
}
```

**Forge devcontainer environment:**

```yaml
# .devcontainer/docker-compose.yml
environment:
  - ENGRAM_URL=https://engram.forge.dev
  - ENGRAM_API_KEY=${ENGRAM_API_KEY}
  - FORGE_DATA_DIR=/workspaces/forge/data
```

**Lifecycle hooks:**

```bash
# .devcontainer/post-start.sh
recall sync --pull

# .devcontainer/pre-stop.sh (if supported)
recall sync --push
```

### 19.15 Pre-Implementation Checklist

**Recall Library:**

- [ ] Repository created: `github.com/hyperengineering/recall`
- [ ] Go module initialized with minimal dependencies
- [ ] Devcontainer configuration tested
- [ ] CI pipeline running (test + lint)
- [ ] Release workflow configured for binary distribution
- [ ] README with usage examples
- [ ] CLI tool functional (`recall record`, `recall query`, `recall sync`)

**Engram Service:**

- [ ] Repository created: `github.com/hyperengineering/engram`
- [ ] Go module initialized
- [ ] Devcontainer configuration tested
- [ ] CI pipeline running
- [ ] OpenAI API key obtained
- [ ] Fly.io account set up
- [ ] Fly.io volume created for persistence
- [ ] API key generation strategy defined

**Integration:**

- [ ] Recall added to Forge's go.mod
- [ ] MCP tools registered in Forge
- [ ] Devcontainer environment variables configured
- [ ] Lifecycle hooks implemented
- [ ] End-to-end sync tested

---

*The path is clear. Build well.*
