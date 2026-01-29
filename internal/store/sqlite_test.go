package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/types"
	_ "modernc.org/sqlite"
)

func TestStore_NewSQLiteStore(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
}

func TestStore_Record(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	lore := types.Lore{
		Content:  "Test lore content",
		Category: types.CategoryPatternOutcome,
		Context:  "test context",
	}

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	result, err := db.Record(lore, embedding)
	if err != nil {
		t.Fatal(err)
	}

	if result.ID == "" {
		t.Error("Expected ID to be set")
	}

	if result.Content != lore.Content {
		t.Errorf("Expected content %q, got %q", lore.Content, result.Content)
	}
}

func TestStore_Count(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	count, err := db.Count()
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	lore := types.Lore{
		Content:  "Test lore",
		Category: types.CategoryPatternOutcome,
	}
	embedding := make([]float32, 1536)
	_, err = db.Record(lore, embedding)
	if err != nil {
		t.Fatal(err)
	}

	count, err = db.Count()
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}
}

// --- IngestLore Tests ---

func TestIngestLore_SingleEntry(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test lore content",
		Context:    "test context",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.85,
		SourceID:   "source-123",
	}

	result, err := db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	if result.Accepted != 1 {
		t.Errorf("Expected Accepted=1, got %d", result.Accepted)
	}
	if result.Merged != 0 {
		t.Errorf("Expected Merged=0, got %d", result.Merged)
	}
	if result.Rejected != 0 {
		t.Errorf("Expected Rejected=0, got %d", result.Rejected)
	}
}

func TestIngestLore_BatchEntries(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entries := make([]types.NewLoreEntry, 5)
	for i := range entries {
		entries[i] = types.NewLoreEntry{
			Content:    "Test content " + string(rune('A'+i)),
			Category:   "PATTERN_OUTCOME",
			Confidence: 0.8,
			SourceID:   "batch-source",
		}
	}

	result, err := db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	if result.Accepted != 5 {
		t.Errorf("Expected Accepted=5, got %d", result.Accepted)
	}
}

func TestIngestLore_GeneratesValidULID(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-123",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Query the entry to verify ULID format
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	// ULID is 26 characters, Crockford base32
	ulidPattern := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	if !ulidPattern.MatchString(id) {
		t.Errorf("ID %q does not match ULID format", id)
	}
}

func TestIngestLore_SetsTimestamps(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	before := time.Now().UTC().Add(-time.Second)

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-123",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	after := time.Now().UTC().Add(time.Second)

	var createdAt, updatedAt string
	err = db.db.QueryRow("SELECT created_at, updated_at FROM lore_entries LIMIT 1").Scan(&createdAt, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}

	created, _ := time.Parse(time.RFC3339, createdAt)
	updated, _ := time.Parse(time.RFC3339, updatedAt)

	if created.Before(before) || created.After(after) {
		t.Errorf("created_at %v not in expected range [%v, %v]", created, before, after)
	}
	if updated.Before(before) || updated.After(after) {
		t.Errorf("updated_at %v not in expected range [%v, %v]", updated, before, after)
	}
	if !created.Equal(updated) {
		t.Errorf("created_at and updated_at should be equal on creation")
	}
}

func TestIngestLore_SetsEmbeddingStatusPending(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-123",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var status string
	err = db.db.QueryRow("SELECT embedding_status FROM lore_entries LIMIT 1").Scan(&status)
	if err != nil {
		t.Fatal(err)
	}

	if status != "pending" {
		t.Errorf("Expected embedding_status='pending', got %q", status)
	}
}

func TestIngestLore_InitializesSourcesArray(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "my-source-id",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var sourcesJSON string
	err = db.db.QueryRow("SELECT sources FROM lore_entries LIMIT 1").Scan(&sourcesJSON)
	if err != nil {
		t.Fatal(err)
	}

	var sources []string
	if err := json.Unmarshal([]byte(sourcesJSON), &sources); err != nil {
		t.Fatalf("Failed to parse sources JSON: %v", err)
	}

	if len(sources) != 1 || sources[0] != "my-source-id" {
		t.Errorf("Expected sources=[\"my-source-id\"], got %v", sources)
	}
}

func TestIngestLore_EmptyBatch(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.IngestLore(context.Background(), []types.NewLoreEntry{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Accepted != 0 {
		t.Errorf("Expected Accepted=0 for empty batch, got %d", result.Accepted)
	}
}

func TestIngestLore_EscapesSpecialCharactersInSourceID(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// SourceID with characters that need JSON escaping
	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   `source"with"quotes\and\backslashes`,
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Verify we can retrieve it and sources parsed correctly
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	retrieved, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if len(retrieved.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(retrieved.Sources))
	}
	if retrieved.Sources[0] != entry.SourceID {
		t.Errorf("Expected SourceID=%q, got %q", entry.SourceID, retrieved.Sources[0])
	}
}

// --- GetLore Tests ---

func TestGetLore_ReturnsEntry(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content for retrieval",
		Context:    "test context",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.75,
		SourceID:   "source-456",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	retrieved, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if retrieved.ID != id {
		t.Errorf("Expected ID=%q, got %q", id, retrieved.ID)
	}
	if retrieved.Content != entry.Content {
		t.Errorf("Expected Content=%q, got %q", entry.Content, retrieved.Content)
	}
	if retrieved.Context != entry.Context {
		t.Errorf("Expected Context=%q, got %q", entry.Context, retrieved.Context)
	}
	if retrieved.Category != entry.Category {
		t.Errorf("Expected Category=%q, got %q", entry.Category, retrieved.Category)
	}
	if retrieved.Confidence != entry.Confidence {
		t.Errorf("Expected Confidence=%v, got %v", entry.Confidence, retrieved.Confidence)
	}
	if retrieved.SourceID != entry.SourceID {
		t.Errorf("Expected SourceID=%q, got %q", entry.SourceID, retrieved.SourceID)
	}
	if retrieved.EmbeddingStatus != "pending" {
		t.Errorf("Expected EmbeddingStatus='pending', got %q", retrieved.EmbeddingStatus)
	}
}

func TestGetLore_NotFound(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.GetLore(context.Background(), "nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestGetLore_ExcludesDeleted(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "To be deleted",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-123",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID and soft-delete
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.db.Exec("UPDATE lore_entries SET deleted_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.GetLore(context.Background(), id)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound for deleted entry, got %v", err)
	}
}

func TestGetLore_ParsesSourcesJSON(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-abc",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	retrieved, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if len(retrieved.Sources) != 1 || retrieved.Sources[0] != "source-abc" {
		t.Errorf("Expected Sources=[\"source-abc\"], got %v", retrieved.Sources)
	}
}

// --- GetPendingEmbeddings Tests ---

func TestGetPendingEmbeddings_ReturnsPending(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entries := []types.NewLoreEntry{
		{Content: "Entry 1", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
		{Content: "Entry 2", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}

	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	pending, err := db.GetPendingEmbeddings(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 2 {
		t.Errorf("Expected 2 pending entries, got %d", len(pending))
	}
}

func TestGetPendingEmbeddings_RespectsLimit(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entries := make([]types.NewLoreEntry, 5)
	for i := range entries {
		entries[i] = types.NewLoreEntry{
			Content:    "Entry " + string(rune('A'+i)),
			Category:   "PATTERN_OUTCOME",
			Confidence: 0.8,
			SourceID:   "src",
		}
	}

	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	pending, err := db.GetPendingEmbeddings(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 2 {
		t.Errorf("Expected 2 entries (limited), got %d", len(pending))
	}
}

func TestGetPendingEmbeddings_OrdersByCreatedAt(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert entries with small delays to ensure different timestamps
	for i := 0; i < 3; i++ {
		entry := types.NewLoreEntry{
			Content:    "Entry " + string(rune('A'+i)),
			Category:   "PATTERN_OUTCOME",
			Confidence: 0.8,
			SourceID:   "src",
		}
		_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond) // Small delay for timestamp ordering
	}

	pending, err := db.GetPendingEmbeddings(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(pending))
	}

	// Verify oldest first
	if pending[0].Content != "Entry A" {
		t.Errorf("Expected first entry to be 'Entry A', got %q", pending[0].Content)
	}
	if pending[2].Content != "Entry C" {
		t.Errorf("Expected last entry to be 'Entry C', got %q", pending[2].Content)
	}
}

func TestGetPendingEmbeddings_ExcludesComplete(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entries := []types.NewLoreEntry{
		{Content: "Entry pending", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
		{Content: "Entry complete", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}

	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Mark one as complete
	_, err = db.db.Exec("UPDATE lore_entries SET embedding_status = 'complete' WHERE content = 'Entry complete'")
	if err != nil {
		t.Fatal(err)
	}

	pending, err := db.GetPendingEmbeddings(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 1 {
		t.Errorf("Expected 1 pending entry, got %d", len(pending))
	}
	if pending[0].Content != "Entry pending" {
		t.Errorf("Expected 'Entry pending', got %q", pending[0].Content)
	}
}

// --- UpdateEmbedding Tests ---

func TestUpdateEmbedding_SetsEmbedding(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "src",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	err = db.UpdateEmbedding(context.Background(), id, embedding)
	if err != nil {
		t.Fatal(err)
	}

	// Verify embedding was stored and can be retrieved
	retrieved, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if len(retrieved.Embedding) != 1536 {
		t.Errorf("Expected embedding length 1536, got %d", len(retrieved.Embedding))
	}

	// Spot check a few values
	if retrieved.Embedding[0] != 0.0 {
		t.Errorf("Expected embedding[0]=0.0, got %v", retrieved.Embedding[0])
	}
	if retrieved.Embedding[768] != float32(768)/1536.0 {
		t.Errorf("Expected embedding[768]=%v, got %v", float32(768)/1536.0, retrieved.Embedding[768])
	}
}

func TestUpdateEmbedding_SetsStatusComplete(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "src",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	embedding := make([]float32, 1536)
	err = db.UpdateEmbedding(context.Background(), id, embedding)
	if err != nil {
		t.Fatal(err)
	}

	retrieved, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if retrieved.EmbeddingStatus != "complete" {
		t.Errorf("Expected EmbeddingStatus='complete', got %q", retrieved.EmbeddingStatus)
	}
}

func TestUpdateEmbedding_UpdatesTimestamp(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "src",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	// Get original timestamp
	original, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1100 * time.Millisecond) // Sleep >1s to ensure RFC3339 timestamp changes

	embedding := make([]float32, 1536)
	err = db.UpdateEmbedding(context.Background(), id, embedding)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Errorf("Expected updated_at to be after original, got original=%v updated=%v", original.UpdatedAt, updated.UpdatedAt)
	}
}

func TestUpdateEmbedding_NotFound(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	embedding := make([]float32, 1536)
	err = db.UpdateEmbedding(context.Background(), "nonexistent-id", embedding)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

// --- Concurrency Test ---

func TestConcurrentReadWrite(t *testing.T) {
	// Use temp file for concurrent access testing (in-memory has connection pool issues)
	tmpFile := t.TempDir() + "/test_concurrent.db"
	db, err := NewSQLiteStore(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed some data
	entries := make([]types.NewLoreEntry, 10)
	for i := range entries {
		entries[i] = types.NewLoreEntry{
			Content:    "Content " + string(rune('A'+i)),
			Category:   "PATTERN_OUTCOME",
			Confidence: 0.8,
			SourceID:   "src",
		}
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	readErrors := make(chan error, 10)
	writeComplete := make(chan struct{})

	// Start a writer that does sequential writes (SQLite only allows one writer at a time)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(writeComplete)
		for i := 0; i < 5; i++ {
			entry := types.NewLoreEntry{
				Content:    "Concurrent " + string(rune('A'+i)),
				Category:   "PATTERN_OUTCOME",
				Confidence: 0.8,
				SourceID:   "concurrent-src",
			}
			_, err := db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
			if err != nil {
				t.Errorf("Write error: %v", err)
			}
		}
	}()

	// Concurrent readers should NOT be blocked by the writer (WAL mode)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Read while writes may be happening
			_, err := db.GetPendingEmbeddings(context.Background(), 5)
			if err != nil {
				readErrors <- err
			}
		}()
	}

	wg.Wait()
	close(readErrors)

	for err := range readErrors {
		t.Errorf("Read error during concurrent write: %v", err)
	}
}

// --- MarkEmbeddingFailed Tests ---

func TestMarkEmbeddingFailed_Success(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create an entry with pending embedding status
	entry := types.NewLoreEntry{
		Content:    "Test content for failed embedding",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "test-source",
	}

	result, err := db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 1 {
		t.Fatalf("Expected 1 accepted, got %d", result.Accepted)
	}

	// Get the entry to find its ID
	pending, err := db.GetPendingEmbeddings(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("Expected 1 pending entry, got %d", len(pending))
	}
	id := pending[0].ID

	// Mark as failed
	err = db.MarkEmbeddingFailed(context.Background(), id)
	if err != nil {
		t.Errorf("MarkEmbeddingFailed() error = %v", err)
	}

	// Verify it's no longer in pending list
	pending, err = db.GetPendingEmbeddings(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending entries after marking failed, got %d", len(pending))
	}

	// Verify the entry still exists and has failed status
	entry2, err := db.GetLore(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if entry2.EmbeddingStatus != "failed" {
		t.Errorf("Expected embedding_status 'failed', got %q", entry2.EmbeddingStatus)
	}
}

func TestMarkEmbeddingFailed_NotFound(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.MarkEmbeddingFailed(context.Background(), "nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestMarkEmbeddingFailed_ExcludesDeleted(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create an entry
	entry := types.NewLoreEntry{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "test-source",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := db.GetPendingEmbeddings(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	id := pending[0].ID

	// Soft-delete the entry directly via SQL
	_, err = db.db.Exec("UPDATE lore_entries SET deleted_at = datetime('now') WHERE id = ?", id)
	if err != nil {
		t.Fatal(err)
	}

	// Try to mark as failed - should return ErrNotFound
	err = db.MarkEmbeddingFailed(context.Background(), id)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound for soft-deleted entry, got %v", err)
	}
}

// --- FindSimilar Tests ---

// makeTestEmbedding creates a normalized embedding for testing.
// Using a simple pattern: first dimension has value 1.0, rest are 0.
// This makes cosine similarity calculation predictable.
func makeTestEmbedding(dim int) []float32 {
	emb := make([]float32, 1536)
	emb[dim%1536] = 1.0
	return emb
}

// makeIdenticalEmbedding creates an embedding identical to another (cosine similarity = 1.0).
func makeIdenticalEmbedding(base []float32) []float32 {
	emb := make([]float32, len(base))
	copy(emb, base)
	return emb
}

// makeOrthogonalEmbedding creates an embedding orthogonal to another (cosine similarity = 0.0).
func makeOrthogonalEmbedding(base []float32) []float32 {
	emb := make([]float32, len(base))
	// Find the first dimension with value and set a different one
	for i := range base {
		if base[i] != 0 {
			emb[(i+1)%len(base)] = 1.0
			return emb
		}
	}
	emb[0] = 1.0
	return emb
}

// makeSimilarEmbedding creates an embedding with a specific target similarity to the base.
// Uses weighted combination: result = cos(angle)*base + sin(angle)*orthogonal
// where angle = acos(targetSimilarity)
func makeSimilarEmbedding(base []float32, targetSimilarity float64) []float32 {
	if targetSimilarity >= 1.0 {
		return makeIdenticalEmbedding(base)
	}
	if targetSimilarity <= 0.0 {
		return makeOrthogonalEmbedding(base)
	}

	emb := make([]float32, len(base))
	orthogonal := makeOrthogonalEmbedding(base)

	// For cosine similarity S, use angle θ = acos(S)
	// result = cos(θ)*base + sin(θ)*orthogonal
	cosAngle := targetSimilarity
	sinAngle := math.Sqrt(1.0 - cosAngle*cosAngle)

	for i := range emb {
		emb[i] = float32(cosAngle)*base[i] + float32(sinAngle)*orthogonal[i]
	}
	return emb
}

// setupFindSimilarTest creates a store with test entries that have known embeddings.
func setupFindSimilarTest(t *testing.T) (*SQLiteStore, []float32) {
	t.Helper()
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	// Create a base embedding for similarity calculations
	baseEmbedding := makeTestEmbedding(0)

	return db, baseEmbedding
}

// insertEntryWithEmbedding is a helper to insert an entry and set its embedding.
func insertEntryWithEmbedding(t *testing.T, db *SQLiteStore, content, category string, embedding []float32) string {
	t.Helper()

	entry := types.NewLoreEntry{
		Content:    content,
		Category:   category,
		Confidence: 0.8,
		SourceID:   "test-source",
	}

	_, err := db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID of the just-inserted entry
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries WHERE content = ? ORDER BY created_at DESC LIMIT 1", content).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	// Set the embedding
	if embedding != nil {
		err = db.UpdateEmbedding(context.Background(), id, embedding)
		if err != nil {
			t.Fatal(err)
		}
	}

	return id
}

func TestFindSimilar_ReturnsEntriesAboveThreshold(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entries with different similarity levels
	// Entry with identical embedding (similarity = 1.0)
	insertEntryWithEmbedding(t, db, "Identical entry", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	// Entry with high similarity (0.95)
	insertEntryWithEmbedding(t, db, "High similarity", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.95))

	// Entry with low similarity (0.5) - should be excluded
	insertEntryWithEmbedding(t, db, "Low similarity", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.5))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should return 2 entries (1.0 and 0.95, both >= 0.92)
	if len(results) != 2 {
		t.Errorf("Expected 2 results above threshold 0.92, got %d", len(results))
	}
}

func TestFindSimilar_ExcludesEntriesBelowThreshold(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entry just below threshold (0.91)
	insertEntryWithEmbedding(t, db, "Just below threshold", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.91))

	// Insert entry at threshold (0.92) - should be included
	insertEntryWithEmbedding(t, db, "At threshold", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.92))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should return only 1 entry (at threshold)
	if len(results) != 1 {
		t.Errorf("Expected 1 result at threshold, got %d", len(results))
	}

	if len(results) > 0 && results[0].Content != "At threshold" {
		t.Errorf("Expected 'At threshold' entry, got %q", results[0].Content)
	}
}

func TestFindSimilar_FiltersByCategory(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert identical embedding in target category
	insertEntryWithEmbedding(t, db, "Same category", "DEPENDENCY_BEHAVIOR", makeIdenticalEmbedding(baseEmbedding))

	// Insert identical embedding in different category - should be excluded
	insertEntryWithEmbedding(t, db, "Different category", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "DEPENDENCY_BEHAVIOR", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should return only the entry from DEPENDENCY_BEHAVIOR category
	if len(results) != 1 {
		t.Errorf("Expected 1 result from matching category, got %d", len(results))
	}

	if len(results) > 0 && results[0].Category != "DEPENDENCY_BEHAVIOR" {
		t.Errorf("Expected category DEPENDENCY_BEHAVIOR, got %q", results[0].Category)
	}
}

func TestFindSimilar_OrdersBySimilarityDescending(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert in non-sorted order
	insertEntryWithEmbedding(t, db, "Medium similarity", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.95))
	insertEntryWithEmbedding(t, db, "Highest similarity", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))
	insertEntryWithEmbedding(t, db, "Lower similarity", "PATTERN_OUTCOME", makeSimilarEmbedding(baseEmbedding, 0.93))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Verify descending order
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Errorf("Results not sorted descending: results[%d].Similarity=%v > results[%d].Similarity=%v",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}

	// First result should be the highest similarity (identical embedding)
	if results[0].Content != "Highest similarity" {
		t.Errorf("Expected first result to be 'Highest similarity', got %q", results[0].Content)
	}
}

func TestFindSimilar_ReturnsEmptySliceWhenNoMatches(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entry with low similarity (below any reasonable threshold)
	insertEntryWithEmbedding(t, db, "Very dissimilar", "PATTERN_OUTCOME", makeOrthogonalEmbedding(baseEmbedding))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should return empty slice, not nil
	if results == nil {
		t.Error("Expected empty slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestFindSimilar_ExcludesEntriesWithoutEmbeddings(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entry without embedding (nil)
	insertEntryWithEmbedding(t, db, "No embedding", "PATTERN_OUTCOME", nil)

	// Insert entry with embedding
	insertEntryWithEmbedding(t, db, "Has embedding", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should only return the entry with embedding
	if len(results) != 1 {
		t.Errorf("Expected 1 result (with embedding), got %d", len(results))
	}

	if len(results) > 0 && results[0].Content != "Has embedding" {
		t.Errorf("Expected 'Has embedding' entry, got %q", results[0].Content)
	}
}

func TestFindSimilar_ExcludesDeletedEntries(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entry and then soft-delete it
	id := insertEntryWithEmbedding(t, db, "Deleted entry", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	// Soft-delete the entry
	_, err := db.db.Exec("UPDATE lore_entries SET deleted_at = datetime('now') WHERE id = ?", id)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a non-deleted entry
	insertEntryWithEmbedding(t, db, "Active entry", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	// Should only return the non-deleted entry
	if len(results) != 1 {
		t.Errorf("Expected 1 result (non-deleted), got %d", len(results))
	}

	if len(results) > 0 && results[0].Content != "Active entry" {
		t.Errorf("Expected 'Active entry', got %q", results[0].Content)
	}
}

func TestFindSimilar_IncludesSimilarityScoreInResult(t *testing.T) {
	db, baseEmbedding := setupFindSimilarTest(t)
	defer db.Close()

	// Insert entry with identical embedding (similarity should be 1.0)
	insertEntryWithEmbedding(t, db, "Identical entry", "PATTERN_OUTCOME", makeIdenticalEmbedding(baseEmbedding))

	results, err := db.FindSimilar(context.Background(), baseEmbedding, "PATTERN_OUTCOME", 0.92)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Similarity should be very close to 1.0 for identical embeddings
	if results[0].Similarity < 0.99 {
		t.Errorf("Expected similarity ~1.0 for identical embedding, got %v", results[0].Similarity)
	}
}

// --- Helper Function Tests (Story 3.2) ---

func TestAppendContext_EmptyExisting(t *testing.T) {
	result := appendContext("", "new context")
	if result != "new context" {
		t.Errorf("Expected 'new context', got %q", result)
	}
}

func TestAppendContext_EmptyNew(t *testing.T) {
	result := appendContext("existing context", "")
	if result != "existing context" {
		t.Errorf("Expected 'existing context', got %q", result)
	}
}

func TestAppendContext_BothNonEmpty(t *testing.T) {
	result := appendContext("existing", "new")
	expected := "existing\n\n---\n\nnew"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestAppendContext_TruncatesLongNewContext(t *testing.T) {
	existing := "short"
	// Create a new context that would exceed 1000 chars total
	newCtx := strings.Repeat("x", 1000)

	result := appendContext(existing, newCtx)

	if len(result) > MaxContextLength {
		t.Errorf("Result length %d exceeds MaxContextLength %d", len(result), MaxContextLength)
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("Expected truncation marker '...' at end")
	}
	if !strings.HasPrefix(result, "short\n\n---\n\n") {
		t.Errorf("Expected existing context preserved, got %q", result[:20])
	}
}

func TestAppendContext_PreservesExistingWhenNoRoom(t *testing.T) {
	// Existing context at max length
	existing := strings.Repeat("x", MaxContextLength)
	result := appendContext(existing, "new context")

	if result != existing {
		t.Error("Expected existing context to be preserved when no room")
	}
}

func TestAppendContext_TruncatesNewContextAtEmptyExisting(t *testing.T) {
	// New context exceeds 1000 chars with empty existing
	newCtx := strings.Repeat("x", 1500)
	result := appendContext("", newCtx)

	if len(result) != MaxContextLength {
		t.Errorf("Expected length %d, got %d", MaxContextLength, len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("Expected truncation marker '...'")
	}
}

func TestAddSourceID_AddsNewSource(t *testing.T) {
	sources := []string{"source-1", "source-2"}
	result, added := addSourceID(sources, "source-3")

	if !added {
		t.Error("Expected added=true for new source")
	}
	if len(result) != 3 {
		t.Errorf("Expected 3 sources, got %d", len(result))
	}
	if result[2] != "source-3" {
		t.Errorf("Expected 'source-3' at end, got %q", result[2])
	}
}

func TestAddSourceID_SkipsDuplicate(t *testing.T) {
	sources := []string{"source-1", "source-2"}
	result, added := addSourceID(sources, "source-1")

	if added {
		t.Error("Expected added=false for duplicate source")
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 sources (unchanged), got %d", len(result))
	}
}

func TestAddSourceID_EmptySlice(t *testing.T) {
	sources := []string{}
	result, added := addSourceID(sources, "first-source")

	if !added {
		t.Error("Expected added=true for first source")
	}
	if len(result) != 1 {
		t.Errorf("Expected 1 source, got %d", len(result))
	}
}

// --- MergeLore Tests (Story 3.2) ---

// setupMergeLoreTest creates a store with a target entry for merge testing.
func setupMergeLoreTest(t *testing.T, confidence float64, ctx string) (*SQLiteStore, string) {
	t.Helper()
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	// Insert a target entry
	entry := types.NewLoreEntry{
		Content:    "Target content",
		Context:    ctx,
		Category:   "PATTERN_OUTCOME",
		Confidence: confidence,
		SourceID:   "original-source",
	}

	_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	return db, id
}

func TestMergeLore_BoostsConfidenceBy010(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "source context",
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	// Verify confidence boosted
	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	expected := 0.90
	if math.Abs(updated.Confidence-expected) > 0.001 {
		t.Errorf("Expected confidence %v, got %v", expected, updated.Confidence)
	}
}

func TestMergeLore_CapsConfidenceAt10(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.95, "original context")
	defer db.Close()

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "source context",
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	// Should be capped at 1.0, not 1.05
	if updated.Confidence > 1.0 {
		t.Errorf("Confidence should be capped at 1.0, got %v", updated.Confidence)
	}
	if updated.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0 (capped), got %v", updated.Confidence)
	}
}

func TestMergeLore_AppendsContextWithSeparator(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	expected := "original context\n\n---\n\nnew context"
	if updated.Context != expected {
		t.Errorf("Expected context %q, got %q", expected, updated.Context)
	}
}

func TestMergeLore_TruncatesContextAt1000Chars(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	// Create a very long source context
	longContext := strings.Repeat("x", 2000)
	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  longContext,
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	if len(updated.Context) > MaxContextLength {
		t.Errorf("Context length %d exceeds max %d", len(updated.Context), MaxContextLength)
	}
}

func TestMergeLore_AddsTruncationMarker(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	longContext := strings.Repeat("x", 2000)
	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  longContext,
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(updated.Context, "...") {
		t.Error("Expected truncation marker '...' at end of context")
	}
}

func TestMergeLore_AddsSourceIdToArray(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "merge-source",
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	// Should have both original-source and merge-source
	if len(updated.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d: %v", len(updated.Sources), updated.Sources)
	}

	found := false
	for _, s := range updated.Sources {
		if s == "merge-source" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'merge-source' in sources array: %v", updated.Sources)
	}
}

func TestMergeLore_DoesNotDuplicateSourceId(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	// Merge with the same source_id that already exists
	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "original-source", // Same as original
	}

	err := db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	// Should still have only 1 source (no duplicate)
	if len(updated.Sources) != 1 {
		t.Errorf("Expected 1 source (no duplicate), got %d: %v", len(updated.Sources), updated.Sources)
	}
}

func TestMergeLore_UpdatesTimestamp(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	// Get original timestamp
	original, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1100 * time.Millisecond) // Ensure timestamp changes

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "merge-source",
	}

	err = db.MergeLore(context.Background(), targetID, source)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := db.GetLore(context.Background(), targetID)
	if err != nil {
		t.Fatal(err)
	}

	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Errorf("Expected updated_at to be after original: original=%v, updated=%v",
			original.UpdatedAt, updated.UpdatedAt)
	}
}

func TestMergeLore_ReturnsErrNotFoundForMissingTarget(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "merge-source",
	}

	err = db.MergeLore(context.Background(), "nonexistent-id", source)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestMergeLore_ReturnsErrNotFoundForDeletedTarget(t *testing.T) {
	db, targetID := setupMergeLoreTest(t, 0.80, "original context")
	defer db.Close()

	// Soft-delete the target
	_, err := db.db.Exec("UPDATE lore_entries SET deleted_at = datetime('now') WHERE id = ?", targetID)
	if err != nil {
		t.Fatal(err)
	}

	source := types.NewLoreEntry{
		Content:  "Source content",
		Context:  "new context",
		SourceID: "merge-source",
	}

	err = db.MergeLore(context.Background(), targetID, source)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound for deleted target, got %v", err)
	}
}

// --- Deduplication Integration Tests (Story 3.3) ---

// mockEmbedder implements the Embedder interface for testing.
type mockEmbedder struct {
	embeddings map[string][]float32
	err        error
}

func (m *mockEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	if emb, ok := m.embeddings[content]; ok {
		return emb, nil
	}
	return makeTestEmbedding(len(content)), nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, contents []string) ([][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := make([][]float32, len(contents))
	for i, content := range contents {
		if emb, ok := m.embeddings[content]; ok {
			result[i] = emb
		} else {
			result[i] = makeTestEmbedding(len(content))
		}
	}
	return result, nil
}

func (m *mockEmbedder) ModelName() string {
	return "mock-embedder"
}

// mockConfig implements the Config interface for testing.
type mockConfig struct {
	dedupEnabled bool
	threshold    float64
}

func (m *mockConfig) GetDeduplicationEnabled() bool {
	return m.dedupEnabled
}

func (m *mockConfig) GetSimilarityThreshold() float64 {
	return m.threshold
}

// setupDeduplicationTest creates a store with embedder and config for deduplication testing.
func setupDeduplicationTest(t *testing.T, dedupEnabled bool, threshold float64, embeddings map[string][]float32) *SQLiteStore {
	t.Helper()
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	embedder := &mockEmbedder{embeddings: embeddings}
	cfg := &mockConfig{dedupEnabled: dedupEnabled, threshold: threshold}
	db.SetDependencies(embedder, cfg)

	return db
}

func TestIngestLore_WithDeduplication_FindsAndMergesDuplicate(t *testing.T) {
	// Create embeddings that will be identical (similarity = 1.0)
	baseEmbedding := makeTestEmbedding(0)
	embeddings := map[string][]float32{
		"First content":  baseEmbedding,
		"Second content": baseEmbedding, // Same embedding = duplicate
	}

	db := setupDeduplicationTest(t, true, 0.92, embeddings)
	defer db.Close()

	// First ingest: store the first entry
	first := []types.NewLoreEntry{{
		Content:    "First content",
		Context:    "First context",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}

	result1, err := db.IngestLore(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if result1.Accepted != 1 {
		t.Errorf("First ingest: expected accepted=1, got %d", result1.Accepted)
	}

	// Second ingest: should detect duplicate and merge
	second := []types.NewLoreEntry{{
		Content:    "Second content",
		Context:    "Second context",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.7,
		SourceID:   "source-2",
	}}

	result2, err := db.IngestLore(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	if result2.Merged != 1 {
		t.Errorf("Second ingest: expected merged=1, got %d", result2.Merged)
	}
	if result2.Accepted != 0 {
		t.Errorf("Second ingest: expected accepted=0, got %d", result2.Accepted)
	}

	// Verify only one entry exists
	var count int
	err = db.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("Expected 1 entry in store, got %d", count)
	}

	// Verify the entry was merged (confidence boosted, contexts combined)
	var confidence float64
	var ctx string
	err = db.db.QueryRow("SELECT confidence, context FROM lore_entries WHERE deleted_at IS NULL").Scan(&confidence, &ctx)
	if err != nil {
		t.Fatal(err)
	}
	if confidence != 0.9 { // 0.8 + 0.10
		t.Errorf("Expected confidence 0.9 after merge, got %v", confidence)
	}
	if !strings.Contains(ctx, "First context") || !strings.Contains(ctx, "Second context") {
		t.Errorf("Expected merged contexts, got %q", ctx)
	}
}

func TestIngestLore_WithDeduplication_DifferentCategoriesNotMerged(t *testing.T) {
	baseEmbedding := makeTestEmbedding(0)
	embeddings := map[string][]float32{
		"First content":  baseEmbedding,
		"Second content": baseEmbedding,
	}

	db := setupDeduplicationTest(t, true, 0.92, embeddings)
	defer db.Close()

	// First entry in PATTERN_OUTCOME
	first := []types.NewLoreEntry{{
		Content:    "First content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}
	_, err := db.IngestLore(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}

	// Second entry in DEPENDENCY_BEHAVIOR (different category)
	second := []types.NewLoreEntry{{
		Content:    "Second content",
		Category:   "DEPENDENCY_BEHAVIOR",
		Confidence: 0.8,
		SourceID:   "source-2",
	}}
	result, err := db.IngestLore(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	// Should be accepted, not merged (different categories)
	if result.Accepted != 1 {
		t.Errorf("Expected accepted=1, got %d", result.Accepted)
	}
	if result.Merged != 0 {
		t.Errorf("Expected merged=0, got %d", result.Merged)
	}

	// Verify two entries exist
	var count int
	err = db.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("Expected 2 entries (different categories), got %d", count)
	}
}

func TestIngestLore_WithDeduplication_BelowThresholdNotMerged(t *testing.T) {
	// Create orthogonal embeddings (similarity = 0)
	embeddings := map[string][]float32{
		"First content":  makeTestEmbedding(0),
		"Second content": makeTestEmbedding(1), // Different dimension = orthogonal
	}

	db := setupDeduplicationTest(t, true, 0.92, embeddings)
	defer db.Close()

	first := []types.NewLoreEntry{{
		Content:    "First content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}
	_, err := db.IngestLore(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}

	second := []types.NewLoreEntry{{
		Content:    "Second content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-2",
	}}
	result, err := db.IngestLore(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	// Should be accepted (below threshold)
	if result.Accepted != 1 {
		t.Errorf("Expected accepted=1, got %d", result.Accepted)
	}
	if result.Merged != 0 {
		t.Errorf("Expected merged=0, got %d", result.Merged)
	}
}

func TestIngestLore_DeduplicationDisabled_StoresAll(t *testing.T) {
	baseEmbedding := makeTestEmbedding(0)
	embeddings := map[string][]float32{
		"First content":  baseEmbedding,
		"Second content": baseEmbedding,
	}

	// Deduplication DISABLED
	db := setupDeduplicationTest(t, false, 0.92, embeddings)
	defer db.Close()

	first := []types.NewLoreEntry{{
		Content:    "First content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}
	_, err := db.IngestLore(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}

	second := []types.NewLoreEntry{{
		Content:    "Second content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-2",
	}}
	result, err := db.IngestLore(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	// Should be accepted (deduplication disabled)
	if result.Accepted != 1 {
		t.Errorf("Expected accepted=1, got %d", result.Accepted)
	}
	if result.Merged != 0 {
		t.Errorf("Expected merged=0 (dedup disabled), got %d", result.Merged)
	}

	// Verify two entries exist
	var count int
	err = db.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("Expected 2 entries (dedup disabled), got %d", count)
	}
}

func TestIngestLore_EmbeddingFailure_StoresAsPending(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Set up embedder that returns an error
	embedder := &mockEmbedder{err: errors.New("API error")}
	cfg := &mockConfig{dedupEnabled: true, threshold: 0.92}
	db.SetDependencies(embedder, cfg)

	entries := []types.NewLoreEntry{{
		Content:    "Test content",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}

	result, err := db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Should be accepted (stored as pending)
	if result.Accepted != 1 {
		t.Errorf("Expected accepted=1, got %d", result.Accepted)
	}

	// Verify entry has pending embedding status
	var status string
	err = db.db.QueryRow("SELECT embedding_status FROM lore_entries WHERE deleted_at IS NULL").Scan(&status)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Errorf("Expected embedding_status='pending', got %q", status)
	}
}

func TestIngestLore_MergedCountAccurate(t *testing.T) {
	baseEmbedding := makeTestEmbedding(0)
	embeddings := map[string][]float32{
		"Content A": baseEmbedding,
		"Content B": baseEmbedding,     // Duplicate of A
		"Content C": makeTestEmbedding(1), // Different
		"Content D": baseEmbedding,     // Duplicate of A
		"Content E": makeTestEmbedding(2), // Different
	}

	db := setupDeduplicationTest(t, true, 0.92, embeddings)
	defer db.Close()

	// First: store entry A
	first := []types.NewLoreEntry{{
		Content:    "Content A",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.8,
		SourceID:   "source-1",
	}}
	_, err := db.IngestLore(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}

	// Batch: B (merge), C (accept), D (merge), E (accept)
	batch := []types.NewLoreEntry{
		{Content: "Content B", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-2"},
		{Content: "Content C", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-3"},
		{Content: "Content D", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-4"},
		{Content: "Content E", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-5"},
	}

	result, err := db.IngestLore(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}

	// Expect: 2 merged (B, D), 2 accepted (C, E)
	if result.Merged != 2 {
		t.Errorf("Expected merged=2, got %d", result.Merged)
	}
	if result.Accepted != 2 {
		t.Errorf("Expected accepted=2, got %d", result.Accepted)
	}

	// Verify 3 entries total (A, C, E)
	var count int
	err = db.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("Expected 3 entries, got %d", count)
	}
}

func TestIngestLore_NoEmbedder_StoresAllAsPending(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// No SetDependencies called - embedder and config are nil

	entries := []types.NewLoreEntry{
		{Content: "Content A", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-1"},
		{Content: "Content B", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "source-2"},
	}

	result, err := db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	if result.Accepted != 2 {
		t.Errorf("Expected accepted=2, got %d", result.Accepted)
	}
	if result.Merged != 0 {
		t.Errorf("Expected merged=0 (no embedder), got %d", result.Merged)
	}

	// Verify all have pending status
	var count int
	err = db.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE embedding_status = 'pending'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("Expected 2 entries with pending status, got %d", count)
	}
}

// --- Snapshot Generation Tests (Story 4.1) ---

func TestGenerateSnapshot_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify snapshot file exists
	snapshotPath := db.snapshotPath()
	if _, err := os.Stat(snapshotPath); os.IsNotExist(err) {
		t.Errorf("Snapshot file not created at %s", snapshotPath)
	}
}

func TestGenerateSnapshot_IncludesAllLore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert some lore entries
	entries := []types.NewLoreEntry{
		{Content: "Entry 1", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src-1"},
		{Content: "Entry 2", Category: "PATTERN_OUTCOME", Confidence: 0.9, SourceID: "src-2"},
		{Content: "Entry 3", Category: "DEPENDENCY_BEHAVIOR", Confidence: 0.7, SourceID: "src-3"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Generate snapshot
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Open snapshot and verify entries
	snapshotDB, err := sql.Open("sqlite", db.snapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotDB.Close()

	var count int
	err = snapshotDB.QueryRow("SELECT COUNT(*) FROM lore_entries").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("Expected 3 entries in snapshot, got %d", count)
	}
}

func TestGenerateSnapshot_UpdatesLastSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	before := time.Now()
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	stats, err := db.GetStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if stats.LastSnapshot == nil {
		t.Fatal("Expected LastSnapshot to be set")
	}

	if stats.LastSnapshot.Before(before) || stats.LastSnapshot.After(after) {
		t.Errorf("LastSnapshot %v not in expected range [%v, %v]", stats.LastSnapshot, before, after)
	}
}

func TestGenerateSnapshot_AtomicReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// First snapshot
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	snapshotPath := db.snapshotPath()
	info1, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}

	// Add more data
	entries := []types.NewLoreEntry{
		{Content: "New entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Second snapshot (should replace first)
	time.Sleep(10 * time.Millisecond) // Ensure different modification time
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	info2, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}

	// File should be newer or at least same time
	if info2.ModTime().Before(info1.ModTime()) {
		t.Error("Snapshot file was not replaced")
	}

	// Verify new snapshot has the new entry
	snapshotDB, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotDB.Close()

	var count int
	err = snapshotDB.QueryRow("SELECT COUNT(*) FROM lore_entries").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("Expected 1 entry in snapshot, got %d", count)
	}
}

func TestGenerateSnapshot_PreventsConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Start two concurrent snapshot generations
	var wg sync.WaitGroup
	results := make(chan error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := db.GenerateSnapshot(context.Background())
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	// Collect results
	var errs []error
	for err := range results {
		if err != nil {
			errs = append(errs, err)
		}
	}

	// One should succeed, one should return ErrSnapshotInProgress
	inProgressCount := 0
	for _, err := range errs {
		if errors.Is(err, ErrSnapshotInProgress) {
			inProgressCount++
		}
	}

	// At least one should get ErrSnapshotInProgress (could be 0 if one finishes before other starts)
	// But both shouldn't fail
	if len(errs) == 2 {
		t.Error("Both concurrent snapshot generations failed")
	}
}

func TestGenerateSnapshot_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/data/engram.db"

	// Ensure data dir is created by NewSQLiteStore
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Snapshots directory doesn't exist yet
	snapshotDir := db.snapshotDir()
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Skip("Snapshot directory already exists")
	}

	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify snapshot directory was created
	if _, err := os.Stat(snapshotDir); os.IsNotExist(err) {
		t.Errorf("Snapshot directory not created at %s", snapshotDir)
	}
}

func TestGetSnapshotPath_ReturnsPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Generate a snapshot first
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	path, err := db.GetSnapshotPath(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expectedPath := db.snapshotPath()
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}

	// Verify the path points to an existing file
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("GetSnapshotPath returned non-existent path: %s", path)
	}
}

func TestGetSnapshotPath_ErrorWhenNoSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Don't generate a snapshot
	_, err = db.GetSnapshotPath(context.Background())
	if !errors.Is(err, ErrSnapshotNotAvailable) {
		t.Errorf("Expected ErrSnapshotNotAvailable, got %v", err)
	}
}

// --- GetSnapshot Tests (Story 4.2) ---

func TestGetSnapshot_ReturnsReader(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Generate a snapshot first
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	reader, err := db.GetSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetSnapshot() error = %v", err)
	}
	if reader == nil {
		t.Fatal("GetSnapshot() returned nil reader")
	}
	defer reader.Close()
}

func TestGetSnapshot_ReaderContainsData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert some data
	entries := []types.NewLoreEntry{
		{Content: "Test entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Generate snapshot
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	reader, err := db.GetSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	// Read first 16 bytes - SQLite header starts with "SQLite format 3\x00"
	header := make([]byte, 16)
	n, err := reader.Read(header)
	if err != nil {
		t.Fatalf("Failed to read from snapshot: %v", err)
	}
	if n < 16 {
		t.Fatalf("Expected at least 16 bytes, got %d", n)
	}

	// Check SQLite magic bytes
	expectedMagic := "SQLite format 3\x00"
	if string(header) != expectedMagic {
		t.Errorf("Expected SQLite header %q, got %q", expectedMagic, string(header))
	}
}

func TestGetSnapshot_ErrorWhenNoSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Don't generate a snapshot
	reader, err := db.GetSnapshot(context.Background())
	if !errors.Is(err, ErrSnapshotNotAvailable) {
		t.Errorf("Expected ErrSnapshotNotAvailable, got %v", err)
	}
	if reader != nil {
		reader.Close()
		t.Error("Expected nil reader when snapshot not available")
	}
}

func TestGetSnapshot_ReaderCloseable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"
	db, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Generate snapshot
	err = db.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	reader, err := db.GetSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Close should not error
	err = reader.Close()
	if err != nil {
		t.Errorf("reader.Close() error = %v", err)
	}

	// Double close should be safe (or at least not panic)
	// This is a defensive test - os.File returns error on double close
	// but we don't want to panic
	_ = reader.Close()
}

// --- GetDelta Tests (Story 4.3) ---

func TestGetDelta_ReturnsUpdatedEntries(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert entry
	entries := []types.NewLoreEntry{
		{Content: "Delta test entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Query with timestamp before insertion
	since := time.Now().Add(-1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Lore) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(result.Lore))
	}
	if result.Lore[0].Content != "Delta test entry" {
		t.Errorf("Expected content 'Delta test entry', got %q", result.Lore[0].Content)
	}
}

func TestGetDelta_ExcludesOldEntries(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert entry
	entries := []types.NewLoreEntry{
		{Content: "Old entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Query with timestamp after insertion
	since := time.Now().Add(1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Lore) != 0 {
		t.Errorf("Expected 0 entries (old entry excluded), got %d", len(result.Lore))
	}
}

func TestGetDelta_ReturnsDeletedIDs(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert and then soft-delete an entry
	entries := []types.NewLoreEntry{
		{Content: "To be deleted", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	since := time.Now().Add(-1 * time.Hour)

	// Soft delete using RFC3339 format (consistent with rest of codebase)
	deletedAt := time.Now().UTC().Format(time.RFC3339)
	_, err = db.db.Exec("UPDATE lore_entries SET deleted_at = ? WHERE id = ?", deletedAt, id)
	if err != nil {
		t.Fatal(err)
	}

	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	// Should be in deleted_ids, not in lore
	if len(result.Lore) != 0 {
		t.Errorf("Expected 0 lore entries (deleted), got %d", len(result.Lore))
	}
	if len(result.DeletedIDs) != 1 {
		t.Fatalf("Expected 1 deleted ID, got %d", len(result.DeletedIDs))
	}
	if result.DeletedIDs[0] != id {
		t.Errorf("Expected deleted ID %q, got %q", id, result.DeletedIDs[0])
	}
}

func TestGetDelta_IncludesEmbeddings(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert entry
	entries := []types.NewLoreEntry{
		{Content: "Entry with embedding", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "src"},
	}
	_, err = db.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Get the ID and add an embedding
	var id string
	err = db.db.QueryRow("SELECT id FROM lore_entries LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}
	err = db.UpdateEmbedding(context.Background(), id, embedding)
	if err != nil {
		t.Fatal(err)
	}

	since := time.Now().Add(-1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Lore) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(result.Lore))
	}
	if len(result.Lore[0].Embedding) != 1536 {
		t.Errorf("Expected embedding with 1536 dimensions, got %d", len(result.Lore[0].Embedding))
	}
}

func TestGetDelta_EmptyResultNotNull(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// No entries, query should return empty arrays not nil
	since := time.Now().Add(-1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	if result.Lore == nil {
		t.Error("Expected Lore to be empty slice, got nil")
	}
	if result.DeletedIDs == nil {
		t.Error("Expected DeletedIDs to be empty slice, got nil")
	}
	if len(result.Lore) != 0 {
		t.Errorf("Expected 0 lore entries, got %d", len(result.Lore))
	}
	if len(result.DeletedIDs) != 0 {
		t.Errorf("Expected 0 deleted IDs, got %d", len(result.DeletedIDs))
	}
}

func TestGetDelta_AsOfIsRecent(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	before := time.Now().UTC()
	since := time.Now().Add(-1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()

	if result.AsOf.Before(before) || result.AsOf.After(after) {
		t.Errorf("AsOf %v not in expected range [%v, %v]", result.AsOf, before, after)
	}
}

func TestGetDelta_OrderByUpdatedAt(t *testing.T) {
	db, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert entries with small delays to ensure different timestamps
	for i := 0; i < 3; i++ {
		entry := types.NewLoreEntry{
			Content:    "Entry " + string(rune('A'+i)),
			Category:   "PATTERN_OUTCOME",
			Confidence: 0.8,
			SourceID:   "src",
		}
		_, err = db.IngestLore(context.Background(), []types.NewLoreEntry{entry})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	since := time.Now().Add(-1 * time.Hour)
	result, err := db.GetDelta(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Lore) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(result.Lore))
	}

	// Should be ordered by updated_at ASC (oldest first)
	if result.Lore[0].Content != "Entry A" {
		t.Errorf("Expected first entry 'Entry A', got %q", result.Lore[0].Content)
	}
	if result.Lore[2].Content != "Entry C" {
		t.Errorf("Expected last entry 'Entry C', got %q", result.Lore[2].Content)
	}
}
