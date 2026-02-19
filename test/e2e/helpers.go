package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/api"
	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/plugin/recall"
	"github.com/hyperengineering/engram/internal/store"
	engramsync "github.com/hyperengineering/engram/internal/sync"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Fixture Types ---

type recallFixtureEntry struct {
	Content    string   `json:"content"`
	Context    string   `json:"context,omitempty"`
	Category   string   `json:"category"`
	Confidence float64  `json:"confidence"`
	Sources    []string `json:"sources,omitempty"`
}

type scenarioFixture struct {
	Description   string                 `json:"description"`
	SharedEntryID string                 `json:"shared_entry_id,omitempty"`
	ClientA       map[string]interface{} `json:"client_a,omitempty"`
	ClientB       map[string]interface{} `json:"client_b,omitempty"`
	Entries       []recallFixtureEntry   `json:"entries,omitempty"`
}

type changeLogRow struct {
	Sequence   int64
	TableName  string
	EntityID   string
	Operation  string
	Payload    []byte
	SourceID   string
	CreatedAt  time.Time
	ReceivedAt time.Time
}

type loreRow struct {
	ID         string
	Content    string
	Category   string
	Confidence float64
	SourceID   string
	DeletedAt  *time.Time
}

// --- Fixture Loading ---

func fixturesDir() string {
	if dir := os.Getenv("TEST_FIXTURES_DIR"); dir != "" {
		return dir
	}
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "fixtures")
}

func loadRecallFixture(t *testing.T, name string) []recallFixtureEntry {
	t.Helper()
	path := filepath.Join(fixturesDir(), "recall", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load recall fixture %s: %v", name, err)
	}
	var entries []recallFixtureEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse recall fixture %s: %v", name, err)
	}
	return entries
}

func loadScenario(t *testing.T, name string) scenarioFixture {
	t.Helper()
	path := filepath.Join(fixturesDir(), "scenarios", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load scenario %s: %v", name, err)
	}
	var s scenarioFixture
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse scenario %s: %v", name, err)
	}
	return s
}

// --- DB Inspection ---

func serverChangeLogEntries(t *testing.T, db *sql.DB) []changeLogRow {
	t.Helper()
	rows, err := db.Query("SELECT sequence, table_name, entity_id, operation, payload, source_id, created_at, received_at FROM change_log ORDER BY sequence")
	if err != nil {
		t.Fatalf("query change_log: %v", err)
	}
	defer rows.Close()

	var result []changeLogRow
	for rows.Next() {
		var r changeLogRow
		var createdAt, receivedAt string
		if err := rows.Scan(&r.Sequence, &r.TableName, &r.EntityID, &r.Operation, &r.Payload, &r.SourceID, &createdAt, &receivedAt); err != nil {
			t.Fatalf("scan change_log row: %v", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		r.ReceivedAt, _ = time.Parse(time.RFC3339Nano, receivedAt)
		result = append(result, r)
	}
	return result
}

func serverLoreCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count); err != nil {
		t.Fatalf("count lore_entries: %v", err)
	}
	return count
}

func clientSyncMeta(t *testing.T, db *sql.DB, key string) string {
	t.Helper()
	var value string
	err := db.QueryRow("SELECT value FROM sync_meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("query sync_meta key=%s: %v", key, err)
	}
	return value
}

func assertLoreSetsEqual(t *testing.T, expected, actual []loreRow) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("lore set size mismatch: expected %d, got %d", len(expected), len(actual))
	}
	expectedMap := make(map[string]loreRow)
	for _, e := range expected {
		expectedMap[e.ID] = e
	}
	for _, a := range actual {
		e, ok := expectedMap[a.ID]
		if !ok {
			t.Errorf("unexpected lore entry: %s", a.ID)
			continue
		}
		if a.Content != e.Content {
			t.Errorf("lore %s content mismatch: expected %q, got %q", a.ID, e.Content, a.Content)
		}
		if a.Category != e.Category {
			t.Errorf("lore %s category mismatch: expected %q, got %q", a.ID, e.Category, a.Category)
		}
	}
}

// --- Sync Test Environment Setup ---

func setupSyncTestEnv(t *testing.T) (*multistore.StoreManager, *api.Handler, *multistore.ManagedStore) {
	t.Helper()

	// Register recall plugin (idempotent, may already be registered)
	func() {
		defer func() { recover() }()
		plugin.Register(recall.New())
	}()

	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	t.Cleanup(func() { manager.Close() })

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "test-store", "recall", "Test store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	// Set schema version to 2
	if err := managed.Store.SetSyncMeta(ctx, "schema_version", "2"); err != nil {
		t.Fatalf("SetSyncMeta() error = %v", err)
	}

	defaultStore := &noopStore{}
	embedder := &noopEmbedder{}
	handler := api.NewHandler(defaultStore, manager, embedder, nil, "test-api-key", "1.0.0")

	return manager, handler, managed
}

// --- Noop Implementations ---

type noopEmbedder struct{}

func (e *noopEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

func (e *noopEmbedder) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

func (e *noopEmbedder) ModelName() string {
	return "noop"
}

var _ embedding.Embedder = (*noopEmbedder)(nil)

type noopStore struct{}

func (s *noopStore) IngestLore(_ context.Context, _ []types.NewLoreEntry) (*types.IngestResult, error) {
	return &types.IngestResult{}, nil
}
func (s *noopStore) FindSimilar(_ context.Context, _ []float32, _ string, _ float64) ([]types.SimilarEntry, error) {
	return nil, nil
}
func (s *noopStore) MergeLore(_ context.Context, _ string, _ types.NewLoreEntry) error { return nil }
func (s *noopStore) GetLore(_ context.Context, _ string) (*types.LoreEntry, error) {
	return nil, nil
}
func (s *noopStore) DeleteLore(_ context.Context, _, _ string) error { return nil }
func (s *noopStore) GetMetadata(_ context.Context) (*types.StoreMetadata, error) {
	return &types.StoreMetadata{}, nil
}
func (s *noopStore) GetSnapshot(_ context.Context) (io.ReadCloser, error) {
	return nil, nil
}
func (s *noopStore) GetDelta(_ context.Context, _ time.Time) (*types.DeltaResult, error) {
	return &types.DeltaResult{}, nil
}
func (s *noopStore) GenerateSnapshot(_ context.Context) error { return nil }
func (s *noopStore) GetSnapshotPath(_ context.Context) (string, error) {
	return "", nil
}
func (s *noopStore) RecordFeedback(_ context.Context, _ []types.FeedbackEntry) (*types.FeedbackResult, error) {
	return &types.FeedbackResult{}, nil
}
func (s *noopStore) DecayConfidence(_ context.Context, _ time.Time, _ float64) (int64, error) {
	return 0, nil
}
func (s *noopStore) SetLastDecay(_ time.Time)    {}
func (s *noopStore) GetLastDecay() *time.Time    { return nil }
func (s *noopStore) GetPendingEmbeddings(_ context.Context, _ int) ([]types.LoreEntry, error) {
	return nil, nil
}
func (s *noopStore) UpdateEmbedding(_ context.Context, _ string, _ []float32) error { return nil }
func (s *noopStore) MarkEmbeddingFailed(_ context.Context, _ string) error          { return nil }
func (s *noopStore) GetStats(_ context.Context) (*types.StoreStats, error) {
	return &types.StoreStats{}, nil
}
func (s *noopStore) GetExtendedStats(_ context.Context) (*types.ExtendedStats, error) {
	return &types.ExtendedStats{}, nil
}
func (s *noopStore) AppendChangeLog(_ context.Context, _ *engramsync.ChangeLogEntry) (int64, error) {
	return 0, nil
}
func (s *noopStore) AppendChangeLogBatch(_ context.Context, _ []engramsync.ChangeLogEntry) (int64, error) {
	return 0, nil
}
func (s *noopStore) GetChangeLogAfter(_ context.Context, _ int64, _ int) ([]engramsync.ChangeLogEntry, error) {
	return nil, nil
}
func (s *noopStore) GetLatestSequence(_ context.Context) (int64, error) { return 0, nil }
func (s *noopStore) CheckPushIdempotency(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}
func (s *noopStore) RecordPushIdempotency(_ context.Context, _, _ string, _ []byte, _ time.Duration) error {
	return nil
}
func (s *noopStore) CleanExpiredIdempotency(_ context.Context) (int64, error) { return 0, nil }
func (s *noopStore) GetSyncMeta(_ context.Context, _ string) (string, error) { return "", nil }
func (s *noopStore) SetSyncMeta(_ context.Context, _, _ string) error        { return nil }
func (s *noopStore) CompactChangeLog(_ context.Context, _ time.Time, _ string) (int64, int64, error) {
	return 0, 0, nil
}
func (s *noopStore) SetLastCompaction(_ context.Context, _ int64, _ time.Time) error { return nil }
func (s *noopStore) UpsertRow(_ context.Context, _ string, _ string, _ []byte) error { return nil }
func (s *noopStore) DeleteRow(_ context.Context, _ string, _ string) error            { return nil }
func (s *noopStore) QueueEmbedding(_ context.Context, _ string) error                 { return nil }
func (s *noopStore) Close() error                                                     { return nil }

var _ store.Store = (*noopStore)(nil)

// --- HTTP Helpers ---

func makePushBody(t *testing.T, req engramsync.PushRequest) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal push request: %v", err)
	}
	return bytes.NewBuffer(b)
}

func validLorePayload(t *testing.T, id string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"content":    "Test lore content for " + id,
		"context":    "Test context",
		"category":   "TESTING_STRATEGY",
		"confidence": 0.5,
		"source_id":  "test-source",
		"sources":    []string{"test-source"},
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return json.RawMessage(b)
}

func validLorePayloadWithSource(t *testing.T, id, sourceID string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"content":    "Test lore content for " + id,
		"context":    "Test context",
		"category":   "TESTING_STRATEGY",
		"confidence": 0.5,
		"source_id":  sourceID,
		"sources":    []string{sourceID},
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return json.RawMessage(b)
}

func fixtureToPayload(t *testing.T, id string, entry recallFixtureEntry) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"content":    entry.Content,
		"context":    entry.Context,
		"category":   entry.Category,
		"confidence": entry.Confidence,
		"source_id":  "fixture-source",
		"sources":    entry.Sources,
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture payload: %v", err)
	}
	return json.RawMessage(b)
}

func pushEntries(t *testing.T, router http.Handler, storeID string, n int, sourceID string) engramsync.PushResponse {
	t.Helper()
	entries := make([]engramsync.ChangeLogEntry, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("entry-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   validLorePayloadWithSource(t, id, sourceID),
		}
	}
	req := engramsync.PushRequest{
		PushID:        fmt.Sprintf("push-%s-%d-%d", sourceID, n, time.Now().UnixNano()),
		SourceID:      sourceID,
		SchemaVersion: 2,
		Entries:       entries,
	}
	url := fmt.Sprintf("/api/v1/stores/%s/sync/push", storeID)
	httpReq := httptest.NewRequest(http.MethodPost, url, makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("pushEntries: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	return resp
}

func deltaRequest(t *testing.T, router http.Handler, storeID string, after int64, limit int) engramsync.DeltaResponse {
	t.Helper()
	url := fmt.Sprintf("/api/v1/stores/%s/sync/delta?after=%d", storeID, after)
	if limit > 0 {
		url += fmt.Sprintf("&limit=%d", limit)
	}
	httpReq := httptest.NewRequest(http.MethodGet, url, nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("deltaRequest: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp engramsync.DeltaResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode delta response: %v", err)
	}
	return resp
}
