# Snapshot Schema Contract

This document defines the contract between Engram snapshots and client applications (e.g., Tract/Recall) that consume them.

## What Engram Snapshots Contain

Engram snapshots are complete SQLite databases created via `VACUUM INTO`. They contain the exact schema and data from the Engram store at the time of snapshot creation.

### Tables Included

| Table | Description |
|-------|-------------|
| `lore_entries` | Primary data table containing lore records |
| `store_metadata` | Store configuration (type, schema version) |
| `change_log` | Sync change tracking entries |
| `push_idempotency` | Deduplication keys for push operations |
| `sync_meta` | Sync state metadata (schema version, last sync) |

### `lore_entries` Schema

```sql
CREATE TABLE lore_entries (
    id                TEXT PRIMARY KEY,
    content           TEXT NOT NULL,
    context           TEXT,
    category          TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0.5,
    embedding         BLOB,
    embedding_status  TEXT NOT NULL DEFAULT 'complete',
    source_id         TEXT NOT NULL,
    sources           TEXT NOT NULL DEFAULT '[]',
    validation_count  INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    deleted_at        TEXT,
    last_validated_at TEXT
);
```

### Indexes

The snapshot includes all indexes defined by Engram:
- `idx_lore_entries_category`
- `idx_lore_entries_source_id`
- `idx_lore_entries_confidence`
- `idx_lore_entries_created_at`
- `idx_lore_entries_deleted_at`

## What Clients Must Add

Client applications may need local-only columns that are NOT included in Engram snapshots. These columns track client-specific state that has no meaning to the central Engram service.

### Common Client-Local Columns

| Column | Type | Purpose |
|--------|------|---------|
| `synced_at` | `TEXT` | Tracks when a row was last synced to Engram |

### Adding Local Columns After Bootstrap

After loading a snapshot, clients must add their local columns:

```sql
-- Add client-local tracking column
ALTER TABLE lore_entries ADD COLUMN synced_at TEXT;
```

**Important:** SQLite's `ALTER TABLE ADD COLUMN` appends columns without affecting existing data. This is safe to run after snapshot load.

## Bootstrap Workflow

Clients should follow this workflow when bootstrapping from an Engram snapshot:

### 1. Download Snapshot

```http
GET /api/v1/sync/snapshot
```

Save the response body to a local file.

### 2. Open Snapshot Database

```go
db, err := sql.Open("sqlite3", snapshotPath)
```

### 3. Verify Schema Version

```sql
SELECT value FROM sync_meta WHERE key = 'schema_version';
```

Compare against expected version. If mismatched, handle migration or reject.

### 4. Add Client-Local Columns

```sql
ALTER TABLE lore_entries ADD COLUMN synced_at TEXT;
```

### 5. Initialize Local State

```sql
-- Mark all existing rows as synced (they came from Engram)
UPDATE lore_entries SET synced_at = datetime('now');
```

### 6. Begin Normal Operations

The client is now ready to:
- Query local data
- Record new lore entries locally
- Push changes to Engram via `/api/v1/sync/push`

## Schema Evolution

### Version Tracking

Schema version is stored in the `sync_meta` table:

```sql
SELECT value FROM sync_meta WHERE key = 'schema_version';
```

### Version Compatibility

| Scenario | Behavior |
|----------|----------|
| Client version < Snapshot version | Client should migrate or reject |
| Client version = Snapshot version | Compatible, proceed normally |
| Client version > Snapshot version | Snapshot is older; may need migration |

### Migration Strategy

When Engram's schema evolves:

1. **Engram updates schema version** in migrations
2. **Snapshots contain new schema** automatically (via `VACUUM INTO`)
3. **Clients detect version mismatch** during bootstrap
4. **Clients apply local migrations** or request updated client version

### Push Validation

The sync push endpoint validates schema version in the request header. Clients with incompatible schemas will receive a `409 Conflict` error.

## Key Principles

1. **Engram owns the schema.** Clients adapt to Engram's schema, not vice versa.

2. **Snapshots are complete.** No transformation or filtering is applied during snapshot creation.

3. **Client columns are local.** Any column not in Engram's schema must be added by the client after bootstrap.

4. **Version compatibility is enforced.** Push operations validate schema version to prevent data corruption.

## Related Documentation

- [ADR-001: Engram-Recall Schema Contract](_bmad-output/adrs/001-engram-recall-schema-contract.md)
- [ADR-003: Snapshot Schema Boundary](_bmad-output/adrs/003-snapshot-schema-boundary.md)
- [Sync Integration Guide](recall-client-sync-integration.md)
