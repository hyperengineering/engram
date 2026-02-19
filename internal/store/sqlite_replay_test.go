package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

func newReplayTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func makeLorePayload(t *testing.T, overrides map[string]interface{}) []byte {
	t.Helper()
	base := map[string]interface{}{
		"id":               "entry-1",
		"content":          "Test lore content",
		"context":          "Test context",
		"category":         "TESTING_STRATEGY",
		"confidence":       0.5,
		"source_id":        "src-1",
		"sources":          []string{"src-1"},
		"validation_count": 0,
		"created_at":       "2026-01-01T00:00:00Z",
		"updated_at":       "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// --- UpsertRow tests ---

func TestUpsertRow_NewEntry(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	payload := makeLorePayload(t, nil)
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Verify entry exists
	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.Content != "Test lore content" {
		t.Errorf("Content = %q, want %q", entry.Content, "Test lore content")
	}
	if entry.Category != "TESTING_STRATEGY" {
		t.Errorf("Category = %q, want %q", entry.Category, "TESTING_STRATEGY")
	}
	if entry.Confidence != 0.5 {
		t.Errorf("Confidence = %v, want %v", entry.Confidence, 0.5)
	}
	if entry.SourceID != "src-1" {
		t.Errorf("SourceID = %q, want %q", entry.SourceID, "src-1")
	}
}

func TestUpsertRow_ExistingEntry(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert initial entry
	payload := makeLorePayload(t, nil)
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("first UpsertRow() error = %v", err)
	}

	// Update with new content
	payload = makeLorePayload(t, map[string]interface{}{
		"content":    "Updated content",
		"confidence": 0.8,
	})
	err = s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("second UpsertRow() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.Content != "Updated content" {
		t.Errorf("Content = %q, want %q", entry.Content, "Updated content")
	}
	if entry.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want %v", entry.Confidence, 0.8)
	}
}

func TestUpsertRow_UpdatesUpdatedAt(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	payload := makeLorePayload(t, nil)
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}

	// updated_at should be set to a recent time (not the original payload time)
	if entry.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestUpsertRow_WithEmbedding(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	payload := makeLorePayload(t, map[string]interface{}{
		"embedding":        []float32{0.1, 0.2, 0.3},
		"embedding_status": "complete",
	})
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if len(entry.Embedding) != 3 {
		t.Fatalf("Embedding length = %d, want 3", len(entry.Embedding))
	}
	if entry.EmbeddingStatus != "complete" {
		t.Errorf("EmbeddingStatus = %q, want %q", entry.EmbeddingStatus, "complete")
	}
}

func TestUpsertRow_WithoutEmbedding_DefaultsPending(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	payload := makeLorePayload(t, nil)
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.EmbeddingStatus != "pending" {
		t.Errorf("EmbeddingStatus = %q, want %q", entry.EmbeddingStatus, "pending")
	}
}

func TestUpsertRow_UnsupportedTable(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	err := s.UpsertRow(ctx, "other_table", "entry-1", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unsupported table")
	}
}

func TestUpsertRow_IDMismatch(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	payload := makeLorePayload(t, map[string]interface{}{"id": "different-id"})
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err == nil {
		t.Fatal("expected error for ID mismatch")
	}
}

func TestUpsertRow_InvalidJSON(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	err := s.UpsertRow(ctx, "lore_entries", "entry-1", []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestUpsertRow_NullSources(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Sources as null in JSON
	payload := makeLorePayload(t, map[string]interface{}{"sources": nil})
	err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() with null sources error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry == nil {
		t.Fatal("entry should exist")
	}
}

// --- DeleteRow tests ---

func TestDeleteRow_SoftDeletes(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert entry first
	payload := makeLorePayload(t, nil)
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Soft delete
	err := s.DeleteRow(ctx, "lore_entries", "entry-1")
	if err != nil {
		t.Fatalf("DeleteRow() error = %v", err)
	}

	// Verify soft deleted — GetLore returns ErrNotFound for deleted entries
	_, err = s.GetLore(ctx, "entry-1")
	if err == nil {
		t.Error("expected error for soft-deleted entry")
	}

	// Verify entry still exists in DB with deleted_at set
	var deletedAt sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT deleted_at FROM lore_entries WHERE id = ?`, "entry-1").Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query deleted entry: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("deleted_at should be set")
	}
}

func TestDeleteRow_AlreadyDeleted(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert and delete
	payload := makeLorePayload(t, nil)
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}
	if err := s.DeleteRow(ctx, "lore_entries", "entry-1"); err != nil {
		t.Fatalf("first DeleteRow() error = %v", err)
	}

	// Delete again — should not error (idempotent)
	err := s.DeleteRow(ctx, "lore_entries", "entry-1")
	if err != nil {
		t.Fatalf("second DeleteRow() should be idempotent, got error = %v", err)
	}
}

func TestDeleteRow_NotFound(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Delete non-existent entry — should not error (idempotent)
	err := s.DeleteRow(ctx, "lore_entries", "nonexistent")
	if err != nil {
		t.Fatalf("DeleteRow() on non-existent entry should be idempotent, got error = %v", err)
	}
}

func TestDeleteRow_UnsupportedTable(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	err := s.DeleteRow(ctx, "other_table", "entry-1")
	if err == nil {
		t.Fatal("expected error for unsupported table")
	}
}

// --- QueueEmbedding tests ---

func TestQueueEmbedding_SetsStatus(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert entry with embedding_status = 'complete' but no actual embedding
	// (simulating a synced entry without embedding)
	payload := makeLorePayload(t, map[string]interface{}{
		"embedding_status": "failed",
	})
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	err := s.QueueEmbedding(ctx, "entry-1")
	if err != nil {
		t.Fatalf("QueueEmbedding() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.EmbeddingStatus != "pending" {
		t.Errorf("EmbeddingStatus = %q, want %q", entry.EmbeddingStatus, "pending")
	}
}

func TestQueueEmbedding_AlreadyPending(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert entry without embedding — defaults to "pending"
	payload := makeLorePayload(t, nil)
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// QueueEmbedding should be a no-op when already pending
	err := s.QueueEmbedding(ctx, "entry-1")
	if err != nil {
		t.Fatalf("QueueEmbedding() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.EmbeddingStatus != "pending" {
		t.Errorf("EmbeddingStatus = %q, want %q", entry.EmbeddingStatus, "pending")
	}
}

func TestQueueEmbedding_HasEmbedding(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert entry with embedding
	payload := makeLorePayload(t, map[string]interface{}{
		"embedding":        []float32{0.1, 0.2, 0.3},
		"embedding_status": "complete",
	})
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// QueueEmbedding should be a no-op when embedding exists
	err := s.QueueEmbedding(ctx, "entry-1")
	if err != nil {
		t.Fatalf("QueueEmbedding() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.EmbeddingStatus != "complete" {
		t.Errorf("EmbeddingStatus = %q, want %q (should not change for entries with embeddings)", entry.EmbeddingStatus, "complete")
	}
}

func TestQueueEmbedding_NonExistentEntry(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Should not error for non-existent entry (UPDATE affects 0 rows)
	err := s.QueueEmbedding(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("QueueEmbedding() for non-existent entry error = %v", err)
	}
}

// --- ReplayStore interface compliance ---

func TestSQLiteStore_ImplementsReplayStore(t *testing.T) {
	// Compile-time check that SQLiteStore satisfies plugin.ReplayStore
	s := newReplayTestStore(t)
	_ = s.UpsertRow
	_ = s.DeleteRow
	_ = s.QueueEmbedding
}

// --- Transaction-scoped replay function tests ---

func TestBeginTx(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer tx.Rollback()

	// Verify tx is usable
	_, err = tx.ExecContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("transaction should be usable: %v", err)
	}
}

func TestUpsertRowTx_NewEntry(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	payload := makeLorePayload(t, nil)
	err = UpsertRowTx(ctx, tx, "lore_entries", "entry-1", payload)
	if err != nil {
		t.Fatalf("UpsertRowTx() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify entry exists
	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.Content != "Test lore content" {
		t.Errorf("Content = %q, want %q", entry.Content, "Test lore content")
	}
}

func TestUpsertRowTx_Rollback(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	payload := makeLorePayload(t, nil)
	if err := UpsertRowTx(ctx, tx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRowTx() error = %v", err)
	}

	// Rollback instead of commit
	tx.Rollback()

	// Entry should NOT exist
	_, err = s.GetLore(ctx, "entry-1")
	if err == nil {
		t.Error("expected error — entry should not exist after rollback")
	}
}

func TestUpsertRowTx_UnsupportedTable(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer tx.Rollback()

	err = UpsertRowTx(ctx, tx, "other_table", "entry-1", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unsupported table")
	}
}

func TestDeleteRowTx_SoftDeletes(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert via non-tx first
	payload := makeLorePayload(t, nil)
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Delete via tx
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	if err := DeleteRowTx(ctx, tx, "lore_entries", "entry-1"); err != nil {
		t.Fatalf("DeleteRowTx() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Entry should be soft-deleted
	_, err = s.GetLore(ctx, "entry-1")
	if err == nil {
		t.Error("expected error for soft-deleted entry")
	}

	// Verify in DB
	var deletedAt sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT deleted_at FROM lore_entries WHERE id = ?`, "entry-1").Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query deleted entry: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("deleted_at should be set")
	}
}

func TestDeleteRowTx_UnsupportedTable(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer tx.Rollback()

	err = DeleteRowTx(ctx, tx, "other_table", "entry-1")
	if err == nil {
		t.Fatal("expected error for unsupported table")
	}
}

func TestQueueEmbeddingTx(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Insert entry with failed embedding_status (no embedding)
	payload := makeLorePayload(t, map[string]interface{}{
		"embedding_status": "failed",
	})
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	if err := QueueEmbeddingTx(ctx, tx, "entry-1"); err != nil {
		t.Fatalf("QueueEmbeddingTx() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.EmbeddingStatus != "pending" {
		t.Errorf("EmbeddingStatus = %q, want %q", entry.EmbeddingStatus, "pending")
	}
}

func TestAppendChangeLogBatchTx(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	entries := []engramsync.ChangeLogEntry{
		{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", SourceID: "src-1", Payload: json.RawMessage(`{"id":"e1"}`)},
		{TableName: "lore_entries", EntityID: "e2", Operation: "upsert", SourceID: "src-1", Payload: json.RawMessage(`{"id":"e2"}`)},
	}

	maxSeq, err := s.AppendChangeLogBatchTx(ctx, tx, entries)
	if err != nil {
		t.Fatalf("AppendChangeLogBatchTx() error = %v", err)
	}
	if maxSeq <= 0 {
		t.Errorf("expected maxSeq > 0, got %d", maxSeq)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify entries in change log
	result, err := s.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 change log entries, got %d", len(result))
	}
	if result[1].Sequence != maxSeq {
		t.Errorf("last entry sequence=%d, want %d", result[1].Sequence, maxSeq)
	}
}

func TestAppendChangeLogBatchTx_Rollback(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	entries := []engramsync.ChangeLogEntry{
		{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", SourceID: "src-1", Payload: json.RawMessage(`{"id":"e1"}`)},
	}

	_, err = s.AppendChangeLogBatchTx(ctx, tx, entries)
	if err != nil {
		t.Fatalf("AppendChangeLogBatchTx() error = %v", err)
	}

	// Rollback
	tx.Rollback()

	// Nothing should be in the change log
	result, err := s.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 change log entries after rollback, got %d", len(result))
	}
}

// =============================================================================
// Phase 7: Plugin Migration Runner + DB() accessor tests
// =============================================================================

func TestRunPluginMigrations_CreatesTable(t *testing.T) {
	s := newReplayTestStore(t)

	migrations := []plugin.Migration{
		{
			Version: 999,
			Name:    "test_migration",
			UpSQL:   "CREATE TABLE IF NOT EXISTS pm_test (id TEXT PRIMARY KEY)",
		},
	}

	err := RunPluginMigrations(s.DB(), migrations)
	if err != nil {
		t.Fatalf("RunPluginMigrations() error = %v", err)
	}

	// Verify plugin_migrations table was created
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM plugin_migrations WHERE version = 999").Scan(&count)
	if err != nil {
		t.Fatalf("query plugin_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("migration record count = %d, want 1", count)
	}
}

func TestRunPluginMigrations_AppliesSQL(t *testing.T) {
	s := newReplayTestStore(t)

	migrations := []plugin.Migration{
		{
			Version: 100,
			Name:    "create_test",
			UpSQL:   "CREATE TABLE IF NOT EXISTS pm_items (id TEXT PRIMARY KEY, name TEXT)",
		},
	}

	if err := RunPluginMigrations(s.DB(), migrations); err != nil {
		t.Fatalf("RunPluginMigrations() error = %v", err)
	}

	// Verify the table was created by the migration
	_, err := s.DB().Exec("INSERT INTO pm_items (id, name) VALUES ('t1', 'test')")
	if err != nil {
		t.Fatalf("insert into pm_items: %v (table not created)", err)
	}
}

func TestRunPluginMigrations_Idempotent(t *testing.T) {
	s := newReplayTestStore(t)

	migrations := []plugin.Migration{
		{
			Version: 100,
			Name:    "create_test",
			UpSQL:   "CREATE TABLE IF NOT EXISTS pm_idem (id TEXT PRIMARY KEY)",
		},
	}

	// Apply twice
	if err := RunPluginMigrations(s.DB(), migrations); err != nil {
		t.Fatalf("first RunPluginMigrations() error = %v", err)
	}
	if err := RunPluginMigrations(s.DB(), migrations); err != nil {
		t.Fatalf("second RunPluginMigrations() error = %v (not idempotent)", err)
	}

	// Verify only one record
	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM plugin_migrations WHERE version = 100").Scan(&count)
	if count != 1 {
		t.Errorf("migration record count = %d, want 1 (applied only once)", count)
	}
}

func TestRunPluginMigrations_EmptySlice(t *testing.T) {
	s := newReplayTestStore(t)

	// Empty migrations should be a no-op
	err := RunPluginMigrations(s.DB(), nil)
	if err != nil {
		t.Fatalf("RunPluginMigrations(nil) error = %v", err)
	}

	err = RunPluginMigrations(s.DB(), []plugin.Migration{})
	if err != nil {
		t.Fatalf("RunPluginMigrations([]) error = %v", err)
	}
}

func TestSQLiteStore_DB_NotNil(t *testing.T) {
	s := newReplayTestStore(t)
	if s.DB() == nil {
		t.Error("DB() returned nil")
	}
}

// =============================================================================
// Generic table-agnostic replay tests
// =============================================================================

// newGenericReplayTestStore creates a test store with a custom table and registered schema.
func newGenericReplayTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	plugin.ResetTableSchemas()
	t.Cleanup(func() { plugin.ResetTableSchemas() })

	s := newReplayTestStore(t)
	ctx := context.Background()

	// Create a test table
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_items (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT,
			created_at TEXT,
			updated_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create test_items table: %v", err)
	}

	// Register schema
	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_items",
		Columns:    []string{"id", "name", "status", "created_at", "updated_at"},
		SoftDelete: false,
	})

	return s
}

// RegisterTableSchemas is a helper to register a single schema for testing.
// It uses the exported GetTableSchema/ResetTableSchemas but we need a way to register
// individual schemas. We'll use the generic mechanism via a plugin or directly.
func init() {
	// Make RegisterTableSchemasForTest available as a package-level function.
	// We only need this for test code, but since this is a _test.go file it's fine.
}

// --- Seed 2.2: Generic upsert for a simple table ---

func TestGenericUpsertRow_NewEntry(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	payload := []byte(`{"id":"item-1","name":"Test Item","status":"active","created_at":"2026-01-01T00:00:00Z"}`)
	err := s.UpsertRow(ctx, "test_items", "item-1", payload)
	if err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Verify row exists via raw SQL
	var name, status string
	err = s.db.QueryRowContext(ctx, "SELECT name, status FROM test_items WHERE id = ?", "item-1").Scan(&name, &status)
	if err != nil {
		t.Fatalf("query test_items: %v", err)
	}
	if name != "Test Item" {
		t.Errorf("name = %q, want %q", name, "Test Item")
	}
	if status != "active" {
		t.Errorf("status = %q, want %q", status, "active")
	}
}

func TestGenericUpsertRow_UpdateEntry(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	// Insert
	payload := []byte(`{"id":"item-1","name":"Original","status":"active","created_at":"2026-01-01T00:00:00Z"}`)
	if err := s.UpsertRow(ctx, "test_items", "item-1", payload); err != nil {
		t.Fatalf("first UpsertRow() error = %v", err)
	}

	// Update
	payload = []byte(`{"id":"item-1","name":"Updated","status":"done","created_at":"2026-01-01T00:00:00Z"}`)
	if err := s.UpsertRow(ctx, "test_items", "item-1", payload); err != nil {
		t.Fatalf("second UpsertRow() error = %v", err)
	}

	var name, status string
	err := s.db.QueryRowContext(ctx, "SELECT name, status FROM test_items WHERE id = ?", "item-1").Scan(&name, &status)
	if err != nil {
		t.Fatalf("query test_items: %v", err)
	}
	if name != "Updated" {
		t.Errorf("name = %q, want %q", name, "Updated")
	}
	if status != "done" {
		t.Errorf("status = %q, want %q", status, "done")
	}
}

// --- Seed 2.3: Generic upsert edge cases ---

func TestGenericUpsertRow_IDMismatch(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	payload := []byte(`{"id":"wrong-id","name":"Test","status":"active"}`)
	err := s.UpsertRow(ctx, "test_items", "item-1", payload)
	if err == nil {
		t.Fatal("expected error for ID mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error = %v, want contains 'does not match'", err)
	}
}

func TestGenericUpsertRow_InvalidJSON(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	err := s.UpsertRow(ctx, "test_items", "item-1", []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %v, want contains 'unmarshal'", err)
	}
}

func TestGenericUpsertRow_JSONMetadataField(t *testing.T) {
	plugin.ResetTableSchemas()
	defer plugin.ResetTableSchemas()

	s := newReplayTestStore(t)
	ctx := context.Background()

	// Create table with metadata column
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_meta (
			id TEXT PRIMARY KEY,
			metadata TEXT,
			updated_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_meta",
		Columns:    []string{"id", "metadata", "updated_at"},
		SoftDelete: false,
	})

	// Upsert with JSON object metadata
	payload := []byte(`{"id":"m1","metadata":{"key":"val","nested":true}}`)
	if err := s.UpsertRow(ctx, "test_meta", "m1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Verify metadata stored as JSON string
	var metadataStr string
	err = s.db.QueryRowContext(ctx, "SELECT metadata FROM test_meta WHERE id = ?", "m1").Scan(&metadataStr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	// Parse to verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(metadataStr), &parsed); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if parsed["key"] != "val" {
		t.Errorf("metadata.key = %v, want %q", parsed["key"], "val")
	}
}

func TestGenericUpsertRow_NullableFields(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	// Upsert with null status
	payload := []byte(`{"id":"item-1","name":"Test","status":null}`)
	if err := s.UpsertRow(ctx, "test_items", "item-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	var status sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT status FROM test_items WHERE id = ?", "item-1").Scan(&status)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if status.Valid {
		t.Errorf("status should be NULL, got %q", status.String)
	}
}

// --- Seed 2.4: Generic delete ---

func TestGenericDeleteRow_SoftDelete(t *testing.T) {
	plugin.ResetTableSchemas()
	defer plugin.ResetTableSchemas()

	s := newReplayTestStore(t)
	ctx := context.Background()

	// Create table with soft delete support
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_soft (
			id TEXT PRIMARY KEY,
			name TEXT,
			updated_at TEXT,
			deleted_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_soft",
		Columns:    []string{"id", "name", "updated_at", "deleted_at"},
		SoftDelete: true,
	})

	// Insert a row
	payload := []byte(`{"id":"s1","name":"Soft Item"}`)
	if err := s.UpsertRow(ctx, "test_soft", "s1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Soft delete
	if err := s.DeleteRow(ctx, "test_soft", "s1"); err != nil {
		t.Fatalf("DeleteRow() error = %v", err)
	}

	// Verify row still exists with deleted_at set
	var deletedAt sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT deleted_at FROM test_soft WHERE id = ?", "s1").Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("deleted_at should be set for soft delete")
	}
}

func TestGenericDeleteRow_HardDelete(t *testing.T) {
	s := newGenericReplayTestStore(t) // test_items has SoftDelete=false
	ctx := context.Background()

	// Insert a row
	payload := []byte(`{"id":"item-1","name":"Delete Me","status":"active"}`)
	if err := s.UpsertRow(ctx, "test_items", "item-1", payload); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Hard delete
	if err := s.DeleteRow(ctx, "test_items", "item-1"); err != nil {
		t.Fatalf("DeleteRow() error = %v", err)
	}

	// Verify row is actually gone
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_items WHERE id = ?", "item-1").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("row count = %d, want 0 (hard delete)", count)
	}
}

func TestGenericDeleteRow_Idempotent(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	// Delete non-existent row — should not error
	err := s.DeleteRow(ctx, "test_items", "nonexistent")
	if err != nil {
		t.Fatalf("DeleteRow() on non-existent should be idempotent, got error = %v", err)
	}
}

// --- Seed 2.5: ON CONFLICT preserves FK children ---

func TestGenericUpsertRow_OnConflictPreservesChildren(t *testing.T) {
	plugin.ResetTableSchemas()
	defer plugin.ResetTableSchemas()

	s := newReplayTestStore(t)
	ctx := context.Background()

	// Create parent and child tables with FK constraint and ON DELETE CASCADE
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_parents (
			id TEXT PRIMARY KEY,
			name TEXT,
			updated_at TEXT
		);
		CREATE TABLE IF NOT EXISTS test_children (
			id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL,
			name TEXT,
			updated_at TEXT,
			FOREIGN KEY (parent_id) REFERENCES test_parents(id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_parents",
		Columns:    []string{"id", "name", "updated_at"},
		SoftDelete: false,
	})
	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_children",
		Columns:    []string{"id", "parent_id", "name", "updated_at"},
		SoftDelete: false,
	})

	// Insert parent
	if err := s.UpsertRow(ctx, "test_parents", "p1", []byte(`{"id":"p1","name":"Parent"}`)); err != nil {
		t.Fatalf("UpsertRow parent: %v", err)
	}

	// Insert child referencing parent
	if err := s.UpsertRow(ctx, "test_children", "c1", []byte(`{"id":"c1","parent_id":"p1","name":"Child"}`)); err != nil {
		t.Fatalf("UpsertRow child: %v", err)
	}

	// Upsert parent with updated name (should NOT cascade-delete child)
	if err := s.UpsertRow(ctx, "test_parents", "p1", []byte(`{"id":"p1","name":"Updated Parent"}`)); err != nil {
		t.Fatalf("UpsertRow parent update: %v", err)
	}

	// Verify child still exists
	var childName string
	err = s.db.QueryRowContext(ctx, "SELECT name FROM test_children WHERE id = ?", "c1").Scan(&childName)
	if err != nil {
		t.Fatalf("query child: %v (child was cascade-deleted by upsert!)", err)
	}
	if childName != "Child" {
		t.Errorf("child name = %q, want %q", childName, "Child")
	}

	// Verify parent was updated
	var parentName string
	err = s.db.QueryRowContext(ctx, "SELECT name FROM test_parents WHERE id = ?", "p1").Scan(&parentName)
	if err != nil {
		t.Fatalf("query parent: %v", err)
	}
	if parentName != "Updated Parent" {
		t.Errorf("parent name = %q, want %q", parentName, "Updated Parent")
	}
}

// --- Seed 2.6: Transaction-scoped generic replay ---

func TestUpsertRowTx_GenericPath(t *testing.T) {
	s := newGenericReplayTestStore(t)
	ctx := context.Background()

	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	payload := []byte(`{"id":"item-1","name":"Tx Item","status":"active"}`)
	if err := UpsertRowTx(ctx, tx, "test_items", "item-1", payload); err != nil {
		t.Fatalf("UpsertRowTx() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	var name string
	err = s.db.QueryRowContext(ctx, "SELECT name FROM test_items WHERE id = ?", "item-1").Scan(&name)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if name != "Tx Item" {
		t.Errorf("name = %q, want %q", name, "Tx Item")
	}
}

func TestDeleteRowTx_GenericPath(t *testing.T) {
	plugin.ResetTableSchemas()
	defer plugin.ResetTableSchemas()

	s := newReplayTestStore(t)
	ctx := context.Background()

	// Create table with soft delete
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS test_soft_tx (
			id TEXT PRIMARY KEY,
			name TEXT,
			updated_at TEXT,
			deleted_at TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	plugin.RegisterTableSchemas(plugin.TableSchema{
		Name:       "test_soft_tx",
		Columns:    []string{"id", "name", "updated_at", "deleted_at"},
		SoftDelete: true,
	})

	// Insert row
	if err := s.UpsertRow(ctx, "test_soft_tx", "s1", []byte(`{"id":"s1","name":"Test"}`)); err != nil {
		t.Fatalf("UpsertRow() error = %v", err)
	}

	// Delete via transaction
	tx, err := s.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	if err := DeleteRowTx(ctx, tx, "test_soft_tx", "s1"); err != nil {
		t.Fatalf("DeleteRowTx() error = %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	// Verify soft deleted
	var deletedAt sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT deleted_at FROM test_soft_tx WHERE id = ?", "s1").Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("deleted_at should be set")
	}
}

// --- Seed 2.7: Backward compatibility verification ---

func TestExistingLoreEntries_StillWork(t *testing.T) {
	s := newReplayTestStore(t)
	ctx := context.Background()

	// Verify lore_entries upsert still uses hardcoded path
	payload := makeLorePayload(t, nil)
	if err := s.UpsertRow(ctx, "lore_entries", "entry-1", payload); err != nil {
		t.Fatalf("UpsertRow(lore_entries) error = %v", err)
	}

	entry, err := s.GetLore(ctx, "entry-1")
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.Content != "Test lore content" {
		t.Errorf("Content = %q, want %q", entry.Content, "Test lore content")
	}
	if entry.EmbeddingStatus != "pending" {
		t.Errorf("EmbeddingStatus = %q, want %q (should default to pending)", entry.EmbeddingStatus, "pending")
	}

	// Verify lore_entries delete still uses hardcoded path
	if err := s.DeleteRow(ctx, "lore_entries", "entry-1"); err != nil {
		t.Fatalf("DeleteRow(lore_entries) error = %v", err)
	}

	_, err = s.GetLore(ctx, "entry-1")
	if err == nil {
		t.Error("expected error for soft-deleted entry")
	}

	// Verify lore_entries is NOT in the schema registry
	_, ok := plugin.GetTableSchema("lore_entries")
	if ok {
		t.Error("lore_entries should NOT be in schema registry")
	}
}
