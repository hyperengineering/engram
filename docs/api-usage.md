# API Usage Guide

This guide covers practical examples and workflows for integrating with Engram.

## Authentication

All endpoints except `/api/v1/health` and `/api/v1/stats` require Bearer token authentication:

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://engram.example.com/api/v1/lore
```

The API key is configured via the `ENGRAM_API_KEY` environment variable on the server.

## Recall Client Integration

[Recall](https://github.com/hyperengineering/recall) is the client library that AI agents use to interact with Engram. Understanding the client workflow helps contextualize the API design.

### How Recall Clients Work

Recall maintains a **local SQLite database** that mirrors Engram's lore store. This architecture provides:

- **Low-latency queries** — Semantic search runs against local data, not over the network
- **Offline capability** — Agents can record and query lore without connectivity
- **Efficient sync** — Only changed data transfers during synchronization

```
┌─────────────────────────────────────────────────────────────┐
│                      Recall Client                          │
│                                                             │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────┐ │
│  │   Record    │    │   Query     │    │    Feedback     │ │
│  │   (write)   │    │   (read)    │    │    (update)     │ │
│  └──────┬──────┘    └──────┬──────┘    └────────┬────────┘ │
│         │                  │                     │          │
│         ▼                  ▼                     ▼          │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                  Local SQLite DB                      │  │
│  └──────────────────────────────────────────────────────┘  │
│                           │                                 │
│                           │ sync                            │
│                           ▼                                 │
└───────────────────────────┼─────────────────────────────────┘
                            │
                    ┌───────┴───────┐
                    │    Engram     │
                    │   (central)   │
                    └───────────────┘
```

### Synchronization Flow

1. **Bootstrap** (new environment):
   - Client calls `GET /api/v1/lore/snapshot` to download the full database
   - Replaces local database with the snapshot

2. **Incremental Sync** (ongoing):
   - Client calls `POST /api/v1/lore` to push new lore entries
   - Client calls `POST /api/v1/lore/feedback` to push feedback
   - Client calls `GET /api/v1/lore/delta?since=<timestamp>` to pull changes

3. **Shutdown Flush**:
   - Before shutdown, client pushes any pending lore and feedback

### Session Reference System

During a session, Recall tracks which lore was surfaced and assigns simple references (L1, L2, L3...) for easy feedback:

```
Agent receives context with recalled lore:
  [L1] Queue consumers need idempotency verification (confidence: 0.80)
  [L2] Message broker confirmations can be lost on crash (confidence: 0.75)
  [L3] Batch processing can exceed memory limits (confidence: 0.65)

After using the knowledge, agent provides feedback:
  recall_feedback(helpful: ["L1", "L2"], not_relevant: ["L3"])
```

## Common Workflows

### Initial Bootstrap (New Environment)

When starting a new client environment, perform a full bootstrap:

```bash
# 1. Verify service is healthy and check embedding model
curl https://engram.example.com/api/v1/health

# 2. Check if models match (important for semantic search compatibility)
# Response includes: "embedding_model": "text-embedding-3-small"

# 3. Download full snapshot
curl -H "Authorization: Bearer YOUR_API_KEY" \
  https://engram.example.com/api/v1/lore/snapshot \
  -o lore-snapshot.db

# 4. Replace local database with snapshot
# (Implementation-specific to your client)
```

### Incremental Sync (Existing Environment)

For ongoing synchronization after initial bootstrap:

```bash
# 1. Push new lore entries
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env-123", "lore": [...]}'

# 2. Push feedback collected during session
curl -X POST https://engram.example.com/api/v1/lore/feedback \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env-123", "feedback": [...]}'

# 3. Pull changes from other environments
curl -H "Authorization: Bearer YOUR_API_KEY" \
  "https://engram.example.com/api/v1/lore/delta?since=2026-01-28T10:00:00Z"
```

### Recording Lore (Agent Learning)

When an agent discovers something worth remembering:

```bash
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "devcontainer-abc123",
    "lore": [
      {
        "content": "ORM generates N+1 queries for polymorphic associations unless eager loading is explicitly configured",
        "context": "Discovered while profiling Rails app performance in Story 3.2",
        "category": "DEPENDENCY_BEHAVIOR",
        "confidence": 0.75
      }
    ]
  }'
```

### Providing Feedback (Agent Experience)

After using recalled lore, provide feedback to improve future recommendations:

```bash
curl -X POST https://engram.example.com/api/v1/lore/feedback \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "devcontainer-abc123",
    "feedback": [
      {"lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4G", "type": "helpful"},
      {"lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4H", "type": "not_relevant"},
      {"lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4I", "type": "incorrect"}
    ]
  }'
```

**Feedback Effects:**

| Type | Confidence Change | When to Use |
|------|-------------------|-------------|
| `helpful` | +0.08 | Lore was useful and accurate |
| `not_relevant` | No change | Lore didn't apply to this context |
| `incorrect` | -0.15 | Lore was wrong or misleading |

The asymmetric penalty for `incorrect` feedback reflects that bad information is more costly than good information is helpful.

## Endpoint Reference

### Health Check

Check service availability and configuration. **No authentication required.**

**Request:**

```bash
curl https://engram.example.com/api/v1/health
```

**Response:**

```json
{
  "status": "healthy",
  "version": "1.0.0",
  "embedding_model": "text-embedding-3-small",
  "lore_count": 1234,
  "last_snapshot": "2026-01-28T12:00:00Z"
}
```

**Use this to:**

- Verify service is running before sync
- Check embedding model compatibility
- Monitor lore count growth

---

### Extended Stats

Get detailed system metrics for monitoring dashboards. **No authentication required.**

**Request:**

```bash
curl https://engram.example.com/api/v1/stats
```

**Response:**

```json
{
  "total_lore": 1234,
  "active_lore": 1200,
  "deleted_lore": 34,
  "embedding_stats": {
    "complete": 1195,
    "pending": 3,
    "failed": 2
  },
  "category_stats": {
    "ARCHITECTURAL_DECISION": 156,
    "PATTERN_OUTCOME": 234,
    "INTERFACE_LESSON": 89,
    "EDGE_CASE_DISCOVERY": 178,
    "IMPLEMENTATION_FRICTION": 145,
    "TESTING_STRATEGY": 201,
    "DEPENDENCY_BEHAVIOR": 112,
    "PERFORMANCE_INSIGHT": 85
  },
  "quality_stats": {
    "average_confidence": 0.72,
    "validated_count": 456,
    "high_confidence_count": 890,
    "low_confidence_count": 78
  },
  "unique_source_count": 12,
  "last_snapshot": "2026-01-28T12:00:00Z",
  "last_decay": "2026-01-28T00:00:00Z",
  "stats_as_of": "2026-01-30T14:30:00Z"
}
```

**Field descriptions:**

| Field | Description |
|-------|-------------|
| `total_lore` | All lore entries including deleted |
| `active_lore` | Non-deleted lore entries |
| `deleted_lore` | Soft-deleted entries |
| `embedding_stats.complete` | Entries with generated embeddings |
| `embedding_stats.pending` | Entries awaiting embedding generation |
| `embedding_stats.failed` | Entries that failed embedding after max retries |
| `category_stats` | Count of entries per category |
| `quality_stats.average_confidence` | Mean confidence score |
| `quality_stats.validated_count` | Entries with at least one "helpful" feedback |
| `quality_stats.high_confidence_count` | Entries with confidence >= 0.7 |
| `quality_stats.low_confidence_count` | Entries with confidence < 0.3 |
| `unique_source_count` | Number of distinct source environments |
| `last_snapshot` | When the last snapshot was generated |
| `last_decay` | When confidence decay last ran |

---

### Ingest Lore

Submit new lore entries for storage.

**Request:**

```bash
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "devcontainer-abc123",
    "lore": [
      {
        "content": "Queue consumers benefit from idempotency verification in integration tests",
        "context": "Learned after debugging flaky test suite",
        "category": "TESTING_STRATEGY",
        "confidence": 0.75
      },
      {
        "content": "Event sourcing adds unnecessary complexity for simple CRUD operations",
        "context": "Architecture review for inventory service",
        "category": "ARCHITECTURAL_DECISION",
        "confidence": 0.80
      }
    ]
  }'
```

**Response:**

```json
{
  "accepted": 2,
  "merged": 0,
  "rejected": 0,
  "errors": []
}
```

**With semantic deduplication (merged response):**

```json
{
  "accepted": 1,
  "merged": 1,
  "rejected": 0,
  "errors": []
}
```

When a submitted entry is semantically similar (>92% cosine similarity) to existing lore, it merges rather than creating a duplicate. The merge:
- Boosts confidence by +0.10
- Appends the new context
- Aggregates source IDs

**Partial Success (some entries invalid):**

```json
{
  "accepted": 1,
  "merged": 0,
  "rejected": 1,
  "errors": [
    "lore[1].content: exceeds maximum length of 4000 characters"
  ]
}
```

**Field Constraints:**

| Field | Required | Constraints |
|-------|----------|-------------|
| `source_id` | Yes | Non-empty string |
| `lore` | Yes | 1-50 entries |
| `lore[].content` | Yes | 1-4000 characters, valid UTF-8 |
| `lore[].context` | No | 0-1000 characters, valid UTF-8 |
| `lore[].category` | Yes | Valid category enum |
| `lore[].confidence` | Yes | 0.0 to 1.0 |

**Valid Categories:**

- `ARCHITECTURAL_DECISION` — High-level design choices
- `PATTERN_OUTCOME` — Results of applying patterns
- `INTERFACE_LESSON` — API/contract design insights
- `EDGE_CASE_DISCOVERY` — Unexpected behaviors
- `IMPLEMENTATION_FRICTION` — Design-to-code difficulties
- `TESTING_STRATEGY` — Testing approach insights
- `DEPENDENCY_BEHAVIOR` — Library/framework gotchas
- `PERFORMANCE_INSIGHT` — Performance discoveries

---

### Download Snapshot

Get a complete database snapshot for initial bootstrap.

**Request:**

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" \
  https://engram.example.com/api/v1/lore/snapshot \
  -o engram-snapshot.db
```

**Response:**

- **Content-Type:** `application/octet-stream`
- **Body:** Binary SQLite database file

The snapshot contains all active lore with embeddings packed as float32 arrays.

**If no snapshot is available yet:**

Returns `503 Service Unavailable` with a `Retry-After` header indicating when to retry.

```bash
# Check response headers
curl -I -H "Authorization: Bearer YOUR_API_KEY" \
  https://engram.example.com/api/v1/lore/snapshot
```

---

### Get Delta

Retrieve incremental changes since a given timestamp.

**Request:**

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" \
  "https://engram.example.com/api/v1/lore/delta?since=2026-01-28T10:00:00Z"
```

**Response:**

```json
{
  "lore": [
    {
      "id": "01ARYZ6S41TSV4RRFFQ69G5FAV",
      "content": "Event sourcing adds unnecessary complexity for simple CRUD operations",
      "context": "Architecture review for inventory service",
      "category": "ARCHITECTURAL_DECISION",
      "confidence": 0.85,
      "embedding": [0.123, -0.456, 0.789, ...],
      "source_id": "devcontainer-xyz789",
      "sources": ["devcontainer-xyz789", "devcontainer-abc123"],
      "validation_count": 3,
      "created_at": "2026-01-27T08:30:00Z",
      "updated_at": "2026-01-28T11:15:00Z",
      "last_validated_at": "2026-01-28T11:15:00Z",
      "embedding_status": "complete"
    }
  ],
  "deleted_ids": [
    "01ARYZ6S41TSV4RRFFQ69G5ABC"
  ],
  "as_of": "2026-01-28T14:30:00Z"
}
```

**Important:** Use the `as_of` timestamp as the `since` parameter for your next delta request to ensure no changes are missed.

**Timestamp Format:** RFC 3339 (e.g., `2026-01-28T10:00:00Z`)

---

### Submit Feedback

Provide feedback on recalled lore to adjust confidence scores.

**Request:**

```bash
curl -X POST https://engram.example.com/api/v1/lore/feedback \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "devcontainer-abc123",
    "feedback": [
      {"lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4G", "type": "helpful"},
      {"lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4H", "type": "incorrect"}
    ]
  }'
```

**Response:**

```json
{
  "updates": [
    {
      "lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4G",
      "previous_confidence": 0.80,
      "current_confidence": 0.88,
      "validation_count": 5
    },
    {
      "lore_id": "01HQ5K9X2YPZV3CMWN8BTRFJ4H",
      "previous_confidence": 0.65,
      "current_confidence": 0.50
    }
  ]
}
```

**Notes:**

- `validation_count` only appears for `helpful` feedback
- Confidence is capped at 1.0 and floored at 0.0
- `not_relevant` feedback has no effect on confidence (no penalty for context mismatch)

---

### Delete Lore

Soft-delete a lore entry. The entry is marked as deleted but retained for delta sync.

**Request:**

```bash
curl -X DELETE https://engram.example.com/api/v1/lore/01HQ5K9X2YPZV3CMWN8BTRFJ4G \
  -H "Authorization: Bearer YOUR_API_KEY"
```

**Response:** `204 No Content`

---

## Error Handling

### Validation Errors

When request validation fails, you receive detailed field errors:

```bash
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "", "lore": []}'
```

**Response (422):**

```json
{
  "type": "https://engram.dev/errors/validation-error",
  "title": "Validation Error",
  "status": 422,
  "detail": "Request contains invalid fields",
  "instance": "/api/v1/lore",
  "errors": [
    {"field": "source_id", "message": "is required"},
    {"field": "lore", "message": "is required and must not be empty"}
  ]
}
```

### Authentication Failures

```json
{
  "type": "https://engram.dev/errors/unauthorized",
  "title": "Unauthorized",
  "status": 401,
  "detail": "Missing or invalid API key",
  "instance": "/api/v1/lore"
}
```

### Service Unavailable

When the snapshot isn't ready:

```json
{
  "type": "https://engram.dev/errors/service-unavailable",
  "title": "Service Unavailable",
  "status": 503,
  "detail": "Snapshot not yet available. Please retry after the indicated interval.",
  "instance": "/api/v1/lore/snapshot"
}
```

Check the `Retry-After` header for wait time in seconds.

See [Error Reference](errors.md) for the complete list of error types.

## Best Practices

### Batch Size

- **Ingest:** Send up to 50 lore entries per request
- **Feedback:** Send up to 50 feedback entries per request
- Batching reduces round trips and improves throughput

### Sync Intervals

| Event | Recommended Action |
|-------|-------------------|
| Client startup | Full snapshot bootstrap |
| Task/session completion | Incremental sync (push lore + feedback) |
| Every 5-60 minutes | Periodic incremental sync (pull delta) |
| Client shutdown | Flush all pending operations |

### Shutdown Flush

Before shutting down, ensure all pending lore and feedback is synced:

```bash
# Push any remaining lore
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env", "lore": [...]}'

# Push any remaining feedback
curl -X POST https://engram.example.com/api/v1/lore/feedback \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env", "feedback": [...]}'
```

### Retry Logic

For transient errors (500, 503):

1. Check `Retry-After` header if present
2. Use exponential backoff: 1s, 2s, 4s, 8s...
3. Maximum 3-5 retry attempts
4. Log failures for debugging

```python
import time
import requests

def make_request_with_retry(url, headers, max_retries=3):
    for attempt in range(max_retries):
        response = requests.get(url, headers=headers)

        if response.status_code == 503:
            retry_after = int(response.headers.get('Retry-After', 60))
            time.sleep(retry_after)
            continue

        if response.status_code == 500:
            time.sleep(2 ** attempt)  # Exponential backoff
            continue

        return response

    raise Exception("Max retries exceeded")
```

### Embedding Model Compatibility

Before syncing, verify embedding model matches:

```bash
local_model="text-embedding-3-small"
remote_model=$(curl -s https://engram.example.com/api/v1/health | jq -r '.embedding_model')

if [ "$local_model" != "$remote_model" ]; then
  echo "Warning: Model mismatch. Semantic search quality may degrade."
  echo "Consider downloading a fresh snapshot."
fi
```

If models differ, semantic search results may be inconsistent. Download a fresh snapshot to re-index with consistent embeddings.
