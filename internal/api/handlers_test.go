package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Mock Implementations for Testing ---

// mockStore implements store.Store interface for testing
type mockStore struct {
	stats            *types.StoreStats
	statsErr         error
	extendedStats    *types.ExtendedStats
	extendedStatsErr error
	ingestErr        error
	ingestResult     *types.IngestResult // Custom ingest result (for dedup testing)
	ingestCalls      int
	lastEntries      []types.NewLoreEntry
	snapshotReader   io.ReadCloser
	snapshotErr      error
	deltaResult      *types.DeltaResult
	deltaErr         error
	feedbackResult   *types.FeedbackResult
	feedbackErr      error
	deleteErr        error
}

func (m *mockStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	m.ingestCalls++
	m.lastEntries = entries
	if m.ingestErr != nil {
		return nil, m.ingestErr
	}
	if m.ingestResult != nil {
		return m.ingestResult, nil
	}
	return &types.IngestResult{Accepted: len(entries)}, nil
}

func (m *mockStore) FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error) {
	return nil, nil
}

func (m *mockStore) MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error {
	return nil
}

func (m *mockStore) GetLore(ctx context.Context, id string) (*types.LoreEntry, error) {
	return nil, store.ErrNotFound
}

func (m *mockStore) DeleteLore(ctx context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return nil
}

func (m *mockStore) GetMetadata(ctx context.Context) (*types.StoreMetadata, error) {
	return nil, nil
}

func (m *mockStore) GetSnapshot(ctx context.Context) (io.ReadCloser, error) {
	return m.snapshotReader, m.snapshotErr
}

func (m *mockStore) GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error) {
	return m.deltaResult, m.deltaErr
}

func (m *mockStore) GenerateSnapshot(ctx context.Context) error {
	return nil
}

func (m *mockStore) GetSnapshotPath(ctx context.Context) (string, error) {
	return "", nil
}

func (m *mockStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	if m.feedbackErr != nil {
		return nil, m.feedbackErr
	}
	if m.feedbackResult != nil {
		return m.feedbackResult, nil
	}
	return &types.FeedbackResult{Updates: []types.FeedbackResultUpdate{}}, nil
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

func (m *mockStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
	return nil
}

func (m *mockStore) GetStats(ctx context.Context) (*types.StoreStats, error) {
	return m.stats, m.statsErr
}

func (m *mockStore) GetExtendedStats(ctx context.Context) (*types.ExtendedStats, error) {
	return m.extendedStats, m.extendedStatsErr
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

// --- Health Endpoint Tests with Store Parameter (Story 7.3) ---

func TestHealth_WithStoreParameter(t *testing.T) {
	// Use real store manager with temp directory
	tmpDir := t.TempDir()
	mgr, err := multistore.NewStoreManager(tmpDir)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer mgr.Close()

	// Create test store
	_, err = mgr.CreateStore(context.Background(), "test-store", "Test store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	defaultStore := &mockStore{stats: &types.StoreStats{LoreCount: 100}}
	handler := NewHandler(defaultStore, mgr, embedder, "test-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health?store=test-store", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Lore count from newly created store should be 0
	if resp.LoreCount != 0 {
		t.Errorf("lore_count = %d, want 0 (from new test store)", resp.LoreCount)
	}

	if resp.StoreID != "test-store" {
		t.Errorf("store_id = %q, want %q", resp.StoreID, "test-store")
	}
}

func TestHealth_WithStoreParameter_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := multistore.NewStoreManager(tmpDir)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer mgr.Close()

	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	defaultStore := &mockStore{stats: &types.StoreStats{LoreCount: 100}}
	handler := NewHandler(defaultStore, mgr, embedder, "test-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health?store=nonexistent", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHealth_WithStoreParameter_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := multistore.NewStoreManager(tmpDir)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer mgr.Close()

	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	defaultStore := &mockStore{stats: &types.StoreStats{LoreCount: 100}}
	handler := NewHandler(defaultStore, mgr, embedder, "test-key", "1.0.0")

	// INVALID has uppercase letters, which is invalid
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health?store=INVALID", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHealth_WithoutStoreParameter_ReturnsDefault(t *testing.T) {
	defaultStore := &mockStore{stats: &types.StoreStats{LoreCount: 100}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(defaultStore, embedder, "api-key", "1.0.0")

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

	if resp.LoreCount != 100 {
		t.Errorf("lore_count = %d, want 100 (from default store)", resp.LoreCount)
	}

	// store_id should be omitted when not specified
	if resp.StoreID != "" {
		t.Errorf("store_id = %q, want empty (omitted)", resp.StoreID)
	}
}

// --- Stats Endpoint Tests ---

func TestStats_ReturnsExtendedStats(t *testing.T) {
	now := time.Now().UTC()
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			TotalLore:   100,
			ActiveLore:  95,
			DeletedLore: 5,
			EmbeddingStats: types.EmbeddingStats{
				Complete: 90,
				Pending:  3,
				Failed:   2,
			},
			CategoryStats: map[string]int64{
				"PATTERN_OUTCOME":        50,
				"ARCHITECTURAL_DECISION": 30,
			},
			QualityStats: types.QualityStats{
				AverageConfidence:   0.72,
				ValidatedCount:      40,
				HighConfidenceCount: 25,
				LowConfidenceCount:  10,
			},
			UniqueSourceCount: 5,
			LastSnapshot:      &now,
			StatsAsOf:         now,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.ExtendedStats
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.TotalLore != 100 {
		t.Errorf("total_lore = %d, want 100", resp.TotalLore)
	}
	if resp.ActiveLore != 95 {
		t.Errorf("active_lore = %d, want 95", resp.ActiveLore)
	}
	if resp.EmbeddingStats.Complete != 90 {
		t.Errorf("embedding_stats.complete = %d, want 90", resp.EmbeddingStats.Complete)
	}
	if resp.QualityStats.AverageConfidence != 0.72 {
		t.Errorf("quality_stats.average_confidence = %f, want 0.72", resp.QualityStats.AverageConfidence)
	}
}

func TestStats_ReturnsCorrectJSONStructure(t *testing.T) {
	now := time.Now().UTC()
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			TotalLore:         10,
			ActiveLore:        10,
			DeletedLore:       0,
			EmbeddingStats:    types.EmbeddingStats{Complete: 10},
			CategoryStats:     map[string]int64{"PATTERN_OUTCOME": 10},
			QualityStats:      types.QualityStats{AverageConfidence: 0.5},
			UniqueSourceCount: 1,
			StatsAsOf:         now,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	// Parse as raw JSON to check field names
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Check all required fields are present with snake_case names
	requiredFields := []string{
		"total_lore", "active_lore", "deleted_lore",
		"embedding_stats", "category_stats", "quality_stats",
		"unique_source_count", "stats_as_of",
	}
	for _, field := range requiredFields {
		if _, ok := rawResp[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestStats_NoAuthRequired(t *testing.T) {
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			CategoryStats: map[string]int64{},
			StatsAsOf:     time.Now().UTC(),
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "test-api-key", "1.0.0")

	// Create request WITHOUT Authorization header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	// Direct handler call (bypassing middleware) - endpoint should work
	handler.Stats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (stats should not require auth)", w.Code, http.StatusOK)
	}
}

func TestStats_ContentTypeJSON(t *testing.T) {
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			CategoryStats: map[string]int64{},
			StatsAsOf:     time.Now().UTC(),
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

func TestStats_StoreErrorReturns500(t *testing.T) {
	s := &mockStore{
		extendedStats:    nil,
		extendedStatsErr: errors.New("database error"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d for store error", w.Code, http.StatusInternalServerError)
	}
}

func TestStats_EmptyCategoryStatsIsObject(t *testing.T) {
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			CategoryStats: map[string]int64{}, // Empty but not nil
			StatsAsOf:     time.Now().UTC(),
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	// Parse raw JSON
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// category_stats should be {} not null
	catStats, ok := rawResp["category_stats"]
	if !ok {
		t.Fatal("category_stats field missing")
	}
	catMap, ok := catStats.(map[string]any)
	if !ok {
		t.Errorf("category_stats should be an object, got %T", catStats)
	}
	if len(catMap) != 0 {
		t.Errorf("category_stats should be empty object, got %v", catMap)
	}
}

// --- IngestLore Endpoint Tests ---

func TestIngestLore_ValidBatch(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [
			{"content": "First insight", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.7},
			{"content": "Second insight", "category": "PATTERN_OUTCOME", "confidence": 0.8},
			{"content": "Third insight", "context": "optional context", "category": "ARCHITECTURAL_DECISION", "confidence": 0.9}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Accepted != 3 {
		t.Errorf("accepted = %d, want 3", resp.Accepted)
	}
	if resp.Rejected != 0 {
		t.Errorf("rejected = %d, want 0", resp.Rejected)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("errors = %v, want empty", resp.Errors)
	}
}

func TestIngestLore_MissingSourceID(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "",
		"lore": [{"content": "valid", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if problem.Status != http.StatusUnprocessableEntity {
		t.Errorf("problem.status = %d, want %d", problem.Status, http.StatusUnprocessableEntity)
	}

	hasSourceIDError := false
	for _, e := range problem.Errors {
		if e.Field == "source_id" {
			hasSourceIDError = true
			break
		}
	}
	if !hasSourceIDError {
		t.Errorf("expected source_id error, got: %v", problem.Errors)
	}
}

func TestIngestLore_ContentTooLong(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	longContent := strings.Repeat("a", 4001)
	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "` + longContent + `", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	// Partial acceptance: entry rejected, response is 200
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (partial acceptance)", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Rejected != 1 {
		t.Errorf("rejected = %d, want 1", resp.Rejected)
	}
	if resp.Accepted != 0 {
		t.Errorf("accepted = %d, want 0", resp.Accepted)
	}

	hasLengthError := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "4000") {
			hasLengthError = true
			break
		}
	}
	if !hasLengthError {
		t.Errorf("expected length error in errors, got: %v", resp.Errors)
	}
}

func TestIngestLore_ContentNullBytes(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// JSON with null byte escaped as \u0000 (valid JSON that decodes to string with null byte)
	body := `{"source_id": "devcontainer-abc123", "lore": [{"content": "hello\u0000world", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	// Partial acceptance: entry rejected, response is 200
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (partial acceptance)", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Rejected != 1 {
		t.Errorf("rejected = %d, want 1", resp.Rejected)
	}

	hasNullError := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "null") {
			hasNullError = true
			break
		}
	}
	if !hasNullError {
		t.Errorf("expected null byte error in errors, got: %v", resp.Errors)
	}
}

func TestIngestLore_InvalidCategory(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "valid content", "category": "INVALID_CATEGORY", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (partial acceptance)", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Rejected != 1 {
		t.Errorf("rejected = %d, want 1", resp.Rejected)
	}

	hasCategoryError := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "category") && strings.Contains(e, "must be one of") {
			hasCategoryError = true
			break
		}
	}
	if !hasCategoryError {
		t.Errorf("expected category error in errors, got: %v", resp.Errors)
	}
}

func TestIngestLore_ConfidenceOutOfRange(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [
			{"content": "below min", "category": "DEPENDENCY_BEHAVIOR", "confidence": -0.1},
			{"content": "above max", "category": "DEPENDENCY_BEHAVIOR", "confidence": 1.1}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (partial acceptance)", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Rejected != 2 {
		t.Errorf("rejected = %d, want 2", resp.Rejected)
	}
}

func TestIngestLore_PartialBatch(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [
			{"content": "valid first", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.7},
			{"content": "valid second", "category": "PATTERN_OUTCOME", "confidence": 0.8},
			{"content": "", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Accepted != 2 {
		t.Errorf("accepted = %d, want 2", resp.Accepted)
	}
	if resp.Rejected != 1 {
		t.Errorf("rejected = %d, want 1", resp.Rejected)
	}
	if len(resp.Errors) == 0 {
		t.Error("expected errors for rejected entry")
	}
}

func TestIngestLore_EmptyLoreArray(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{"source_id": "devcontainer-abc123", "lore": []}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestIngestLore_BatchTooLarge(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// Build a batch with 51 entries
	var loreEntries []string
	for i := 0; i < 51; i++ {
		loreEntries = append(loreEntries, `{"content": "entry", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}`)
	}
	body := `{"source_id": "devcontainer-abc123", "lore": [` + strings.Join(loreEntries, ",") + `]}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var problem ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	hasBatchError := false
	for _, e := range problem.Errors {
		if e.Field == "lore" && strings.Contains(e.Message, "50") {
			hasBatchError = true
			break
		}
	}
	if !hasBatchError {
		t.Errorf("expected batch size error, got: %v", problem.Errors)
	}
}

func TestIngestLore_InvalidJSON(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{invalid json`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if problem.Status != http.StatusBadRequest {
		t.Errorf("problem.status = %d, want %d", problem.Status, http.StatusBadRequest)
	}
}

func TestIngestLore_StoreError(t *testing.T) {
	s := &mockStore{
		stats:     &types.StoreStats{},
		ingestErr: errors.New("database connection failed"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "valid content", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestIngestLore_ResponseContentType(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "valid", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q for success response", contentType, "application/json")
	}
}

func TestIngestLore_ErrorsArrayNeverNull(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "valid", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	// Parse raw JSON to check errors is [] not null
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	errors, ok := rawResp["errors"].([]any)
	if !ok {
		t.Errorf("errors should be an array, got: %T", rawResp["errors"])
	}
	if errors == nil {
		t.Error("errors should be [] not null")
	}
}

func TestIngestLore_ContextTooLong(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	longContext := strings.Repeat("a", 1001)
	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [{"content": "valid content", "context": "` + longContext + `", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (partial acceptance)", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Rejected != 1 {
		t.Errorf("rejected = %d, want 1", resp.Rejected)
	}

	hasContextError := false
	for _, e := range resp.Errors {
		if strings.Contains(e, "context") && strings.Contains(e, "1000") {
			hasContextError = true
			break
		}
	}
	if !hasContextError {
		t.Errorf("expected context length error, got: %v", resp.Errors)
	}
}

// TestIngestLore_MergedCountFromStore tests BUG-001 fix:
// Verifies that when the store returns merged entries, the API response
// correctly reflects that count instead of hardcoding 0.
func TestIngestLore_MergedCountFromStore(t *testing.T) {
	// Mock store returns a result where 1 entry was accepted and 1 was merged
	// (simulating deduplication scenario)
	s := &mockStore{
		stats: &types.StoreStats{},
		ingestResult: &types.IngestResult{
			Accepted: 1,
			Merged:   1,
			Rejected: 0,
			Errors:   []string{},
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// Send 2 valid entries - store will report 1 accepted, 1 merged
	body := `{
		"source_id": "devcontainer-abc123",
		"lore": [
			{"content": "First insight about Go patterns", "category": "PATTERN_OUTCOME", "confidence": 0.8},
			{"content": "Similar insight about Go patterns", "category": "PATTERN_OUTCOME", "confidence": 0.7}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.IngestLore(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.IngestResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// BUG-001: This assertion will FAIL with current code (hardcoded 0)
	if resp.Merged != 1 {
		t.Errorf("merged = %d, want 1 (BUG-001: handler must use result.Merged from store)", resp.Merged)
	}

	if resp.Accepted != 1 {
		t.Errorf("accepted = %d, want 1", resp.Accepted)
	}
}

// --- Snapshot Endpoint Tests (Story 4.2) ---

// trackingReadCloser wraps an io.ReadCloser and tracks if Close was called
type trackingReadCloser struct {
	io.ReadCloser
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return t.ReadCloser.Close()
}

func TestSnapshot_ServesFile(t *testing.T) {
	// Create a mock reader with test data
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(strings.NewReader(string(testData)))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
		snapshotErr:    nil,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/octet-stream")
	}

	if !strings.HasPrefix(w.Body.String(), "SQLite format 3") {
		t.Errorf("body doesn't contain expected data, got: %q", w.Body.String())
	}
}

func TestSnapshot_503WhenNoSnapshot(t *testing.T) {
	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: nil,
		snapshotErr:    store.ErrSnapshotNotAvailable,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "60")
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if problem.Status != http.StatusServiceUnavailable {
		t.Errorf("problem.status = %d, want %d", problem.Status, http.StatusServiceUnavailable)
	}
}

func TestSnapshot_500OnStoreError(t *testing.T) {
	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: nil,
		snapshotErr:    errors.New("disk I/O error"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestSnapshot_ClosesReader(t *testing.T) {
	// Use tracking reader to verify Close is called
	testData := []byte("test data")
	baseReader := io.NopCloser(strings.NewReader(string(testData)))
	tracker := &trackingReadCloser{ReadCloser: baseReader}

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: tracker,
		snapshotErr:    nil,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	if !tracker.closed {
		t.Error("reader.Close() was not called")
	}
}

// --- Snapshot Integration Test (Story 4.2) ---

func TestSnapshotEndpoint_RoundTrip(t *testing.T) {
	// This test uses a real SQLiteStore to verify the full data flow
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"

	// Create real store
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()

	// Insert some test data
	entries := []types.NewLoreEntry{
		{Content: "Integration test entry 1", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "test-src"},
		{Content: "Integration test entry 2", Category: "DEPENDENCY_BEHAVIOR", Confidence: 0.9, SourceID: "test-src"},
	}
	_, err = sqliteStore.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Generate snapshot
	err = sqliteStore.GenerateSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Create handler with real store
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(sqliteStore, nil, embedder, "api-key", "1.0.0")

	// Make request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	w := httptest.NewRecorder()

	handler.Snapshot(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/octet-stream")
	}

	// Verify the response is a valid SQLite database
	body := w.Body.Bytes()
	if len(body) < 16 {
		t.Fatalf("Response too short: %d bytes", len(body))
	}

	// Check SQLite magic bytes
	expectedMagic := "SQLite format 3\x00"
	if string(body[:16]) != expectedMagic {
		t.Errorf("Expected SQLite header, got: %q", body[:16])
	}

	// Verify the response has reasonable size (should contain our test data)
	if len(body) < 4096 {
		t.Logf("Snapshot size: %d bytes (SQLite minimum page size)", len(body))
	}
}

// --- Delta Endpoint Tests (Story 4.3) ---

func TestDelta_ReturnsEntries(t *testing.T) {
	asOf := time.Now().UTC()
	s := &mockStore{
		stats: &types.StoreStats{},
		deltaResult: &types.DeltaResult{
			Lore: []types.LoreEntry{
				{
					ID:       "test-id-1",
					Content:  "Test content",
					Category: "PATTERN_OUTCOME",
				},
			},
			DeletedIDs: []string{},
			AsOf:       asOf,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var result types.DeltaResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result.Lore) != 1 {
		t.Errorf("expected 1 lore entry, got %d", len(result.Lore))
	}
}

func TestDelta_400MissingSince(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// No since parameter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if !strings.Contains(problem.Detail, "since") {
		t.Errorf("problem.Detail should mention 'since', got: %q", problem.Detail)
	}
}

func TestDelta_400InvalidSince(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// Invalid since parameter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=not-a-timestamp", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if !strings.Contains(problem.Detail, "RFC3339") {
		t.Errorf("problem.Detail should mention 'RFC3339', got: %q", problem.Detail)
	}
}

func TestDelta_EmptyArraysNotNull(t *testing.T) {
	asOf := time.Now().UTC()
	s := &mockStore{
		stats: &types.StoreStats{},
		deltaResult: &types.DeltaResult{
			Lore:       []types.LoreEntry{},
			DeletedIDs: []string{},
			AsOf:       asOf,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Parse raw JSON to check arrays are [] not null
	var rawResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	lore, ok := rawResp["lore"].([]any)
	if !ok {
		t.Errorf("lore should be array, got: %T", rawResp["lore"])
	}
	if lore == nil {
		t.Error("lore should be [] not null")
	}

	deleted, ok := rawResp["deleted_ids"].([]any)
	if !ok {
		t.Errorf("deleted_ids should be array, got: %T", rawResp["deleted_ids"])
	}
	if deleted == nil {
		t.Error("deleted_ids should be [] not null")
	}
}

func TestDelta_IncludesAsOf(t *testing.T) {
	asOf := time.Date(2026, 1, 29, 15, 0, 0, 0, time.UTC)
	s := &mockStore{
		stats: &types.StoreStats{},
		deltaResult: &types.DeltaResult{
			Lore:       []types.LoreEntry{},
			DeletedIDs: []string{},
			AsOf:       asOf,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	var result types.DeltaResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.AsOf.IsZero() {
		t.Error("as_of should be present")
	}
	if !result.AsOf.Equal(asOf) {
		t.Errorf("as_of = %v, want %v", result.AsOf, asOf)
	}
}

func TestDelta_500OnStoreError(t *testing.T) {
	s := &mockStore{
		stats:    &types.StoreStats{},
		deltaErr: errors.New("database error"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

// --- Delta Integration Test (Story 4.3) ---

func TestDeltaEndpoint_RoundTrip(t *testing.T) {
	// This test uses a real SQLiteStore to verify the full data flow
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"

	// Create real store
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()

	// Record timestamp before inserting data
	sinceBefore := time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339)

	// Insert test data
	entries := []types.NewLoreEntry{
		{Content: "Delta integration test entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "test-src"},
	}
	_, err = sqliteStore.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Create handler with real store
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(sqliteStore, nil, embedder, "api-key", "1.0.0")

	// Make delta request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since="+sinceBefore, nil)
	w := httptest.NewRecorder()

	handler.Delta(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result types.DeltaResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result.Lore) != 1 {
		t.Errorf("expected 1 lore entry, got %d", len(result.Lore))
	}
	if result.Lore[0].Content != "Delta integration test entry" {
		t.Errorf("expected content 'Delta integration test entry', got %q", result.Lore[0].Content)
	}
	if result.AsOf.IsZero() {
		t.Error("as_of should be set")
	}
}

// --- Feedback Endpoint Tests (Story 5.1) ---

func TestFeedback_Success_Helpful(t *testing.T) {
	validationCount := 3
	s := &mockStore{
		stats: &types.StoreStats{},
		feedbackResult: &types.FeedbackResult{
			Updates: []types.FeedbackResultUpdate{
				{
					LoreID:             "01ARZ3NDEKTSV4RRFFQ69G5FAV",
					PreviousConfidence: 0.72,
					CurrentConfidence:  0.80,
					ValidationCount:    &validationCount,
				},
			},
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result types.FeedbackResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(result.Updates))
	}
	if result.Updates[0].LoreID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Errorf("lore_id = %q, want %q", result.Updates[0].LoreID, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	}
	if result.Updates[0].ValidationCount == nil {
		t.Error("validation_count should be present for helpful feedback")
	}
}

func TestFeedback_Success_Incorrect(t *testing.T) {
	s := &mockStore{
		stats: &types.StoreStats{},
		feedbackResult: &types.FeedbackResult{
			Updates: []types.FeedbackResultUpdate{
				{
					LoreID:             "01ARZ3NDEKTSV4RRFFQ69G5FAV",
					PreviousConfidence: 0.65,
					CurrentConfidence:  0.50,
				},
			},
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "incorrect"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result types.FeedbackResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Updates[0].ValidationCount != nil {
		t.Errorf("validation_count should be nil for incorrect feedback, got %d", *result.Updates[0].ValidationCount)
	}
}

func TestFeedback_LoreNotFound(t *testing.T) {
	s := &mockStore{
		stats:       &types.StoreStats{},
		feedbackErr: store.ErrNotFound,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestFeedback_InvalidJSON(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{invalid json`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestFeedback_MissingSourceID(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var problem ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	hasSourceIDError := false
	for _, e := range problem.Errors {
		if e.Field == "source_id" {
			hasSourceIDError = true
			break
		}
	}
	if !hasSourceIDError {
		t.Errorf("expected source_id error, got: %v", problem.Errors)
	}
}

func TestFeedback_EmptyFeedback(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": []
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestFeedback_ExceedsBatchSize(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	// Build a batch with 51 entries
	var feedbackEntries []string
	for i := 0; i < 51; i++ {
		feedbackEntries = append(feedbackEntries, `{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}`)
	}
	body := `{"source_id": "devcontainer-abc123", "feedback": [` + strings.Join(feedbackEntries, ",") + `]}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestFeedback_InvalidLoreID(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "invalid", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var problem ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	hasLoreIDError := false
	for _, e := range problem.Errors {
		if strings.Contains(e.Field, "lore_id") && strings.Contains(e.Message, "ULID") {
			hasLoreIDError = true
			break
		}
	}
	if !hasLoreIDError {
		t.Errorf("expected lore_id ULID error, got: %v", problem.Errors)
	}
}

func TestFeedback_InvalidType(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "unknown_type"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var problem ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	hasTypeError := false
	for _, e := range problem.Errors {
		if strings.Contains(e.Field, "type") && strings.Contains(e.Message, "must be one of") {
			hasTypeError = true
			break
		}
	}
	if !hasTypeError {
		t.Errorf("expected type enum error, got: %v", problem.Errors)
	}
}

func TestFeedback_ContentType(t *testing.T) {
	s := &mockStore{
		stats:          &types.StoreStats{},
		feedbackResult: &types.FeedbackResult{Updates: []types.FeedbackResultUpdate{}},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-abc123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Feedback(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q for success response", contentType, "application/json")
	}
}

// --- Graceful Shutdown Tests (Epic 5 Retro Action Item) ---

// slowFeedbackStore wraps mockStore with a delay for testing graceful shutdown
type slowFeedbackStore struct {
	*mockStore
	delay time.Duration
}

func (s *slowFeedbackStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	// Simulate slow processing
	select {
	case <-time.After(s.delay):
		// Processing completed
	case <-ctx.Done():
		// Context cancelled - but we still complete the operation
		// This simulates "flush" behavior where in-flight ops complete
	}
	return s.mockStore.RecordFeedback(ctx, feedback)
}

func TestFlushDuringGracefulShutdown_FeedbackCompletes(t *testing.T) {
	// This test verifies that in-flight feedback operations complete during graceful shutdown.
	// The server's Shutdown() method drains in-flight requests before returning.

	validationCount := 1
	baseStore := &mockStore{
		stats: &types.StoreStats{},
		feedbackResult: &types.FeedbackResult{
			Updates: []types.FeedbackResultUpdate{
				{
					LoreID:             "01ARZ3NDEKTSV4RRFFQ69G5FAV",
					PreviousConfidence: 0.5,
					CurrentConfidence:  0.58,
					ValidationCount:    &validationCount,
				},
			},
		},
	}
	slowStore := &slowFeedbackStore{mockStore: baseStore, delay: 100 * time.Millisecond}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := NewHandler(slowStore, nil, embedder, "test-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Create a real HTTP server
	srv := &http.Server{
		Handler: router,
	}

	// Use a listener to get a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for server to be ready
	time.Sleep(10 * time.Millisecond)

	// Start a slow feedback request in goroutine
	requestDone := make(chan struct {
		statusCode int
		err        error
	}, 1)

	go func() {
		body := `{
			"source_id": "shutdown-test",
			"feedback": [
				{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
			]
		}`
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api/v1/lore/feedback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-key")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			requestDone <- struct {
				statusCode int
				err        error
			}{0, err}
			return
		}
		defer resp.Body.Close()
		requestDone <- struct {
			statusCode int
			err        error
		}{resp.StatusCode, nil}
	}()

	// Wait a bit for request to start processing, then initiate shutdown
	time.Sleep(20 * time.Millisecond)

	// Initiate graceful shutdown with generous timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Verify the in-flight request completed successfully
	result := <-requestDone
	if result.err != nil {
		t.Errorf("Request failed during shutdown: %v", result.err)
	}
	if result.statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d (feedback should complete during shutdown)", result.statusCode)
	}

	// Check for server errors
	select {
	case err := <-serverErr:
		if err != nil {
			t.Errorf("Server error: %v", err)
		}
	default:
	}
}

// --- DeleteLore Endpoint Tests (Story 6.4) ---
//
// NOTE: Unit tests (TestDeleteLore_Success, TestDeleteLore_NotFound, etc.) call the handler
// directly using withChiURLParam to inject URL parameters, intentionally bypassing the router
// and auth middleware. This isolates handler logic testing from routing/middleware concerns.
//
// Integration tests (TestDeleteEndpoint_RoundTrip, TestDeleteLore_Unauthorized) use the full
// router to verify end-to-end behavior including route registration and auth middleware.

// withChiURLParam adds a chi URL param to the request context
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestDeleteLore_Success(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	req = withChiURLParam(req, "id", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	w := httptest.NewRecorder()

	handler.DeleteLore(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// 204 should have no response body
	if w.Body.Len() != 0 {
		t.Errorf("body length = %d, want 0 for 204 No Content", w.Body.Len())
	}
}

func TestDeleteLore_NotFound(t *testing.T) {
	s := &mockStore{
		stats:     &types.StoreStats{},
		deleteErr: store.ErrNotFound,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	req = withChiURLParam(req, "id", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	w := httptest.NewRecorder()

	handler.DeleteLore(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if problem.Status != http.StatusNotFound {
		t.Errorf("problem.status = %d, want %d", problem.Status, http.StatusNotFound)
	}
}

func TestDeleteLore_InvalidULID(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/invalid-id", nil)
	req = withChiURLParam(req, "id", "invalid-id")
	w := httptest.NewRecorder()

	handler.DeleteLore(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}

	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatalf("failed to unmarshal problem: %v", err)
	}

	if !strings.Contains(problem.Detail, "ULID") {
		t.Errorf("problem.Detail should mention ULID, got: %q", problem.Detail)
	}
}

func TestDeleteLore_StoreError(t *testing.T) {
	s := &mockStore{
		stats:     &types.StoreStats{},
		deleteErr: errors.New("database connection failed"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	req = withChiURLParam(req, "id", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	w := httptest.NewRecorder()

	handler.DeleteLore(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestDeleteLore_Unauthorized(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := NewHandler(s, nil, embedder, "secret-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Request WITHOUT Authorization header
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- Delete Integration Test (Story 6.4) ---

// --- Recall Client Connection Logging Tests (Story 6.6) ---

// logEntry represents a captured structured log entry for testing
type logEntry struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Component string `json:"component"`
	Action    string `json:"action"`
	SourceID  string `json:"source_id"`
	RemoteAddr string `json:"remote_addr"`
	BytesServed int64 `json:"bytes_served"`
	DurationMs int64 `json:"duration_ms"`
	Since      string `json:"since"`
	LoreCount  int    `json:"lore_count"`
	DeletedCount int  `json:"deleted_count"`
	Accepted   int    `json:"accepted"`
	Merged     int    `json:"merged"`
	Rejected   int    `json:"rejected"`
	Count      int    `json:"count"`
}

// captureLog runs a function and captures all slog output, returning parsed log entries
func captureLog(t *testing.T, fn func()) []logEntry {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	fn()

	var entries []logEntry
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry logEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Logf("failed to parse log line: %s", line)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// findLogEntry finds a log entry with matching action
func findLogEntry(entries []logEntry, action string) *logEntry {
	for _, e := range entries {
		if e.Action == action {
			return &e
		}
	}
	return nil
}

func TestExtractSourceID_WithHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderRecallSourceID, "test-client-abc123")

	sourceID := extractSourceID(req)

	if sourceID != "test-client-abc123" {
		t.Errorf("extractSourceID() = %q, want %q", sourceID, "test-client-abc123")
	}
}

func TestExtractSourceID_WithoutHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	sourceID := extractSourceID(req)

	if sourceID != "unknown" {
		t.Errorf("extractSourceID() = %q, want %q", sourceID, "unknown")
	}
}

func TestExtractSourceID_EmptyHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(HeaderRecallSourceID, "")

	sourceID := extractSourceID(req)

	if sourceID != "unknown" {
		t.Errorf("extractSourceID() = %q, want %q for empty header", sourceID, "unknown")
	}
}

func TestSnapshot_LogsClientBootstrapWithSourceID(t *testing.T) {
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(strings.NewReader(string(testData)))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
		snapshotErr:    nil,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	req.Header.Set(HeaderRecallSourceID, "devcontainer-test123")
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.Snapshot(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	entry := findLogEntry(entries, "client_bootstrap")
	if entry == nil {
		t.Fatalf("expected log with action=client_bootstrap, got entries: %+v", entries)
	}

	if entry.SourceID != "devcontainer-test123" {
		t.Errorf("source_id = %q, want %q", entry.SourceID, "devcontainer-test123")
	}
	if entry.Msg != "recall client bootstrap" {
		t.Errorf("msg = %q, want %q", entry.Msg, "recall client bootstrap")
	}
	if entry.Component != "api" {
		t.Errorf("component = %q, want %q", entry.Component, "api")
	}
	if entry.BytesServed == 0 {
		t.Error("expected bytes_served > 0")
	}
}

func TestSnapshot_LogsUnknownSourceIDWithoutHeader(t *testing.T) {
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(strings.NewReader(string(testData)))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
		snapshotErr:    nil,
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/snapshot", nil)
	// No X-Recall-Source-ID header
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.Snapshot(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	entry := findLogEntry(entries, "client_bootstrap")
	if entry == nil {
		t.Fatalf("expected log with action=client_bootstrap, got entries: %+v", entries)
	}

	if entry.SourceID != "unknown" {
		t.Errorf("source_id = %q, want %q when header absent", entry.SourceID, "unknown")
	}
}

func TestDelta_LogsClientSyncWithSourceID(t *testing.T) {
	asOf := time.Now().UTC()
	s := &mockStore{
		stats: &types.StoreStats{},
		deltaResult: &types.DeltaResult{
			Lore: []types.LoreEntry{
				{ID: "test-id-1", Content: "Test", Category: "PATTERN_OUTCOME"},
				{ID: "test-id-2", Content: "Test 2", Category: "PATTERN_OUTCOME"},
			},
			DeletedIDs: []string{"deleted-1"},
			AsOf:       asOf,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	req.Header.Set(HeaderRecallSourceID, "devcontainer-delta123")
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.Delta(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	entry := findLogEntry(entries, "client_sync")
	if entry == nil {
		t.Fatalf("expected log with action=client_sync, got entries: %+v", entries)
	}

	if entry.SourceID != "devcontainer-delta123" {
		t.Errorf("source_id = %q, want %q", entry.SourceID, "devcontainer-delta123")
	}
	if entry.Msg != "recall client sync" {
		t.Errorf("msg = %q, want %q", entry.Msg, "recall client sync")
	}
	if entry.LoreCount != 2 {
		t.Errorf("lore_count = %d, want %d", entry.LoreCount, 2)
	}
	if entry.DeletedCount != 1 {
		t.Errorf("deleted_count = %d, want %d", entry.DeletedCount, 1)
	}
}

func TestDelta_LogsUnknownSourceIDWithoutHeader(t *testing.T) {
	asOf := time.Now().UTC()
	s := &mockStore{
		stats: &types.StoreStats{},
		deltaResult: &types.DeltaResult{
			Lore:       []types.LoreEntry{},
			DeletedIDs: []string{},
			AsOf:       asOf,
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since=2026-01-28T10:00:00Z", nil)
	// No X-Recall-Source-ID header
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.Delta(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	entry := findLogEntry(entries, "client_sync")
	if entry == nil {
		t.Fatalf("expected log with action=client_sync, got entries: %+v", entries)
	}

	if entry.SourceID != "unknown" {
		t.Errorf("source_id = %q, want %q when header absent", entry.SourceID, "unknown")
	}
}

func TestIngestLore_LogsSuccessWithSourceIDAndCounts(t *testing.T) {
	s := &mockStore{
		stats: &types.StoreStats{},
		ingestResult: &types.IngestResult{
			Accepted: 2,
			Merged:   1,
			Rejected: 0,
			Errors:   []string{},
		},
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-ingest123",
		"lore": [
			{"content": "First insight", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.7},
			{"content": "Second insight", "category": "PATTERN_OUTCOME", "confidence": 0.8},
			{"content": "Third insight", "category": "ARCHITECTURAL_DECISION", "confidence": 0.9}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.IngestLore(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	entry := findLogEntry(entries, "ingest")
	if entry == nil {
		t.Fatalf("expected log with action=ingest, got entries: %+v", entries)
	}

	if entry.SourceID != "devcontainer-ingest123" {
		t.Errorf("source_id = %q, want %q", entry.SourceID, "devcontainer-ingest123")
	}
	if entry.Msg != "lore ingested" {
		t.Errorf("msg = %q, want %q", entry.Msg, "lore ingested")
	}
	if entry.Accepted != 2 {
		t.Errorf("accepted = %d, want %d", entry.Accepted, 2)
	}
	if entry.Merged != 1 {
		t.Errorf("merged = %d, want %d", entry.Merged, 1)
	}
	if entry.DurationMs < 0 {
		t.Error("expected duration_ms >= 0")
	}
}

func TestIngestLore_LogsFailureWithSourceID(t *testing.T) {
	s := &mockStore{
		stats:     &types.StoreStats{},
		ingestErr: errors.New("database connection failed"),
	}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := newTestHandler(s, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-fail123",
		"lore": [{"content": "Test", "category": "DEPENDENCY_BEHAVIOR", "confidence": 0.5}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.IngestLore(w, req)
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	entry := findLogEntry(entries, "ingest_failed")
	if entry == nil {
		t.Fatalf("expected log with action=ingest_failed, got entries: %+v", entries)
	}

	if entry.SourceID != "devcontainer-fail123" {
		t.Errorf("source_id = %q, want %q", entry.SourceID, "devcontainer-fail123")
	}
}

func TestFeedback_PerformanceWarningIncludesSourceID(t *testing.T) {
	// Create a slow feedback store that takes > 500ms
	baseStore := &mockStore{
		stats: &types.StoreStats{},
		feedbackResult: &types.FeedbackResult{
			Updates: []types.FeedbackResultUpdate{},
		},
	}
	slowStore := &slowFeedbackStore{mockStore: baseStore, delay: 600 * time.Millisecond}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := NewHandler(slowStore, nil, embedder, "api-key", "1.0.0")

	body := `{
		"source_id": "devcontainer-slow123",
		"feedback": [
			{"lore_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "type": "helpful"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	entries := captureLog(t, func() {
		handler.Feedback(w, req)
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Find the warning log (it should have action=feedback and be a WARN level)
	var warningEntry *logEntry
	for _, e := range entries {
		if e.Action == "feedback" && e.Level == "WARN" {
			warningEntry = &e
			break
		}
	}

	if warningEntry == nil {
		t.Fatalf("expected WARN log with action=feedback, got entries: %+v", entries)
	}

	if warningEntry.SourceID != "devcontainer-slow123" {
		t.Errorf("performance warning source_id = %q, want %q", warningEntry.SourceID, "devcontainer-slow123")
	}
}

func TestDeleteLore_RateLimited(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "text-embedding-3-small"}
	handler := NewHandler(s, nil, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Make many rapid requests to trigger rate limiting
	// The rate limiter allows 100 burst, so we need >100 to trigger
	var rateLimitedCount int
	for i := 0; i < 110; i++ {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
		req.Header.Set("Authorization", "Bearer test-api-key")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	// Should have hit rate limit after 100 requests
	if rateLimitedCount == 0 {
		t.Error("expected some requests to be rate limited")
	}

	// Verify rate limited response has Retry-After header
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusTooManyRequests {
		retryAfter := w.Header().Get("Retry-After")
		if retryAfter == "" {
			t.Error("rate limited response should have Retry-After header")
		}
	}
}

func TestDeleteEndpoint_RoundTrip(t *testing.T) {
	// This test uses a real SQLiteStore to verify the full data flow
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"

	// Create real store
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()

	// Insert test data
	entries := []types.NewLoreEntry{
		{Content: "Delete integration test entry", Category: "PATTERN_OUTCOME", Confidence: 0.8, SourceID: "test-src"},
	}
	_, err = sqliteStore.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Get the entry ID
	entry, err := sqliteStore.GetDelta(context.Background(), time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Lore) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entry.Lore))
	}
	entryID := entry.Lore[0].ID

	// Create handler with real store
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(sqliteStore, nil, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Make DELETE request
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/lore/"+entryID, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify entry is soft-deleted (not retrievable via GetLore)
	_, err = sqliteStore.GetLore(context.Background(), entryID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetLore after delete: expected ErrNotFound, got: %v", err)
	}

	// Verify entry appears in delta deleted_ids
	delta, err := sqliteStore.GetDelta(context.Background(), time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range delta.DeletedIDs {
		if id == entryID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("deleted entry ID %s not found in delta.DeletedIDs", entryID)
	}
}

// --- Stats Endpoint Integration Test ---

func TestStatsEndpoint_RoundTrip(t *testing.T) {
	// This test uses a real SQLiteStore to verify the full data flow
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/engram.db"

	// Create real store
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()

	// Insert test data with varied characteristics
	entries := []types.NewLoreEntry{
		{Content: "First entry high confidence", Category: "PATTERN_OUTCOME", Confidence: 0.9, SourceID: "source-1"},
		{Content: "Second entry low confidence", Category: "PATTERN_OUTCOME", Confidence: 0.2, SourceID: "source-1"},
		{Content: "Third entry different category", Category: "ARCHITECTURAL_DECISION", Confidence: 0.5, SourceID: "source-2"},
	}
	_, err = sqliteStore.IngestLore(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}

	// Create handler with real store
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(sqliteStore, nil, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Make GET request WITHOUT auth (stats is public)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Parse response
	var stats types.ExtendedStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify stats reflect inserted data
	if stats.TotalLore != 3 {
		t.Errorf("total_lore = %d, want 3", stats.TotalLore)
	}
	if stats.ActiveLore != 3 {
		t.Errorf("active_lore = %d, want 3", stats.ActiveLore)
	}
	if stats.DeletedLore != 0 {
		t.Errorf("deleted_lore = %d, want 0", stats.DeletedLore)
	}

	// Check category distribution
	if stats.CategoryStats["PATTERN_OUTCOME"] != 2 {
		t.Errorf("category_stats[PATTERN_OUTCOME] = %d, want 2", stats.CategoryStats["PATTERN_OUTCOME"])
	}
	if stats.CategoryStats["ARCHITECTURAL_DECISION"] != 1 {
		t.Errorf("category_stats[ARCHITECTURAL_DECISION] = %d, want 1", stats.CategoryStats["ARCHITECTURAL_DECISION"])
	}

	// Check unique sources
	if stats.UniqueSourceCount != 2 {
		t.Errorf("unique_source_count = %d, want 2", stats.UniqueSourceCount)
	}

	// Check quality stats
	if stats.QualityStats.HighConfidenceCount != 1 {
		t.Errorf("high_confidence_count = %d, want 1 (only 0.9 >= 0.8)", stats.QualityStats.HighConfidenceCount)
	}
	if stats.QualityStats.LowConfidenceCount != 1 {
		t.Errorf("low_confidence_count = %d, want 1 (only 0.2 < 0.3)", stats.QualityStats.LowConfidenceCount)
	}

	// Verify stats_as_of is set
	if stats.StatsAsOf.IsZero() {
		t.Error("stats_as_of should be set")
	}
}

func TestStatsEndpoint_NoAuthRequired(t *testing.T) {
	// Create a mock store
	s := &mockStore{
		extendedStats: &types.ExtendedStats{
			CategoryStats: map[string]int64{},
			StatsAsOf:     time.Now().UTC(),
		},
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, nil, embedder, "secret-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	// Make GET request WITHOUT auth header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should succeed without auth
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (stats endpoint should be public)", w.Code, http.StatusOK)
	}
}
