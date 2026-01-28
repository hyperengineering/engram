package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Mock Implementations for Testing ---

// mockStore implements store.Store interface for testing
type mockStore struct {
	stats    *types.StoreStats
	statsErr error
}

func (m *mockStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	return &types.IngestResult{Accepted: len(entries)}, nil
}

func (m *mockStore) FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.LoreEntry, error) {
	return nil, nil
}

func (m *mockStore) MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error {
	return nil
}

func (m *mockStore) GetLore(ctx context.Context, id string) (*types.LoreEntry, error) {
	return nil, store.ErrNotFound
}

func (m *mockStore) GetMetadata(ctx context.Context) (*types.StoreMetadata, error) {
	return nil, nil
}

func (m *mockStore) GetSnapshot(ctx context.Context) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockStore) GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error) {
	return nil, nil
}

func (m *mockStore) GenerateSnapshot(ctx context.Context) error {
	return nil
}

func (m *mockStore) GetSnapshotPath(ctx context.Context) (string, error) {
	return "", nil
}

func (m *mockStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	return nil, nil
}

func (m *mockStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	return 0, nil
}

func (m *mockStore) GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error) {
	return nil, nil
}

func (m *mockStore) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	return nil
}

func (m *mockStore) GetStats(ctx context.Context) (*types.StoreStats, error) {
	return m.stats, m.statsErr
}

func (m *mockStore) Close() error {
	return nil
}

// mockEmbedder implements the embedding.Embedder interface for testing
type mockEmbedder struct {
	model string
}

func (m *mockEmbedder) Embed(ctx context.Context, content string) ([]float32, error) {
	return nil, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, contents []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockEmbedder) ModelName() string {
	return m.model
}

// newTestHandler creates a Handler with minimal dependencies for health endpoint testing
func newTestHandler(s store.Store, embedder *mockEmbedder, apiKey, version string) *Handler {
	return &Handler{
		store:    s,
		embedder: embedder,
		apiKey:   apiKey,
		version:  version,
	}
}

// --- Health Endpoint Tests ---

func TestHealth_ReturnsHealthyStatus(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 0},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("status = %q, want %q", resp.Status, "healthy")
	}
}

func TestHealth_ReturnsCorrectJSONStructure(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 42},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	// Parse as raw JSON to check field names
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Check all 5 required fields are present with snake_case names
	requiredFields := []string{"status", "version", "embedding_model", "lore_count", "last_snapshot"}
	for _, field := range requiredFields {
		if _, ok := rawResp[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestHealth_LoreCountReflectsStoreValue(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 42},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.LoreCount != 42 {
		t.Errorf("lore_count = %d, want %d", resp.LoreCount, 42)
	}
}

func TestHealth_LastSnapshotNullWhenNone(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{
			LoreCount:    0,
			LastSnapshot: nil, // No snapshot
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	// Parse raw to check null value
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if rawResp["last_snapshot"] != nil {
		t.Errorf("last_snapshot = %v, want null", rawResp["last_snapshot"])
	}
}

func TestHealth_LastSnapshotReturnsTimestamp(t *testing.T) {
	snapshotTime := time.Date(2026, 1, 28, 10, 30, 0, 0, time.UTC)
	store := &mockStore{
		stats: &types.StoreStats{
			LoreCount:    10,
			LastSnapshot: &snapshotTime,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.LastSnapshot == nil {
		t.Fatal("last_snapshot is nil, want timestamp")
	}

	if !resp.LastSnapshot.Equal(snapshotTime) {
		t.Errorf("last_snapshot = %v, want %v", resp.LastSnapshot, snapshotTime)
	}
}

func TestHealth_NoAuthRequired(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 0},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	// Request WITHOUT Authorization header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	// Should return 200, not 401
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (no auth should be required)", w.Code, http.StatusOK)
	}
}

func TestHealth_ContentTypeJSON(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 0},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}
}

func TestHealth_EmbeddingModelFromEmbedder(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 0},
	}
	embedder := &mockEmbedder{model: "text-embedding-ada-002"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.EmbeddingModel != "text-embedding-ada-002" {
		t.Errorf("embedding_model = %q, want %q", resp.EmbeddingModel, "text-embedding-ada-002")
	}
}

func TestHealth_VersionFromConfig(t *testing.T) {
	store := &mockStore{
		stats: &types.StoreStats{LoreCount: 0},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "2.5.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Version != "2.5.0" {
		t.Errorf("version = %q, want %q", resp.Version, "2.5.0")
	}
}

func TestHealth_StoreErrorReturns500(t *testing.T) {
	store := &mockStore{
		stats:    nil,
		statsErr: context.DeadlineExceeded, // Simulate store error
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(store, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d for store error", w.Code, http.StatusInternalServerError)
	}
}
