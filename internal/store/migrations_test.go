//go:build integration

package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRunMigrations_FreshDatabase(t *testing.T) {
	// Given: A fresh database with no tables
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// When: RunMigrations is called
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Then: The lore_entries table exists with all required columns
	var tableName string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='lore_entries'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("lore_entries table not created: %v", err)
	}

	// Verify all required columns exist by attempting to query them
	_, err = db.Exec(`
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries LIMIT 0
	`)
	if err != nil {
		t.Fatalf("lore_entries missing required columns: %v", err)
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	// Given: A database that has already been migrated
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// When: RunMigrations is called again
	err = RunMigrations(db)

	// Then: No error occurs (idempotent)
	if err != nil {
		t.Fatalf("second migration should be idempotent, got error: %v", err)
	}
}

func TestRunMigrations_PreservesData(t *testing.T) {
	// Given: A database with existing data
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("initial migration failed: %v", err)
	}

	// Insert test data
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO lore_entries (id, content, category, confidence, source_id, sources, created_at, updated_at)
		VALUES ('test-id-123', 'Test content', 'pattern_outcome', 0.5, 'source-1', '[]', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// When: RunMigrations is called again
	if err := RunMigrations(db); err != nil {
		t.Fatalf("re-migration failed: %v", err)
	}

	// Then: Existing data is preserved
	var content string
	err = db.QueryRow(`SELECT content FROM lore_entries WHERE id = 'test-id-123'`).Scan(&content)
	if err != nil {
		t.Fatalf("data not preserved after migration: %v", err)
	}
	if content != "Test content" {
		t.Errorf("expected content 'Test content', got %q", content)
	}
}

func TestSchema_Indexes(t *testing.T) {
	// Given: A migrated database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Then: All required indexes exist
	expectedIndexes := []string{
		"idx_lore_entries_category",
		"idx_lore_entries_embedding_status",
		"idx_lore_entries_updated_at",
		"idx_lore_entries_deleted_at",
		"idx_lore_entries_last_validated_at",
	}

	for _, idx := range expectedIndexes {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name)
		if err != nil {
			t.Errorf("index %s not found: %v", idx, err)
		}
	}
}

func TestSchema_StoreMetadata(t *testing.T) {
	// Given: A migrated database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Then: store_metadata table exists with seed data
	var schemaVersion, embeddingModel string
	err = db.QueryRow(`SELECT value FROM store_metadata WHERE key = 'schema_version'`).Scan(&schemaVersion)
	if err != nil {
		t.Fatalf("schema_version not found in store_metadata: %v", err)
	}
	if schemaVersion != "1" {
		t.Errorf("expected schema_version '1', got %q", schemaVersion)
	}

	err = db.QueryRow(`SELECT value FROM store_metadata WHERE key = 'embedding_model'`).Scan(&embeddingModel)
	if err != nil {
		t.Fatalf("embedding_model not found in store_metadata: %v", err)
	}
	if embeddingModel != "text-embedding-3-small" {
		t.Errorf("expected embedding_model 'text-embedding-3-small', got %q", embeddingModel)
	}
}

func TestWALMode_Enabled(t *testing.T) {
	// Given: A new SQLiteStore
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// When: We check the journal mode
	// Then: WAL mode is enabled
	var journalMode string
	err = store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode 'wal', got %q", journalMode)
	}
}

func TestPragmas_Applied(t *testing.T) {
	// Given: A new SQLiteStore
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Then: busy_timeout is set to 5000
	var busyTimeout int
	err = store.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("failed to query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("expected busy_timeout 5000, got %d", busyTimeout)
	}

	// Then: foreign_keys is enabled
	var foreignKeys int
	err = store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("failed to query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys 1, got %d", foreignKeys)
	}

	// Then: synchronous is NORMAL (1)
	var synchronous int
	err = store.db.QueryRow("PRAGMA synchronous").Scan(&synchronous)
	if err != nil {
		t.Fatalf("failed to query synchronous: %v", err)
	}
	if synchronous != 1 {
		t.Errorf("expected synchronous 1 (NORMAL), got %d", synchronous)
	}
}

func TestNewSQLiteStore_CreatesParentDirectories(t *testing.T) {
	// Given: A path with non-existent parent directories
	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "test.db")

	// When: NewSQLiteStore is called
	store, err := NewSQLiteStore(dbPath)

	// Then: Store is created successfully (SQLite creates the file, dirs should exist or be handled)
	if err != nil {
		t.Fatalf("failed to create store with nested path: %v", err)
	}
	defer store.Close()

	// Verify the file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestSchema_DefaultValues(t *testing.T) {
	// Given: A migrated database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// When: Inserting with minimal required fields
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO lore_entries (id, content, category, source_id, created_at, updated_at)
		VALUES ('test-defaults', 'content', 'pattern_outcome', 'src-1', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert with minimal fields: %v", err)
	}

	// Then: Default values are applied correctly
	var confidence float64
	var embeddingStatus, sources string
	var validationCount int
	err = db.QueryRow(`
		SELECT confidence, embedding_status, sources, validation_count
		FROM lore_entries WHERE id = 'test-defaults'
	`).Scan(&confidence, &embeddingStatus, &sources, &validationCount)
	if err != nil {
		t.Fatalf("failed to query defaults: %v", err)
	}

	if confidence != 0.5 {
		t.Errorf("expected default confidence 0.5, got %f", confidence)
	}
	if embeddingStatus != "complete" {
		t.Errorf("expected default embedding_status 'complete', got %q", embeddingStatus)
	}
	if sources != "[]" {
		t.Errorf("expected default sources '[]', got %q", sources)
	}
	if validationCount != 0 {
		t.Errorf("expected default validation_count 0, got %d", validationCount)
	}
}
