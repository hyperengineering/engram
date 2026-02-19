# NeuralMux Sync Compatibility Matrix

This document defines which versions of Engram, Recall, and Tract are compatible with each other via the Universal Sync Protocol.

## Sync Protocol Version

The sync protocol uses a `schema_version` field in every push request. The server rejects pushes when the client's schema version is ahead of the server's, returning `409 Conflict`. Clients with a schema version equal to or behind the server's are accepted.

**Current protocol version: 2**

## Compatibility Matrix

| Engram Server | Recall Client | Tract Client | Sync Protocol | Notes |
|---------------|---------------|--------------|---------------|-------|
| **v1.3.0+** | **v1.3.1+** | **v0.1.4+** | v2 | Full sync support: push, delta, snapshot/bootstrap |
| v1.2.x | v1.3.0 | — | — | Sync endpoints not available; legacy ingest only |
| v1.2.x | v1.2.x | — | — | Pre-sync: legacy push/delta protocol |

### Minimum Versions for Sync Features

| Feature | Engram | Recall | Tract |
|---------|--------|--------|-------|
| Sync push (client → server) | v1.3.0 | v1.3.1 | v0.1.4 |
| Sync delta (server → client) | v1.3.0 | v1.3.1 | v0.1.4 |
| Sync bootstrap (snapshot download) | v1.3.0 | v1.3.1 | v0.1.4 |
| Push idempotency (retry-safe) | v1.3.0 | v1.3.1 | v0.1.4 |
| Legacy lore ingest (backward compat) | v1.0.0 | v1.0.0 | — |
| Double-encoded payload rejection | v1.3.0 | — | — |
| Change log compaction | v1.3.0 | — | — |

## Known Incompatibilities

| Client Version | Issue | Resolution |
|----------------|-------|------------|
| Recall v1.3.0 | Sync push panics (`storeID` never set) | Upgrade to v1.3.1+ |
| Tract v0.1.1 | Timestamps missing timezone suffix; Engram rejects with parse error | Upgrade to v0.1.2+ |
| Tract v0.1.2 | Payloads double-encoded as JSON strings | Upgrade to v0.1.4+ |
| Tract v0.1.3 | Push sends correct JSON but pull parser rejects it (regression) | Upgrade to v0.1.4+ |

## Protocol Details

### Schema Version Negotiation

```
Client push request:
  POST /api/v1/stores/{store_id}/sync/push
  { "schema_version": 2, "push_id": "...", "source_id": "...", "entries": [...] }

Server checks:
  if client.schema_version > server.schema_version → 409 Conflict
  if client.schema_version <= server.schema_version → 200 OK
```

Clients MUST NOT downgrade their schema version to bypass this check. If a client receives 409, the Engram server must be upgraded before syncing.

### Payload Format

Sync payloads in `change_log.payload` MUST be raw JSON objects, not stringified JSON. Engram v1.3.0+ rejects double-encoded payloads with a descriptive error.

**Correct:**
```json
{ "entries": [{ "payload": {"id": "...", "content": "..."} }] }
```

**Incorrect (rejected):**
```json
{ "entries": [{ "payload": "{\"id\": \"...\", \"content\": \"...\"}" }] }
```

### Store Type Isolation

Each store has a type (`recall` or `tract`) set at creation time. The sync protocol enforces store-level isolation:

- Recall stores accept `lore_entries` table operations only
- Tract stores use the generic plugin (no server-side table validation)
- Sequence numbers are independent per store
- Concurrent sync operations across different stores do not interfere

## Upgrade Path

### Upgrading Engram (server)

1. Upgrade Engram to v1.3.0+
2. Existing stores continue to work (backward-compatible schema migration)
3. Legacy lore ingest (`POST /stores/{id}/lore`) continues to work and writes to `change_log`
4. New sync endpoints become available immediately

### Upgrading Recall (client)

1. Upgrade Recall to v1.3.1+
2. First sync: use `sync bootstrap` to download full snapshot, OR `sync delta` to incrementally catch up
3. Subsequent syncs: `sync push` and `sync delta` for incremental sync

### Upgrading Tract (client)

1. Upgrade Tract to v0.1.4+
2. Initialize local store: `tract init <store-name>`
3. First sync: `sync bootstrap` for full snapshot, OR `sync push` to upload local data
4. Subsequent syncs: `sync push` and `sync pull` for incremental sync

## E2E Test Coverage

The compatibility guarantees above are validated by 64 automated E2E tests across 4 layers:

| Layer | Tests | Scope |
|-------|-------|-------|
| L1: In-process | 20 | Sync endpoints, idempotency, schema validation, plugins, snapshots |
| L2: Single client | 16 | Recall (10) and Tract (6) full round-trip with real binaries |
| L3: Multi-client | 13 | Two/three client convergence, Recall+Tract isolation, legacy interop |
| L4: Resilience | 15 | Retry, restart recovery, concurrency, high volume, bootstrap edge cases |

Run the full suite:
```bash
ENGRAM_BIN=./dist/engram \
RECALL_BIN=./bin/e2e/recall \
TRACT_BIN=./bin/e2e/tract \
go test -v -tags=e2e ./test/e2e/ -timeout 300s
```

---

*Last updated: 2026-02-19*
