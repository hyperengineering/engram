package tract

import (
	"context"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// Plugin implements the DomainPlugin interface for Tract stores.
// Tract manages a goal decomposition hierarchy used by AI agents.
type Plugin struct{}

// New creates a new Tract plugin.
func New() *Plugin {
	return &Plugin{}
}

// Type returns "tract".
func (p *Plugin) Type() string {
	return "tract"
}

// Migrations returns domain-specific migrations for the Tract tables.
func (p *Plugin) Migrations() []plugin.Migration {
	return []plugin.Migration{
		{
			Version: 100,
			Name:    "create_tract_tables",
			UpSQL: `
CREATE TABLE IF NOT EXISTS goals (
    id              TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'active',
    priority        INTEGER NOT NULL DEFAULT 0,
    parent_goal_id  TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT,
    FOREIGN KEY (parent_goal_id) REFERENCES goals(id)
);

CREATE INDEX IF NOT EXISTS idx_goals_status ON goals(status);
CREATE INDEX IF NOT EXISTS idx_goals_parent ON goals(parent_goal_id);
CREATE INDEX IF NOT EXISTS idx_goals_deleted_at ON goals(deleted_at);

CREATE TABLE IF NOT EXISTS csfs (
    id          TEXT PRIMARY KEY,
    goal_id     TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT,
    metric      TEXT,
    target_value TEXT,
    current_value TEXT,
    status      TEXT NOT NULL DEFAULT 'tracking',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    deleted_at  TEXT,
    FOREIGN KEY (goal_id) REFERENCES goals(id)
);

CREATE INDEX IF NOT EXISTS idx_csfs_goal ON csfs(goal_id);
CREATE INDEX IF NOT EXISTS idx_csfs_status ON csfs(status);
CREATE INDEX IF NOT EXISTS idx_csfs_deleted_at ON csfs(deleted_at);

CREATE TABLE IF NOT EXISTS fwus (
    id              TEXT PRIMARY KEY,
    csf_id          TEXT NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT,
    priority        INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'planned',
    estimated_effort TEXT,
    actual_effort   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT,
    FOREIGN KEY (csf_id) REFERENCES csfs(id)
);

CREATE INDEX IF NOT EXISTS idx_fwus_csf ON fwus(csf_id);
CREATE INDEX IF NOT EXISTS idx_fwus_status ON fwus(status);
CREATE INDEX IF NOT EXISTS idx_fwus_deleted_at ON fwus(deleted_at);

CREATE TABLE IF NOT EXISTS implementation_contexts (
    id           TEXT PRIMARY KEY,
    fwu_id       TEXT NOT NULL,
    context_type TEXT,
    content      TEXT,
    metadata     TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    deleted_at   TEXT,
    FOREIGN KEY (fwu_id) REFERENCES fwus(id)
);

CREATE INDEX IF NOT EXISTS idx_ic_fwu ON implementation_contexts(fwu_id);
CREATE INDEX IF NOT EXISTS idx_ic_deleted_at ON implementation_contexts(deleted_at);
`,
			DownSQL: `
DROP TABLE IF EXISTS implementation_contexts;
DROP TABLE IF EXISTS fwus;
DROP TABLE IF EXISTS csfs;
DROP TABLE IF EXISTS goals;
`,
		},
	}
}

// TableSchemas returns the schemas for all four Tract tables.
func (p *Plugin) TableSchemas() []plugin.TableSchema {
	return []plugin.TableSchema{
		{
			Name: "goals",
			Columns: []string{
				"id", "title", "description", "status", "priority",
				"parent_goal_id", "created_at", "updated_at", "deleted_at",
			},
			SoftDelete: true,
		},
		{
			Name: "csfs",
			Columns: []string{
				"id", "goal_id", "title", "description", "metric",
				"target_value", "current_value", "status",
				"created_at", "updated_at", "deleted_at",
			},
			SoftDelete: true,
		},
		{
			Name: "fwus",
			Columns: []string{
				"id", "csf_id", "title", "description", "priority",
				"status", "estimated_effort", "actual_effort",
				"created_at", "updated_at", "deleted_at",
			},
			SoftDelete: true,
		},
		{
			Name: "implementation_contexts",
			Columns: []string{
				"id", "fwu_id", "context_type", "content", "metadata",
				"created_at", "updated_at", "deleted_at",
			},
			SoftDelete: true,
		},
	}
}

// ValidatePush validates and reorders change log entries for FK-safe replay.
// The Tract plugin accepts any table name (validated against a safe regex)
// because the Tract CLI schema evolves independently of the server.
// Unknown tables are stored in the change_log but not replayed to domain tables.
func (p *Plugin) ValidatePush(ctx context.Context, entries []sync.ChangeLogEntry) ([]sync.ChangeLogEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}

	var validationErrors []plugin.ValidationError

	for _, entry := range entries {
		// 1. Table name format check (prevent SQL injection)
		if !tableNameRegex.MatchString(entry.TableName) {
			validationErrors = append(validationErrors, plugin.ValidationError{
				Sequence:  entry.Sequence,
				TableName: entry.TableName,
				EntityID:  entry.EntityID,
				Message:   "invalid table name \"" + entry.TableName + "\"",
			})
			continue
		}

		// 2. Payload validation (upserts only)
		if entry.Operation == sync.OperationUpsert {
			if errs := validatePayload(entry); len(errs) > 0 {
				validationErrors = append(validationErrors, errs...)
			}
		}
	}

	if len(validationErrors) > 0 {
		return nil, plugin.ValidationErrors{Errors: validationErrors}
	}

	// 3. FK-safe topological reorder
	ordered := reorderForFK(entries)
	return ordered, nil
}

// OnReplay dispatches change log entries to the appropriate domain tables.
func (p *Plugin) OnReplay(ctx context.Context, store plugin.ReplayStore, entries []sync.ChangeLogEntry) error {
	return onReplay(ctx, store, entries)
}

// Ensure Plugin implements DomainPlugin at compile time.
var _ plugin.DomainPlugin = (*Plugin)(nil)
