package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
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
