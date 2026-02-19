package plugin

import (
	"context"

	"github.com/hyperengineering/engram/internal/sync"
)

// TableSchema declares the structure of a domain table for replay operations.
// The replay layer uses this to build parameterized SQL at runtime.
type TableSchema struct {
	// Name is the SQL table name (must match migration CREATE TABLE).
	Name string

	// Columns lists the column names in the order they appear in the table.
	// Must include "id" as the primary key column.
	// The replay layer uses these to build INSERT and UPDATE SQL.
	Columns []string

	// SoftDelete indicates whether DeleteRow should SET deleted_at
	// (soft delete) or issue a real DELETE FROM (hard delete).
	SoftDelete bool
}

// DomainPlugin provides type-specific behavior for a store.
// Each store type (recall, tract, generic) has a corresponding plugin
// that handles validation, migrations, and replay side effects.
type DomainPlugin interface {
	// Type returns the store type this plugin handles (e.g., "recall", "tract").
	Type() string

	// Migrations returns SQL migrations for domain-specific tables.
	// These are applied in addition to the base Engram migrations.
	// Return nil for plugins that use only the base schema.
	Migrations() []Migration

	// ValidatePush validates and optionally reorders change log entries
	// before they are applied. Returns entries in the order they should
	// be replayed. Returns an error if any entry is invalid.
	//
	// Implementations should:
	// - Validate required fields per table
	// - Check referential integrity where applicable
	// - Reorder entries for FK compliance (e.g., parents before children)
	ValidatePush(ctx context.Context, entries []sync.ChangeLogEntry) ([]sync.ChangeLogEntry, error)

	// OnReplay is called after entries are replayed into domain tables.
	// Plugins can trigger side effects such as embedding generation,
	// index updates, or external notifications.
	//
	// The store parameter provides access to replay-specific operations.
	// Errors are logged but do not fail the sync operation.
	OnReplay(ctx context.Context, store ReplayStore, entries []sync.ChangeLogEntry) error

	// TableSchemas returns the schemas of domain tables this plugin manages.
	// Used by the replay layer to build parameterized SQL for UpsertRow/DeleteRow.
	// Return nil for plugins that use only the base schema's lore_entries table.
	TableSchemas() []TableSchema
}

// Migration represents a domain-specific SQL migration.
type Migration struct {
	// Version is the migration sequence number.
	// Must be unique within the plugin and greater than base migrations.
	Version int

	// Name is a human-readable identifier (e.g., "add_goals_table").
	Name string

	// UpSQL is the SQL to apply the migration.
	UpSQL string

	// DownSQL is the SQL to rollback the migration.
	DownSQL string
}

// ReplayStore provides the interface plugins use during OnReplay.
// This is a subset of Store focused on replay-specific operations.
type ReplayStore interface {
	// UpsertRow inserts or updates a row in the specified table.
	// The payload is JSON-decoded and applied to the table.
	UpsertRow(ctx context.Context, tableName string, entityID string, payload []byte) error

	// DeleteRow soft-deletes or hard-deletes a row from the specified table.
	DeleteRow(ctx context.Context, tableName string, entityID string) error

	// QueueEmbedding marks an entry for embedding generation.
	// This is Recall-specific but exposed for the Recall plugin.
	QueueEmbedding(ctx context.Context, entryID string) error
}
