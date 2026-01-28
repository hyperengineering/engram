package store

import (
	"database/sql"
	"fmt"

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
