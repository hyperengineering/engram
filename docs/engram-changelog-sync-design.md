# Engram Universal Sync: Change Log Protocol

**Status**: Draft
**Author**: NeuralMux Engineering
**Scope**: Engram, Recall, Tract

---

## 1. Problem Statement

NeuralMux tools (Recall, Tract, and future tools) each maintain local SQLite databases that operate fully offline. Today, only Recall can sync with Engram, the central data store, using a domain-specific protocol tightly coupled to the `lore_entries` table.

Tract needs the same sync capability, but its data model is fundamentally different: 21 relational tables with foreign key constraints versus Recall's single flat table. A second domain-specific sync protocol would double the maintenance surface and prevent future tools from reusing the infrastructure.

**Goal**: Evolve Engram from "Recall's sync backend" into "a central sync service for all NeuralMux tools" by introducing a generic, change-log-based sync protocol.

---

## 2. Current Architecture

### 2.1 Recall Sync (Today)

```
┌─────────────┐         ┌─────────────┐
│  Recall CLI  │◄───────►│   Engram     │
│  + MCP       │  HTTP   │   Server     │
│              │         │              │
│ lore.db      │         │ engram.db    │
│ ├ lore_entries│        │ ├ lore_entries│
│ ├ sync_queue │         │ ├ embeddings │
│ └ sync_meta  │         │ └ meta.yaml  │
└─────────────┘         └─────────────┘
```

**Outbox pattern**: `InsertLore` writes to both `lore_entries` and `sync_queue` in a single transaction.

**Three sync modes**:
- **Bootstrap**: `GET /snapshot` — downloads full SQLite via `VACUUM INTO`, atomic file replacement
- **Push**: Drain `sync_queue` → `POST /ingest` → delete queue entries + set `synced_at`
- **Delta**: `GET /delta?since={ts}` → upserts/deletes → update `last_sync`

**Limitations**:
- Engram only understands `lore_entries` — every route, handler, and migration is lore-specific
- `meta.yaml` has no concept of store type
- Delta uses timestamps (`updated_at > ?`), which are fragile across clock skew

### 2.2 Tract Local Store (Today)

```
~/.tract/stores/{name}/tract.db
├── Reasoning layer (8 tables): goals, csfs, ncs, sos, so_ncs, capabilities, epics, features
├── Planning layer (6 tables): fwus, fwu_boundaries, fwu_dependencies, fwu_design_decisions,
│                               fwu_interface_contracts, fwu_verification_gates
└── Execution layer (7 tables): implementation_contexts, entity_specs, design_decisions,
                                 test_seeds, agent_records, file_actions, followups
```

- GORM + goose migrations, WAL mode, FK enforcement, `MaxOpenConns=1`
- Store resolution: `--store` flag > `TRACT_STORE` env > auto-detect
- No sync capability

---

## 3. Proposed Architecture

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  Recall CLI   │    │  Tract CLI    │    │  Future Tool  │
│  + MCP        │    │  + MCP        │    │  + MCP        │
│               │    │               │    │               │
│ lore.db       │    │ tract.db      │    │ tool.db       │
│ ├ lore_entries│    │ ├ goals       │    │ ├ ...         │
│ ├ change_log  │    │ ├ fwus        │    │ ├ change_log  │
│ └ sync_meta   │    │ ├ impl_ctx   │    │ └ sync_meta   │
└───────┬───────┘    │ ├ change_log  │    └───────┬───────┘
        │            │ └ sync_meta   │            │
        │            └───────┬───────┘            │
        │                    │                    │
        ▼                    ▼                    ▼
┌─────────────────────────────────────────────────────────┐
│                    Engram Server                         │
│                                                         │
│  Generic Layer                                          │
│  ├ POST   /api/v1/stores/{id}/sync/push                │
│  ├ GET    /api/v1/stores/{id}/sync/delta?since={seq}   │
│  ├ GET    /api/v1/stores/{id}/sync/snapshot             │
│  ├ POST   /api/v1/stores                                │
│  └ GET    /api/v1/stores                                │
│                                                         │
│  Domain Plugins (registered per store type)             │
│  ├ recall: embeddings, decay, dedup, similarity search  │
│  ├ tract:  FK validation, entity queries                │
│  └ ...:   future tool-specific logic                    │
│                                                         │
│  ~/.engram/stores/{id}/                                 │
│  ├ store.db      (domain tables + change_log)           │
│  └ meta.yaml     (type: recall|tract, created, ...)     │
└─────────────────────────────────────────────────────────┘
```

### 3.1 Design Principles

1. **Sync is generic** — the change log protocol knows nothing about domain schemas. Any tool that writes a change log can sync.
2. **Domain logic is optional** — Engram plugins provide value-add features (embeddings for Recall, FK validation for Tract) but are not required for sync to work.
3. **Sequence-based, not timestamp-based** — monotonic sequences eliminate clock skew issues.
4. **Offline-first** — all tools work fully without Engram. Sync is opportunistic.
5. **Backward compatible** — existing Recall `/lore/...` routes continue working. Migration is opt-in.

---

## 4. Change Log Protocol

### 4.1 Local Change Log Table

Every participating tool adds this table to its local SQLite database:

```sql
CREATE TABLE change_log (
    sequence    INTEGER PRIMARY KEY AUTOINCREMENT,
    table_name  TEXT    NOT NULL,
    entity_id   TEXT    NOT NULL,
    operation   TEXT    NOT NULL CHECK (operation IN ('upsert', 'delete')),
    payload     TEXT,           -- JSON-encoded row for upserts, NULL for deletes
    source_id   TEXT    NOT NULL,
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    synced_at   TEXT             -- NULL until pushed, set on successful push
);

CREATE INDEX idx_change_log_unsynced ON change_log (synced_at) WHERE synced_at IS NULL;
CREATE INDEX idx_change_log_sequence ON change_log (sequence);
```

**Fields**:

| Field | Description |
|-------|-------------|
| `sequence` | Monotonically increasing local sequence number |
| `table_name` | The source table (e.g., `lore_entries`, `goals`, `fwus`) |
| `entity_id` | Primary key of the affected row |
| `operation` | `upsert` (insert or update) or `delete` |
| `payload` | Full row as JSON for upserts. NULL for deletes. |
| `source_id` | Unique identifier for this sync participant (machine/instance) |
| `created_at` | Local timestamp of the change |
| `synced_at` | Set after successful push to Engram |

### 4.2 Local Sync Metadata Table

```sql
CREATE TABLE sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Populated entries:
-- ('source_id',       '<uuid>')           -- unique per client instance
-- ('last_push_seq',   '0')                -- last sequence successfully pushed
-- ('last_pull_seq',   '0')                -- last remote sequence pulled
-- ('engram_store_id', '<store-id>')       -- remote store identifier
```

### 4.3 Write Path (Local)

Every mutation to a synced table appends to the change log within the same transaction:

```go
func (s *Store) CreateGoal(goal *Goal) error {
    return s.db.Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(goal).Error; err != nil {
            return err
        }
        return tx.Create(&ChangeLogEntry{
            TableName: "goals",
            EntityID:  goal.ID,
            Operation: "upsert",
            Payload:   marshalJSON(goal),
            SourceID:  s.sourceID,
        }).Error
    })
}
```

This is the same outbox pattern Recall already uses with `sync_queue`, but generalized to any table.

### 4.4 Push (Client → Engram)

```
POST /api/v1/stores/{store_id}/sync/push
Authorization: Bearer <token>

{
  "source_id": "client-uuid",
  "entries": [
    {
      "sequence": 42,
      "table_name": "goals",
      "entity_id": "G-01",
      "operation": "upsert",
      "payload": {"id": "G-01", "name": "Platform reliability", ...},
      "created_at": "2026-02-17T10:30:00.000"
    },
    ...
  ]
}
```

**Response**:

```json
{
  "accepted": 15,
  "remote_sequence": 1042,
  "errors": []
}
```

**Engram processing**:
1. Validate entries (well-formed JSON, required fields present)
2. For typed stores, invoke domain plugin validation (e.g., Tract plugin checks FK ordering)
3. Replay entries into domain tables: upserts → INSERT OR REPLACE, deletes → DELETE
4. Append entries to the server-side `change_log` with new server sequence numbers
5. Return the highest server sequence number assigned

**Client post-push**:
1. Mark pushed entries: `UPDATE change_log SET synced_at = ? WHERE sequence <= ?`
2. Update `sync_meta` → `last_push_seq`

### 4.5 Delta Pull (Engram → Client)

```
GET /api/v1/stores/{store_id}/sync/delta?since=1000&limit=500
Authorization: Bearer <token>
```

**Response**:

```json
{
  "entries": [
    {
      "sequence": 1001,
      "table_name": "goals",
      "entity_id": "G-01",
      "operation": "upsert",
      "payload": {"id": "G-01", "name": "Platform reliability", ...},
      "source_id": "other-client-uuid",
      "created_at": "2026-02-17T11:00:00.000"
    },
    ...
  ],
  "has_more": false,
  "latest_sequence": 1042
}
```

**Client processing**:
1. Filter out entries where `source_id` matches the client's own `source_id` (already applied locally)
2. Replay entries into local domain tables within a transaction
3. Append entries to local `change_log` with `synced_at` pre-set (already from server)
4. Update `sync_meta` → `last_pull_seq`

### 4.6 Bootstrap (Full Snapshot)

For initial sync or recovery:

```
GET /api/v1/stores/{store_id}/sync/snapshot
Authorization: Bearer <token>
```

Returns the full SQLite database file (`application/octet-stream`) produced by `VACUUM INTO`.

**Client processing**:
1. Download to a temp file
2. Verify integrity (SQLite `PRAGMA integrity_check`)
3. Atomic rename to replace local database
4. Re-open connections
5. Set `sync_meta` → `last_pull_seq` to the server's latest sequence

This reuses Engram's existing snapshot infrastructure.

### 4.7 FK Ordering (Tract-Specific)

Tract's relational schema requires entries to be replayed in dependency order. The change log protocol itself is ordering-agnostic, but the **Tract domain plugin** enforces topological ordering during push validation and delta generation.

**Insert/upsert order** (parents first, by dependency depth):
```
Level 0: goals, sos
Level 1: csfs
Level 2: ncs
Level 3: so_ncs, capabilities
Level 4: epics
Level 5: features
Level 6: fwus
Level 7: fwu_boundaries, fwu_dependencies, fwu_design_decisions,
         fwu_interface_contracts, fwu_verification_gates
Level 8: implementation_contexts
Level 9: entity_specs, design_decisions, test_seeds, agent_records, file_actions
Level 10: followups
```

Tables at the same level may be inserted in any order. `followups` depends on `agent_records` (via optional FK), so it must come last.

**Delete order** (children first): reverse of the above (Level 10 → Level 0).

**Implementation**: The Tract plugin assigns each table a dependency depth and sorts entries by that depth before replay. Clients are encouraged to produce entries in order, but the server re-sorts as a safety net.

---

## 5. Engram Server Changes

### 5.1 Typed Stores

Extend `meta.yaml` to include a store type:

```yaml
# ~/.engram/stores/{id}/meta.yaml
type: recall          # recall | tract | generic
created: 2026-02-17T10:00:00Z
last_accessed: 2026-02-17T12:00:00Z
description: "Team knowledge base"
```

Store creation API gains an optional `type` parameter:

```
POST /api/v1/stores
{
  "id": "my-tract-store",
  "type": "tract",
  "description": "Project planning data"
}
```

Default type: `recall` (backward compatible).

### 5.2 Domain Plugin Interface

```go
// DomainPlugin provides type-specific behavior for a store.
type DomainPlugin interface {
    // Type returns the store type this plugin handles.
    Type() string

    // Migrations returns SQL migrations for domain-specific tables.
    Migrations() []Migration

    // ValidatePush validates and optionally reorders change log entries
    // before they are applied. Returns entries in apply order.
    ValidatePush(entries []ChangeLogEntry) ([]ChangeLogEntry, error)

    // OnReplay is called after entries are replayed into domain tables.
    // Plugins can trigger side effects (e.g., embedding generation).
    OnReplay(entries []ChangeLogEntry) error
}
```

**Built-in plugins**:

| Plugin | Behavior |
|--------|----------|
| `recall` | Creates `lore_entries` + `embeddings` tables. On replay, queues embedding generation. Provides similarity search, dedup, confidence decay. |
| `tract` | Creates Tract's 21 domain tables. ValidatePush enforces FK ordering. No additional side effects. |
| `generic` | No domain tables. Change log only. Snapshot/push/delta work, but no domain queries. |

### 5.3 Server-Side Change Log

Each Engram store database gets its own change log:

```sql
CREATE TABLE change_log (
    sequence    INTEGER PRIMARY KEY AUTOINCREMENT,
    table_name  TEXT    NOT NULL,
    entity_id   TEXT    NOT NULL,
    operation   TEXT    NOT NULL CHECK (operation IN ('upsert', 'delete')),
    payload     TEXT,
    source_id   TEXT    NOT NULL,
    created_at  TEXT    NOT NULL,
    received_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);

CREATE INDEX idx_change_log_since ON change_log (sequence);
```

The server assigns its own monotonic sequence numbers, independent of client sequences. This is the canonical ordering used by delta pulls.

### 5.4 New Generic Sync Routes

```
POST   /api/v1/stores/{id}/sync/push      — accept change log entries
GET    /api/v1/stores/{id}/sync/delta      — return entries since sequence
GET    /api/v1/stores/{id}/sync/snapshot   — return full database snapshot
```

These work for any store type. Domain plugins hook into the push/replay lifecycle but don't change the API contract.

### 5.5 Backward Compatibility

Existing Recall-specific routes remain:

```
POST   /api/v1/stores/{id}/lore/ingest     — existing recall push
GET    /api/v1/stores/{id}/lore/delta       — existing recall delta
GET    /api/v1/stores/{id}/lore/snapshot    — existing recall snapshot
POST   /api/v1/stores/{id}/lore/feedback    — recall-specific
GET    /api/v1/stores/{id}/lore/search      — recall-specific (embeddings)
```

These are implemented as thin wrappers: `lore/ingest` translates to change log entries internally. This allows Recall clients to migrate to the generic protocol at their own pace.

---

## 6. Client Changes

### 6.1 Tract Sync Client

New package: `internal/sync/`

```go
type SyncClient struct {
    store      *store.Store
    httpClient *http.Client
    config     SyncConfig
}

type SyncConfig struct {
    EngramURL     string  // ENGRAM_URL env var
    EngramAPIKey  string  // ENGRAM_API_KEY env var
    EngramStoreID string  // ENGRAM_STORE or --store flag
    SourceID      string  // auto-generated UUID, persisted in sync_meta
    SyncInterval  time.Duration // default: 5 minutes
}

func (c *SyncClient) Push(ctx context.Context) error { ... }
func (c *SyncClient) Pull(ctx context.Context) error { ... }
func (c *SyncClient) Bootstrap(ctx context.Context) error { ... }
func (c *SyncClient) Sync(ctx context.Context) error {
    if err := c.Push(ctx); err != nil { return err }
    return c.Pull(ctx)
}
```

### 6.2 Tract CLI Commands

```
tract sync push     [--store <name>]   # push local changes to Engram
tract sync pull     [--store <name>]   # pull remote changes
tract sync status   [--store <name>]   # show sync status (pending entries, last sync)
tract sync bootstrap [--store <name>]  # full snapshot download
```

### 6.3 Tract MCP Tools

```
tract_sync_push      — push changes (for agent-driven sync)
tract_sync_pull      — pull changes
tract_sync_status    — check sync status
```

### 6.4 Recall Migration Path

Recall already has sync working. The migration is:

1. **Phase 1**: Add `change_log` table to Recall's local database alongside `sync_queue`
2. **Phase 2**: Write to both `sync_queue` (old) and `change_log` (new) during transition
3. **Phase 3**: Switch Recall's syncer to use the generic `/sync/push` and `/sync/delta` endpoints
4. **Phase 4**: Remove `sync_queue` table and old endpoint usage
5. **Phase 5**: Engram deprecates `/lore/ingest` and `/lore/delta` (keep for compatibility window)

Each phase is independently deployable. No big-bang migration.

---

## 7. Conflict Resolution

### 7.1 Strategy: Last-Writer-Wins

The change log is an ordered sequence. When two clients modify the same entity concurrently:

1. Both push their changes
2. Engram appends both to its change log in arrival order
3. The last entry for a given `(table_name, entity_id)` wins when replayed
4. Other clients pull the full sequence and arrive at the same state

This is the simplest model and matches Recall's current behavior (dedup merge is additive, not conflicting).

### 7.2 Source Filtering

Clients skip entries from their own `source_id` during pull to avoid re-applying their own changes. This prevents:
- Double-application of locally-originated mutations
- Unnecessary write amplification

### 7.3 Future: CRDT or Custom Merge

The change log format supports richer conflict resolution in the future:
- Domain plugins could implement custom merge logic in `OnReplay`
- The full `payload` is preserved, enabling three-way merge if needed
- `source_id` enables per-client change tracking for causal ordering

These are not needed for V1 — last-writer-wins is sufficient for team-sized deployments.

---

## 8. Security

### 8.1 Authentication

Same as current Engram: single Bearer token via `ENGRAM_API_KEY`. Per-client tokens are a future enhancement.

### 8.2 Store Isolation

Each store has its own database file. No cross-store data leakage. Store IDs are validated on every request.

### 8.3 Payload Validation

- JSON payloads are validated for well-formedness
- Domain plugins validate schema conformance (e.g., Tract plugin checks required fields)
- Maximum payload size: 1MB per entry, 10MB per push batch

---

## 9. Delivery Plan

### Phase 1: Engram Generic Layer
- Add `type` field to `meta.yaml` and store creation API
- Implement `DomainPlugin` interface and plugin registry
- Add `change_log` table to Engram store databases
- Implement `/sync/push`, `/sync/delta`, `/sync/snapshot` routes
- Port existing Recall logic into the `recall` domain plugin
- Existing Recall routes delegate to generic layer internally

### Phase 2: Tract Sync Client
- Add `change_log` and `sync_meta` tables to Tract (new goose migration)
- Implement outbox pattern in Tract's store write methods
- Build `internal/sync/` package with push/pull/bootstrap
- Add `tract sync` CLI commands
- Add `tract_sync_*` MCP tools
- Implement Tract domain plugin for Engram (FK ordering validation)

### Phase 3: Recall Migration
- Add `change_log` to Recall local database
- Dual-write to `sync_queue` + `change_log`
- Switch Recall syncer to generic endpoints
- Remove `sync_queue` after transition period

### Phase 4: Polish
- Background sync (configurable interval, default 5 min)
- Sync status in TUI (Tract) and CLI (both tools)
- Retry with exponential backoff
- Offline queue depth warnings
- Monitoring and observability

---

## 10. Open Questions

1. **Compaction**: The change log grows indefinitely. Should we compact (retain only latest entry per entity) after confirmed sync? At what threshold?
2. **Schema evolution**: When Tract adds new tables via migration, how does the domain plugin handle entries for tables it doesn't know about yet? Likely: store in change_log, skip replay, retry after migration.
3. **Large payloads**: Reasoning chains can be large. Should we support chunked push or compress payloads?
4. **Multi-store sync**: Can a single Engram instance sync multiple Tract stores? Yes (each store is independent), but should we support cross-store references?
5. **Access control**: Per-store tokens? Per-client read/write permissions? Needed for team deployments but not V1.
