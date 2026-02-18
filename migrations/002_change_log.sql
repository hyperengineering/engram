-- +goose Up
-- +goose StatementBegin

-- Change log for sync protocol
-- Stores all mutations for delta sync and audit trail
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

-- Index for delta queries: SELECT * FROM change_log WHERE sequence > ?
CREATE INDEX idx_change_log_sequence ON change_log (sequence);

-- Index for compaction: find entries by entity for deduplication
CREATE INDEX idx_change_log_entity ON change_log (table_name, entity_id);

-- Push idempotency cache
-- Prevents duplicate processing of retried push requests
CREATE TABLE push_idempotency (
    push_id     TEXT    PRIMARY KEY,
    store_id    TEXT    NOT NULL,
    response    TEXT    NOT NULL,
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT    NOT NULL
);

-- Index for TTL cleanup: DELETE FROM push_idempotency WHERE expires_at < ?
CREATE INDEX idx_push_idempotency_expires ON push_idempotency (expires_at);

-- Sync protocol metadata
-- Tracks sync state separate from store configuration
CREATE TABLE sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Initialize sync metadata with defaults
INSERT INTO sync_meta (key, value) VALUES ('schema_version', '2');
INSERT INTO sync_meta (key, value) VALUES ('last_compaction_seq', '0');
INSERT INTO sync_meta (key, value) VALUES ('last_compaction_at', '');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_push_idempotency_expires;
DROP TABLE IF EXISTS push_idempotency;
DROP INDEX IF EXISTS idx_change_log_entity;
DROP INDEX IF EXISTS idx_change_log_sequence;
DROP TABLE IF EXISTS change_log;
DROP TABLE IF EXISTS sync_meta;
-- +goose StatementEnd
