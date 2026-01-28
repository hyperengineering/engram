-- +goose Up
-- +goose StatementBegin
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

CREATE INDEX idx_lore_entries_category ON lore_entries(category);
CREATE INDEX idx_lore_entries_embedding_status ON lore_entries(embedding_status);
CREATE INDEX idx_lore_entries_updated_at ON lore_entries(updated_at);
CREATE INDEX idx_lore_entries_deleted_at ON lore_entries(deleted_at);
CREATE INDEX idx_lore_entries_last_validated_at ON lore_entries(last_validated_at);

CREATE TABLE store_metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO store_metadata (key, value) VALUES ('schema_version', '1');
INSERT INTO store_metadata (key, value) VALUES ('embedding_model', 'text-embedding-3-small');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS store_metadata;
DROP INDEX IF EXISTS idx_lore_entries_last_validated_at;
DROP INDEX IF EXISTS idx_lore_entries_deleted_at;
DROP INDEX IF EXISTS idx_lore_entries_updated_at;
DROP INDEX IF EXISTS idx_lore_entries_embedding_status;
DROP INDEX IF EXISTS idx_lore_entries_category;
DROP TABLE IF EXISTS lore_entries;
-- +goose StatementEnd
