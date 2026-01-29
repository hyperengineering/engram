# API Usage Guide

Practical examples and workflows for integrating with Engram.

## Authentication

All endpoints except `/api/v1/health` require Bearer token authentication.

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://engram.example.com/api/v1/lore
```

The API key is configured via the `ENGRAM_API_KEY` environment variable on the server.

## Common Workflows

### Initial Bootstrap (New Environment)

When starting a new client environment, perform a full bootstrap:

1. **Check service health and compatibility**
2. **Download the complete snapshot**
3. **Store locally for semantic search**

```bash
# 1. Verify service is healthy and check embedding model
curl https://engram.example.com/api/v1/health

# 2. Download full snapshot
curl -H "Authorization: Bearer YOUR_API_KEY" \
  https://engram.example.com/api/v1/lore/snapshot \
  -o lore-snapshot.db

# 3. Replace local database with snapshot
# (Implementation-specific)
```

### Incremental Sync (Existing Environment)

For ongoing synchronization:

1. **Push local changes to Engram**
2. **Push any feedback collected**
3. **Pull changes from other environments**

```bash
# 1. Push new lore
curl -X POST https://engram.example.com/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env-123", "lore": [...]}'

# 2. Push feedback
curl -X POST https://engram.example.com/api/v1/lore/feedback \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"source_id": "my-env-123", "feedback": [...]}'

# 3. Pull changes since last sync
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

**Feedback Types:**

| Type | Effect | When to Use |
|------|--------|-------------|
| `helpful` | +0.08 confidence | Lore was useful and accurate |
| `not_relevant` | No change | Lore didn't apply to this context |
| `incorrect` | -0.15 confidence | Lore was wrong or misleading |

## Endpoint Examples

### Health Check

Check service availability and configuration.

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

**Field Reference:**

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

**Note:** If no snapshot is available yet, returns `503 Service Unavailable` with a `Retry-After` header.

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
      "embedding": [0.123, -0.456, 0.789],
      "source_id": "devcontainer-xyz789",
      "sources": ["devcontainer-xyz789", "devcontainer-abc123"],
      "validation_count": 3,
      "created_at": "2026-01-27T08:30:00Z",
      "updated_at": "2026-01-28T11:15:00Z",
      "last_validated_at": "2026-01-28T11:15:00Z",
      "embedding_status": "ready"
    }
  ],
  "deleted_ids": [
    "01ARYZ6S41TSV4RRFFQ69G5ABC"
  ],
  "as_of": "2026-01-28T14:30:00Z"
}
```

**Important:** Use the `as_of` timestamp as the `since` parameter for your next delta request.

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
- `not_relevant` feedback has no effect on confidence

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

## Best Practices

### Batch Size

- **Ingest:** Send up to 50 lore entries per request
- **Feedback:** Send up to 50 feedback entries per request
- Batching reduces round trips and improves throughput

### Sync Intervals

| Event | Recommended Action |
|-------|-------------------|
| Client startup | Full snapshot bootstrap |
| Task completion | Incremental sync |
| Every 5-60 minutes | Periodic incremental sync |
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

### Embedding Model Compatibility

Before syncing, verify embedding model matches:

```bash
local_model="text-embedding-3-small"
remote_model=$(curl -s https://engram.example.com/api/v1/health | jq -r '.embedding_model')

if [ "$local_model" != "$remote_model" ]; then
  echo "Warning: Model mismatch. Consider full re-bootstrap."
fi
```

If models differ, semantic search quality may degrade. Consider downloading a fresh snapshot.
