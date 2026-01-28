package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
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
