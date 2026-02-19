# Engram Universal Sync: Design Clarifications

**Status**: Response to Draft Review
**Reviewer**: Clario (Architecture Agent)
**Date**: 2026-02-17
**Reference**: `docs/engram-changelog-sync-design.md`

---

## Summary

The design is architecturally sound. The separation of generic sync layer from domain plugins is elegant, and the move from timestamp-based to sequence-based ordering is correct.

Six areas required clarification. Resolutions are documented below for incorporation into the final design.

---

## Clarification 1: Change Log Compaction

### Problem

The `change_log` table grows indefinitely. Without compaction, production stores face storage bloat and degraded query performance.

### Resolution

| Parameter | Value |
|-----------|-------|
| **Retention window** | 7 days |
| **Tombstone retention** | 7 days (aligned) |
| **Audit preservation** | Export to append-only archive before compaction |
| **Trigger** | Automatic daily background job |
| **Manual option** | CLI command for on-demand compaction |

### Implementation Notes

- Compact entries where `synced_at` is older than 7 days AND all registered clients have pulled past that sequence
- Before compaction, export affected entries to `{store_id}_audit_{date}.jsonl`
- Retain audit exports according to organizational retention policy (outside Engram scope)

### Schema Addition

```sql
-- Track compaction state
ALTER TABLE sync_meta ADD COLUMN last_compaction_seq INTEGER DEFAULT 0;
ALTER TABLE sync_meta ADD COLUMN last_compaction_at TEXT;
```

---

## Clarification 2: Push Idempotency

### Problem

Network failures cause retries. Without idempotency, duplicate pushes create duplicate change log entries and trigger redundant `OnReplay` side effects.

### Resolution

| Parameter | Value |
|-----------|-------|
| **Mechanism** | `push_id` (client-generated UUID) per push request |
| **Cache TTL** | 24 hours |
| **Cache storage** | Persisted (survives Engram restart) |
| **Stale retry behavior** | Client re-reads local state, generates fresh `push_id` |

### Push Request Schema (Revised)

```json
{
  "push_id": "550e8400-e29b-41d4-a716-446655440000",
  "source_id": "client-uuid",
  "schema_version": 5,
  "entries": [...]
}
```

### Engram Behavior

1. Check if `push_id` exists in idempotency cache
2. If exists: return cached response, skip processing
3. If new: process normally, cache `push_id` → response mapping with 24-hour TTL

### Schema Addition

```sql
CREATE TABLE push_idempotency (
    push_id     TEXT PRIMARY KEY,
    store_id    TEXT NOT NULL,
    response    TEXT NOT NULL,  -- JSON-encoded response
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    expires_at  TEXT NOT NULL
);

CREATE INDEX idx_push_idempotency_expires ON push_idempotency (expires_at);
```

---

## Clarification 3: Source Filtering

### Problem

Original concern: crash recovery could cause data loss if client skips own entries during pull before confirming local application.

### Resolution

**No protocol change required.**

The combination of:
1. **Transactional outbox pattern** (domain write + change_log entry in same transaction)
2. **Push idempotency** (retry after crash returns cached response)
3. **Source filtering** (skip own entries on pull)

...handles crash recovery correctly. If client crashes before push confirmation, restart triggers retry, idempotency returns success, client marks entries synced.

### Precondition

All domain writes MUST be wrapped in transactions that include the change_log entry. Tract confirms this is already the case.

---

## Clarification 4: Schema Evolution

### Problem

Tract uses goose migrations. When new tables are added, the protocol must handle version mismatches between clients and server.

### Resolution

| Parameter | Value |
|-----------|-------|
| **Migration trigger** | CI/CD pipeline (Ops-controlled) |
| **Version policy** | Client ≤ Server always; reject clients ahead of server |
| **Existing store migration** | Auto-migrate with pre-migration snapshot |
| **Snapshot retention** | 7 days |

### Release Process Invariant

```
Engram Tract plugin deploys BEFORE Tract client publishes.
Server schema version is always ≥ any client schema version.
```

### Push/Pull Request Schema Addition

```json
{
  "push_id": "...",
  "source_id": "...",
  "schema_version": 5,
  "entries": [...]
}
```

### Engram Behavior

| Client Version | Server Version | Result |
|----------------|----------------|--------|
| 5 | 5 | Accept |
| 4 | 5 | Accept (backward compatible) |
| 5 | 4 | Reject |

### Rejection Response

```json
{
  "error": "schema_version_mismatch",
  "message": "Client schema version 6 is ahead of server version 5. Engram upgrade required.",
  "client_version": 6,
  "server_version": 5
}
```

### Store Metadata Addition

```yaml
# meta.yaml
type: tract
schema_version: 5
created: 2026-02-17T10:00:00Z
```

### Migration Procedure

```
For each store where type = 'tract':
  1. VACUUM INTO backup_{store_id}_{timestamp}.db
  2. Apply pending migrations
  3. PRAGMA integrity_check
  4. PRAGMA foreign_key_check
  5. If failure → restore from backup, alert Ops
  6. Retain backup for 7 days
```

---

## Clarification 5: Push Transaction Semantics

### Problem

The original response schema implied partial success was possible. Partial success creates complex client recovery logic, especially with FK dependencies.

### Resolution

| Parameter | Value |
|-----------|-------|
| **Model** | All-or-nothing |
| **Success** | All entries accepted |
| **Failure** | Zero entries accepted, errors returned |
| **Client retry** | Fix errors, generate new `push_id`, retry full batch |

### Push Response Schema (Revised)

**Success:**
```json
{
  "accepted": 15,
  "remote_sequence": 1042
}
```

**Failure:**
```json
{
  "accepted": 0,
  "errors": [
    {
      "sequence": 3,
      "table_name": "goals",
      "entity_id": "G-02",
      "code": "VALIDATION_ERROR",
      "message": "missing required field: name"
    }
  ]
}
```

### Rationale

1. Simplicity: client either succeeds completely or retries completely
2. FK safety: prevents orphaned children when parent is rejected
3. Rare failures: local validation should catch most issues before push
4. Idempotency alignment: new `push_id` on retry, clean slate

---

## Clarification 6: Delta Pagination

### Problem

Using `since` parameter with client-tracked sequences can produce duplicates if new entries arrive mid-pagination.

### Resolution

| Parameter | Value |
|-----------|-------|
| **Parameter name** | `after` (exclusive) |
| **Response fields** | `last_sequence`, `latest_sequence`, `has_more` |
| **Client tracking** | Store `last_sequence` as next `after` value |

### Delta Request (Revised)

```
GET /api/v1/stores/{id}/sync/delta?after=1000&limit=500
```

### Delta Response (Revised)

```json
{
  "entries": [...],
  "last_sequence": 1500,
  "latest_sequence": 2042,
  "has_more": true
}
```

| Field | Description |
|-------|-------------|
| `last_sequence` | Highest sequence number in this response page |
| `latest_sequence` | Highest sequence number on server (total remaining indicator) |
| `has_more` | Boolean indicating more entries exist beyond this page |

### Client Pagination Logic

```go
afterSeq := lastPullSeq
for {
    resp := client.Delta(afterSeq, 500)
    applyEntries(resp.Entries)
    afterSeq = resp.LastSequence
    updateSyncMeta("last_pull_seq", afterSeq)
    if !resp.HasMore {
        break
    }
}
```

---

## Updated API Summary

### Push Endpoint

```
POST /api/v1/stores/{id}/sync/push

Request:
{
  "push_id": "<uuid>",
  "source_id": "<client-uuid>",
  "schema_version": <int>,
  "entries": [...]
}

Response (success):
{
  "accepted": <int>,
  "remote_sequence": <int>
}

Response (failure):
{
  "accepted": 0,
  "errors": [{"sequence": <int>, "table_name": "<str>", "entity_id": "<str>", "code": "<str>", "message": "<str>"}]
}
```

### Delta Endpoint

```
GET /api/v1/stores/{id}/sync/delta?after={seq}&limit={n}

Response:
{
  "entries": [...],
  "last_sequence": <int>,
  "latest_sequence": <int>,
  "has_more": <bool>
}
```

### Snapshot Endpoint

No changes from original design.

```
GET /api/v1/stores/{id}/sync/snapshot

Response: application/octet-stream (SQLite database file)
```

---

## Open Questions — Resolved

| # | Original Question | Resolution |
|---|-------------------|------------|
| 1 | Compaction strategy | 7-day window, audit export, daily auto-job |
| 2 | Schema evolution handling | CI/CD migration, version handshake, reject ahead-of-server |
| 3 | Large payloads | Defer to Phase 4 (compression/streaming) |
| 4 | Multi-store sync | Each store independent, no cross-store refs in V1 |
| 5 | Access control | Single bearer token sufficient for V1 |

---

## Next Steps

1. Incorporate these clarifications into the design document
2. Update API specifications with revised schemas
3. Proceed with Phase 1 implementation (Engram Generic Layer)

---

*The path is clear. Build well.*
