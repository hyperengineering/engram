package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Mock Implementations for Testing ---

// mockStore implements store.Store interface for testing
type mockStore struct {
	stats       *types.StoreStats
	statsErr    error
	ingestErr   error
	ingestCalls int
	lastEntries []types.NewLoreEntry
}

func (m *mockStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	m.ingestCalls++
	m.lastEntries = entries
	if m.ingestErr != nil {
		return nil, m.ingestErr
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

func (m *mockStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
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
