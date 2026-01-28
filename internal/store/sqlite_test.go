package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/types"
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
