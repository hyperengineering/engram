package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/migrations"
	"github.com/pressly/goose/v3"
)

// RunMigrations applies all pending database migrations using goose.
// It uses the embedded SQL files from the migrations package.
func RunMigrations(db *sql.DB) error {
	// Disable goose's default logging to avoid stdout noise
	goose.SetLogger(goose.NopLogger())

	// Set the embedded filesystem containing migration files
	goose.SetBaseFS(migrations.FS)

	// Set SQLite dialect
	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	// Apply all pending migrations
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// RunPluginMigrations applies domain-specific migrations from a plugin.
// These are applied after the base goose migrations.
// Uses a simple migration tracking table (plugin_migrations) to avoid re-applying.
func RunPluginMigrations(db *sql.DB, migrations []plugin.Migration) error {
	if len(migrations) == 0 {
		return nil
	}

	// Ensure tracking table exists
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS plugin_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create plugin_migrations table: %w", err)
	}

	for _, m := range migrations {
		// Check if already applied
		var count int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM plugin_migrations WHERE version = ?", m.Version,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if count > 0 {
			continue // Already applied
		}

		// Apply migration
		if _, err := db.Exec(m.UpSQL); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}

		// Record application
		now := time.Now().UTC().Format(time.RFC3339)
		_, err = db.Exec(
			"INSERT INTO plugin_migrations (version, name, applied_at) VALUES (?, ?, ?)",
			m.Version, m.Name, now,
		)
		if err != nil {
			return fmt.Errorf("record migration %d (%s): %w", m.Version, m.Name, err)
		}
	}

	return nil
}
