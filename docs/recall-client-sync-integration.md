# Recall Client: Universal Sync Integration Guide

**Status**: Draft
**Author**: Clario (Architecture Agent)
**Date**: 2026-02-18
**Audience**: Recall MCP tool / Recall CLI development team
**Engram Epic**: 8 — Universal Sync Protocol

---

## 1. Purpose

This document specifies the **mandatory client-side changes** the Recall team must implement to adopt Engram's universal sync protocol. It replaces the current domain-specific sync (outbox + `/lore/ingest` + `/lore/delta?since=`) with a generic, sequence-based protocol (`/sync/push` + `/sync/delta?after=` + `/sync/snapshot`).

**The legacy `/lore/*` endpoints will continue to function** during the transition period (Story 8.8 ensures backward compatibility). However, the Recall client should migrate to the new protocol to gain idempotent pushes, sequence-based ordering, and pagination.

---

## 2. What Changes for the Recall Client

### 2.1 Summary of Differences

| Aspect | Current (Legacy) | New (Universal Sync) |
|--------|-----------------|---------------------|
| Push endpoint | `POST /lore/ingest` | `POST /stores/{id}/sync/push` |
| Push payload | `NewLoreEntry[]` (domain objects) | `ChangeLogEntry[]` (generic change log) |
| Push idempotency | None | `push_id` UUID per request |
| Push semantics | Partial success allowed | All-or-nothing |
| Pull endpoint | `GET /lore/delta?since={timestamp}` | `GET /stores/{id}/sync/delta?after={sequence}` |
| Pull ordering | Timestamp-based (clock-skew fragile) | Sequence-based (monotonic, no skew) |
| Pull pagination | None | Built-in (`has_more` + `last_sequence`) |
| Pull response | `{lore[], deleted_ids[], as_of}` | `{entries[], last_sequence, latest_sequence, has_more}` |
| Bootstrap | `GET /lore/snapshot` | `GET /stores/{id}/sync/snapshot` |
| Local tracking | `sync_queue` + `last_sync` timestamp | `change_log` table + `sync_meta.last_push_seq` / `last_pull_seq` |
| Source filtering | Not supported | Client filters own `source_id` on pull |
| Schema versioning | None | `schema_version` handshake on push |

### 2.2 What Stays the Same

- MCP tool interface (`recall_query`, `recall_feedback`, `recall_record`, `recall_sync`) — these are user-facing; the sync protocol is an implementation detail behind `recall_sync`
- Local `lore_entries` table schema
- Bearer token authentication
- Offline-first operation — all tools work fully without Engram

---

## 3. Client-Side Database Changes

### 3.1 Add Migration 002

The Recall client must apply the same migration 002 that the server uses. This adds three tables to the local SQLite database.

```sql
-- Migration 002: Change Log for Sync Protocol

CREATE TABLE change_log (
    sequence    INTEGER PRIMARY KEY AUTOINCREMENT,
    table_name  TEXT    NOT NULL,
    entity_id   TEXT    NOT NULL,
    operation   TEXT    NOT NULL CHECK (operation IN ('upsert', 'delete')),
    payload     TEXT,
    source_id   TEXT    NOT NULL,
    created_at  TEXT    NOT NULL,
    received_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_change_log_sequence ON change_log (sequence);
CREATE INDEX idx_change_log_entity ON change_log (table_name, entity_id);

CREATE TABLE push_idempotency (
    push_id     TEXT    PRIMARY KEY,
    store_id    TEXT    NOT NULL,
    response    TEXT    NOT NULL,
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT    NOT NULL
);

CREATE INDEX idx_push_idempotency_expires ON push_idempotency (expires_at);

CREATE TABLE sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO sync_meta (key, value) VALUES ('schema_version', '2');
INSERT INTO sync_meta (key, value) VALUES ('last_compaction_seq', '0');
INSERT INTO sync_meta (key, value) VALUES ('last_compaction_at', '');
```

**Additional client-specific keys** to initialize in `sync_meta`:

```sql
INSERT INTO sync_meta (key, value) VALUES ('last_push_seq', '0');
INSERT INTO sync_meta (key, value) VALUES ('last_pull_seq', '0');
INSERT INTO sync_meta (key, value) VALUES ('source_id', '<generate-client-uuid>');
```

### 3.2 Replace `sync_queue` with `change_log`

The current outbox pattern writes to `sync_queue`. Replace this with writes to `change_log`.

**Current pattern:**
```
InsertLore → BEGIN TX → INSERT lore_entries → INSERT sync_queue → COMMIT
```

**New pattern:**
```
InsertLore → BEGIN TX → INSERT lore_entries → INSERT change_log → COMMIT
```

The `change_log` table serves as both the outbox (entries not yet pushed) and the audit trail.

### 3.3 Source ID

Every Recall client instance must have a stable, unique `source_id`. This is used for:
- Identifying which client originated a change (in `change_log.source_id`)
- Source filtering during delta pull (skip your own changes)

**Recommendation**: Generate a UUIDv4 on first launch and persist it in `sync_meta` under the key `source_id`.

---

## 4. Local Mutation Protocol

Every local write operation must atomically append to `change_log` within the same transaction.

### 4.1 Record (Upsert)

When `recall_record` creates a new lore entry:

```
BEGIN TX
  INSERT INTO lore_entries (id, content, category, ...) VALUES (...)
  INSERT INTO change_log (table_name, entity_id, operation, payload, source_id, created_at)
    VALUES ('lore_entries', :id, 'upsert', :full_entry_json, :source_id, :now)
COMMIT
```

**Payload format** for upserts — the full lore entry as JSON:

```json
{
  "id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "content": "Rate limiting prevents API abuse...",
  "context": "During code review",
  "category": "ARCHITECTURAL_DECISION",
  "confidence": 0.7,
  "embedding_status": "pending",
  "source_id": "client-abc-123",
  "sources": ["client-abc-123"],
  "validation_count": 0,
  "created_at": "2026-02-18T10:30:00.000Z",
  "updated_at": "2026-02-18T10:30:00.000Z",
  "deleted_at": null,
  "last_validated_at": null
}
```

### 4.2 Feedback (Confidence Update)

When `recall_feedback` adjusts confidence:

```
BEGIN TX
  UPDATE lore_entries SET confidence = ... WHERE id = :id
  INSERT INTO change_log (table_name, entity_id, operation, payload, source_id, created_at)
    VALUES ('lore_entries', :id, 'upsert', :full_updated_entry_json, :source_id, :now)
COMMIT
```

**Important**: Even though feedback is a partial update (confidence only), the change_log payload must contain the **full entity state** after the update. The sync protocol replays full state, not deltas.

### 4.3 Delete

If lore is ever deleted locally:

```
BEGIN TX
  UPDATE lore_entries SET deleted_at = :now WHERE id = :id
  INSERT INTO change_log (table_name, entity_id, operation, payload, source_id, created_at)
    VALUES ('lore_entries', :id, 'delete', NULL, :source_id, :now)
COMMIT
```

**Payload is NULL for deletes.**

---

## 5. Sync Protocol: Push

### 5.1 Endpoint

```
POST /api/v1/stores/{store_id}/sync/push
Authorization: Bearer <token>
Content-Type: application/json
```

### 5.2 Request Format

```json
{
  "push_id": "550e8400-e29b-41d4-a716-446655440000",
  "source_id": "client-abc-123",
  "schema_version": 2,
  "entries": [
    {
      "sequence": 42,
      "table_name": "lore_entries",
      "entity_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      "operation": "upsert",
      "payload": { "id": "...", "content": "...", "category": "...", ... },
      "created_at": "2026-02-18T10:30:00.000Z"
    },
    {
      "sequence": 43,
      "table_name": "lore_entries",
      "entity_id": "01ARZ3NDEKTSV4RRFFQ69G5FAX",
      "operation": "delete",
      "payload": null,
      "created_at": "2026-02-18T10:31:00.000Z"
    }
  ]
}
```

### 5.3 Field Requirements

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `push_id` | string (UUID) | Yes | Client-generated, unique per push attempt |
| `source_id` | string (UUID) | Yes | Stable client instance identifier |
| `schema_version` | int | Yes | Must be <= server schema version |
| `entries` | array | Yes | 1-1000 entries per push |
| `entries[].sequence` | int64 | Yes | Client-local sequence number |
| `entries[].table_name` | string | Yes | Must be `"lore_entries"` for Recall stores |
| `entries[].entity_id` | string | Yes | Primary key of affected row |
| `entries[].operation` | string | Yes | `"upsert"` or `"delete"` |
| `entries[].payload` | object/null | Conditional | Required for upserts, null for deletes |
| `entries[].created_at` | string (RFC 3339) | Yes | Client-side timestamp |

### 5.4 Payload Validation (Server-Side)

The Engram server validates lore payloads via the Recall domain plugin. The following fields are **required** in upsert payloads:

| Field | Type | Validation |
|-------|------|------------|
| `id` | string | Non-empty |
| `content` | string | Non-empty |
| `category` | string | Must be one of: `ARCHITECTURAL_DECISION`, `PATTERN_OUTCOME`, `INTERFACE_LESSON`, `EDGE_CASE_DISCOVERY`, `IMPLEMENTATION_FRICTION`, `TESTING_STRATEGY`, `DEPENDENCY_BEHAVIOR`, `PERFORMANCE_INSIGHT` |
| `source_id` | string | Non-empty |
| `confidence` | float | 0.0 - 1.0 inclusive |

### 5.5 Response: Success

```json
{
  "accepted": 15,
  "remote_sequence": 1042
}
```

- `accepted`: Number of entries processed
- `remote_sequence`: Highest server-assigned sequence number

**Client action on success**:
1. Update `sync_meta.last_push_seq` to the highest local sequence that was pushed
2. Optionally clean pushed entries from local `change_log` (or retain for audit)

### 5.6 Response: Idempotent Replay

If the same `push_id` is sent again (retry after timeout, network failure, etc.), the server returns the **cached original response** with header `X-Idempotent-Replay: true`.

**Client action**: Treat as success. Update local state as if the push succeeded.

### 5.7 Response: Validation Error (422)

```json
{
  "accepted": 0,
  "errors": [
    {
      "sequence": 42,
      "table_name": "lore_entries",
      "entity_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      "code": "VALIDATION_ERROR",
      "message": "missing required field: content"
    }
  ]
}
```

**Semantics**: All-or-nothing. If any entry fails validation, **zero entries are accepted**. The entire batch is rejected.

**Client action**: Fix the invalid entries and retry the full batch with the **same `push_id`** (not a new one — the original was never processed).

### 5.8 Response: Schema Mismatch (409)

```json
{
  "type": "https://engram.dev/errors/schema-mismatch",
  "title": "Schema Version Mismatch",
  "status": 409,
  "detail": "Client schema version 3 is ahead of server version 2. Engram upgrade required.",
  "client_version": 3,
  "server_version": 2
}
```

**Client action**: The Engram server needs to be upgraded before this client can sync. Log a warning and halt sync until the server is updated.

### 5.9 Push Algorithm

```
function push():
    source_id = sync_meta['source_id']
    last_push_seq = int(sync_meta['last_push_seq'])
    schema_version = int(sync_meta['schema_version'])

    entries = SELECT * FROM change_log
              WHERE sequence > last_push_seq
              AND source_id = source_id          -- only push own changes
              ORDER BY sequence ASC
              LIMIT 1000

    if entries is empty:
        return  -- nothing to push

    push_id = uuid4()  -- new UUID for this push attempt

    response = POST /stores/{store_id}/sync/push {
        push_id: push_id,
        source_id: source_id,
        schema_version: schema_version,
        entries: entries
    }

    if response.status == 200:
        sync_meta['last_push_seq'] = str(entries[-1].sequence)
        -- If more entries remain (> 1000), call push() again
    elif response.status == 422:
        log_error("Validation failed", response.errors)
        -- Fix entries or skip problematic batch
    elif response.status == 409:
        log_warning("Schema mismatch — server upgrade needed")
        halt_sync()
    else:
        -- Retry with same push_id (idempotent)
        retry_with_backoff(push_id)
```

---

## 6. Sync Protocol: Delta Pull

### 6.1 Endpoint

```
GET /api/v1/stores/{store_id}/sync/delta?after={sequence}&limit={n}
Authorization: Bearer <token>
```

### 6.2 Query Parameters

| Parameter | Type | Required | Default | Constraints |
|-----------|------|----------|---------|-------------|
| `after` | int64 | Yes | - | Exclusive lower bound; entries with sequence > after |
| `limit` | int | No | 500 | Max 1000, capped server-side |

### 6.3 Response Format

```json
{
  "entries": [
    {
      "sequence": 1001,
      "table_name": "lore_entries",
      "entity_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      "operation": "upsert",
      "payload": { "id": "...", "content": "...", ... },
      "source_id": "client-xyz-789",
      "created_at": "2026-02-17T10:30:00.000Z",
      "received_at": "2026-02-17T10:30:01.000Z"
    }
  ],
  "last_sequence": 1500,
  "latest_sequence": 2042,
  "has_more": true
}
```

| Field | Description |
|-------|-------------|
| `entries` | Change log entries (may be empty `[]`, never `null`) |
| `last_sequence` | Highest sequence in this response (use as next `after` value) |
| `latest_sequence` | Highest sequence on the server (indicates how far behind) |
| `has_more` | `true` if more entries exist beyond this page |

### 6.4 Source Filtering

The delta response includes **all changes** from all clients. The Recall client **must filter out its own entries** before applying them locally:

```
for entry in response.entries:
    if entry.source_id == my_source_id:
        continue  -- skip own changes (already applied locally)
    apply_entry(entry)
```

**Why client-side filtering?** The server cannot know which entries each client has already applied locally. The `source_id` match is the reliable signal.

### 6.5 Applying Entries

For each entry that passes source filtering:

- **Upsert**: `INSERT OR REPLACE INTO lore_entries (...) VALUES (...)` using the payload JSON. Queue embedding generation for the entry.
- **Delete**: `UPDATE lore_entries SET deleted_at = :received_at WHERE id = :entity_id`

**Apply entries in sequence order.** The entries array is already ordered by the server.

### 6.6 Pull Algorithm

```
function pull():
    source_id = sync_meta['source_id']
    last_pull_seq = int(sync_meta['last_pull_seq'])

    loop:
        response = GET /stores/{store_id}/sync/delta?after={last_pull_seq}&limit=500

        if response.status != 200:
            log_error("Delta pull failed")
            break

        for entry in response.entries:
            if entry.source_id == source_id:
                continue  -- source filtering

            BEGIN TX
                if entry.operation == 'upsert':
                    upsert_lore_entry(entry.payload)
                    queue_embedding(entry.entity_id)
                elif entry.operation == 'delete':
                    soft_delete_lore_entry(entry.entity_id)
            COMMIT

        last_pull_seq = response.last_sequence
        sync_meta['last_pull_seq'] = str(last_pull_seq)

        if not response.has_more:
            break

    return last_pull_seq
```

---

## 7. Sync Protocol: Bootstrap (Snapshot)

### 7.1 Endpoint

```
GET /api/v1/stores/{store_id}/sync/snapshot
Authorization: Bearer <token>
```

### 7.2 Response

- **200**: Binary SQLite database as `application/octet-stream`
- **503**: Snapshot not ready; `Retry-After: 60` header

### 7.3 Bootstrap Flow

Use snapshot when the client has **no local database** or needs a **full reset**:

```
function bootstrap():
    response = GET /stores/{store_id}/sync/snapshot

    if response.status == 503:
        wait(response.headers['Retry-After'])
        retry bootstrap()

    if response.status != 200:
        fail("Bootstrap failed")

    -- Write to temp file
    write_file(temp_path, response.body)

    -- Verify integrity
    db = open(temp_path)
    result = db.exec("PRAGMA integrity_check")
    if result != "ok":
        fail("Snapshot integrity check failed")

    -- Read sync state from snapshot
    last_seq = db.query("SELECT MAX(sequence) FROM change_log") or 0
    db.close()

    -- Atomic replacement
    rename(temp_path, local_db_path)

    -- Initialize sync metadata for future delta pulls
    sync_meta['last_pull_seq'] = str(last_seq)
    sync_meta['last_push_seq'] = '0'  -- no local changes yet
    sync_meta['source_id'] = uuid4()  -- fresh source ID for this instance
```

### 7.4 Snapshot Contents

The downloaded snapshot includes:

| Table | Purpose |
|-------|---------|
| `lore_entries` | All lore data |
| `store_metadata` | Store configuration |
| `change_log` | Server-side change history |
| `push_idempotency` | Server push cache (can be ignored client-side) |
| `sync_meta` | Sync protocol state |

After bootstrap, the client should use `MAX(change_log.sequence)` as the starting point for delta pulls.

---

## 8. Full Sync Cycle

The recommended sync order is **push first, then pull**:

```
function sync():
    -- 1. Push local changes to server
    push()

    -- 2. Pull remote changes from server
    pull()
```

**Why push first?**
- Ensures the server has the client's latest state before the client pulls
- Prevents the client from receiving its own changes back and having to filter them
- If push fails, pull can still proceed (client gets latest server state)

### 8.1 Sync Triggers

The Recall client should sync when:

1. `recall_sync()` is called explicitly by the user/agent
2. Periodically (e.g., every 5 minutes if Engram server is configured)
3. On application startup (if server is reachable)

### 8.2 Conflict Resolution

The protocol uses **last-writer-wins** semantics:

- If two clients modify the same lore entry, the last push to the server wins
- The server assigns monotonically increasing sequence numbers
- Clients replay entries in sequence order, so the latest write naturally overwrites earlier ones

No merge logic is required on the client side.

---

## 9. Environment Configuration

The Recall client must support the following configuration:

| Variable | Required | Description |
|----------|----------|-------------|
| `ENGRAM_URL` | Yes (for sync) | Engram server URL (e.g., `http://localhost:8080`) |
| `ENGRAM_API_KEY` | Yes (for sync) | Bearer token for authentication |
| `ENGRAM_STORE_ID` | Yes (for sync) | Store identifier on the Engram server |

When these are not configured, the Recall client operates in **offline-only mode** — all operations work locally, sync is simply skipped.

---

## 10. Error Handling

### 10.1 Network Errors

- Retry push with the **same `push_id`** (idempotent, safe to retry)
- Use exponential backoff: 1s, 2s, 4s, 8s, max 60s
- After 5 retries, log warning and skip this sync cycle

### 10.2 Server Errors (5xx)

- Treat as transient; retry with backoff
- Same `push_id` for push retries

### 10.3 Validation Errors (422)

- Log the specific errors from the response
- Do **not** retry automatically — the entries need fixing
- Consider quarantining invalid entries and pushing the rest

### 10.4 Schema Mismatch (409)

- Halt sync entirely
- Log: "Engram server needs upgrade to schema version {client_version}"
- Continue operating offline until server is updated

### 10.5 Snapshot Unavailable (503)

- Wait for `Retry-After` duration (typically 60s)
- Retry up to 3 times
- If still unavailable, fall back to delta sync from sequence 0

---

## 11. Migration Path from Legacy Sync

### Phase 1: Dual-Write (Backward Compatible)

During the transition:

1. **Server-side** (Story 8.8): Legacy `/lore/ingest` and `/lore/delete` endpoints now write to `change_log` automatically. Clients using the old protocol generate sync-compatible change logs without any client changes.

2. **Client continues using legacy endpoints** — no client changes required in Phase 1.

### Phase 2: Client Migration

When the Recall client is ready to adopt the new protocol:

1. Apply migration 002 to local database (add `change_log`, `sync_meta` tables)
2. Replace `sync_queue` with `change_log` writes in all mutation paths
3. Replace push logic: `POST /lore/ingest` → `POST /sync/push`
4. Replace pull logic: `GET /lore/delta?since=` → `GET /sync/delta?after=`
5. Replace bootstrap logic: `GET /lore/snapshot` → `GET /sync/snapshot`
6. Initialize `sync_meta` with `last_push_seq=0`, `last_pull_seq=0`
7. On first sync after migration: bootstrap via snapshot to establish sequence baseline

### Phase 3: Deprecate Legacy

Once all Recall clients have migrated:
- Remove `sync_queue` table
- Remove legacy `/lore/ingest` push code from client
- Remove legacy `/lore/delta` pull code from client
- Server legacy endpoints remain available (no server changes needed)

### Migration Safety

- **No data loss**: Snapshot bootstrap provides a clean starting point
- **No duplicate processing**: Push idempotency prevents double-writes
- **Rollback**: If migration fails, client can revert to legacy endpoints (they still work)

---

## 12. Testing Requirements

The Recall team must validate the following scenarios before shipping the new sync integration:

### 12.1 Unit Tests

| Category | Test |
|----------|------|
| **Local mutations** | Every `recall_record` call writes to both `lore_entries` and `change_log` atomically |
| **Local mutations** | `recall_feedback` writes confidence update + `change_log` upsert entry |
| **Local mutations** | Delete writes soft-delete + `change_log` delete entry (null payload) |
| **Push** | Builds correct `PushRequest` from local `change_log` entries |
| **Push** | Generates unique `push_id` per push attempt |
| **Push** | Updates `last_push_seq` on success |
| **Push** | Retries with same `push_id` on network error |
| **Pull** | Filters out entries matching own `source_id` |
| **Pull** | Applies upserts and deletes in sequence order |
| **Pull** | Paginates when `has_more` is true |
| **Pull** | Updates `last_pull_seq` after each page |
| **Bootstrap** | Verifies SQLite integrity after download |
| **Bootstrap** | Initializes `sync_meta` from snapshot |

### 12.2 Integration Tests

| Scenario | Description |
|----------|-------------|
| **Round-trip** | Client A records lore → push → Client B pulls → Client B has the entry |
| **Conflict** | Client A and B both modify same entry → both push → both pull → both converge to last-writer-wins |
| **Idempotent retry** | Push fails mid-flight → retry same push_id → no duplicate entries on server |
| **Large batch** | Push 1000 entries → verify all accepted → pull from another client → all received |
| **Bootstrap + delta** | Fresh client bootstraps → records local changes → pushes → pulls additional remote changes |
| **Offline resilience** | Record lore while offline → come online → sync succeeds → server has all entries |

### 12.3 Compatibility Tests

| Scenario | Description |
|----------|-------------|
| **Legacy server** | Client with new sync code connects to server without `/sync/*` endpoints → falls back to legacy |
| **Mixed clients** | Legacy client pushes via `/lore/ingest` → new client pulls via `/sync/delta` → sees the changes |
| **Schema mismatch** | Client at schema version 3 connects to server at version 2 → receives 409 → halts gracefully |

---

## 13. API Reference Summary

### Push

```
POST /api/v1/stores/{store_id}/sync/push
Content-Type: application/json
Authorization: Bearer <token>

Request:  { push_id, source_id, schema_version, entries[] }
Success:  200 { accepted, remote_sequence }
Replay:   200 { accepted, remote_sequence } + X-Idempotent-Replay: true
Invalid:  422 { accepted: 0, errors[] }
Mismatch: 409 { type, title, status, detail, client_version, server_version }
```

### Delta

```
GET /api/v1/stores/{store_id}/sync/delta?after={seq}&limit={n}
Authorization: Bearer <token>

Success:  200 { entries[], last_sequence, latest_sequence, has_more }
Invalid:  400 { type, title, status, detail }
```

### Snapshot

```
GET /api/v1/stores/{store_id}/sync/snapshot
Authorization: Bearer <token>

Success:  200 application/octet-stream (binary SQLite)
Pending:  503 + Retry-After: 60
```

---

## 14. Change Log Entry Schema (Reference)

```json
{
  "sequence":    1042,
  "table_name":  "lore_entries",
  "entity_id":   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "operation":   "upsert",
  "payload":     { ... full entity JSON ... },
  "source_id":   "client-abc-123",
  "created_at":  "2026-02-18T10:30:00.000Z",
  "received_at": "2026-02-18T10:30:01.000Z"
}
```

| Field | Push Request | Delta Response | Notes |
|-------|-------------|----------------|-------|
| `sequence` | Client-local sequence | Server-assigned sequence | Different namespaces |
| `table_name` | Required | Always present | Must be `"lore_entries"` for Recall |
| `entity_id` | Required | Always present | Lore entry ID |
| `operation` | Required | Always present | `"upsert"` or `"delete"` |
| `payload` | Required for upsert, null for delete | Same | Full entity JSON |
| `source_id` | Sent in request body, not per-entry | Always present | Used for source filtering |
| `created_at` | Required (client time) | Always present | Client-side timestamp |
| `received_at` | Not sent (server sets it) | Always present | Server-side timestamp |

---

## 15. Lore Entry Payload Schema (Reference)

Required fields for `upsert` payloads passing Recall domain plugin validation:

```json
{
  "id":                "string (required, ULID)",
  "content":           "string (required, non-empty)",
  "context":           "string (optional)",
  "category":          "string (required, one of 8 valid categories)",
  "confidence":        0.7,
  "embedding":         null,
  "embedding_status":  "pending",
  "source_id":         "string (required, non-empty)",
  "sources":           ["string"],
  "validation_count":  0,
  "created_at":        "2026-02-18T10:30:00.000Z",
  "updated_at":        "2026-02-18T10:30:00.000Z",
  "deleted_at":        null,
  "last_validated_at": null
}
```

**Valid categories**: `ARCHITECTURAL_DECISION`, `PATTERN_OUTCOME`, `INTERFACE_LESSON`, `EDGE_CASE_DISCOVERY`, `IMPLEMENTATION_FRICTION`, `TESTING_STRATEGY`, `DEPENDENCY_BEHAVIOR`, `PERFORMANCE_INSIGHT`

---

*The path is clear. Build well.*
