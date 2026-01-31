package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Store Management Handler Tests ---

func setupStoreManager(t *testing.T) (*multistore.StoreManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}

	return manager, rootPath
}

func TestListStores_Empty(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ListStoresResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Stores) != 0 {
		t.Errorf("expected 0 stores, got %d", len(resp.Stores))
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestListStores_Multiple(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	// Create multiple stores
	manager.GetStore(ctx, "default") // Auto-creates default
	manager.CreateStore(ctx, "project-a", "Project A")
	manager.CreateStore(ctx, "project-b", "Project B")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp ListStoresResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Stores) != 3 {
		t.Errorf("expected 3 stores, got %d", len(resp.Stores))
	}
	if resp.Total != 3 {
		t.Errorf("expected total 3, got %d", resp.Total)
	}

	// Verify sorted by ID
	if resp.Stores[0].ID != "default" {
		t.Errorf("expected first store 'default', got %q", resp.Stores[0].ID)
	}
	if resp.Stores[1].ID != "project-a" {
		t.Errorf("expected second store 'project-a', got %q", resp.Stores[1].ID)
	}
	if resp.Stores[2].ID != "project-b" {
		t.Errorf("expected third store 'project-b', got %q", resp.Stores[2].ID)
	}
}

func TestListStores_Unauthorized(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestGetStoreInfo_Success(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.GetStore(ctx, "default")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/default", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StoreInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != "default" {
		t.Errorf("expected ID 'default', got %q", resp.ID)
	}
	if resp.Stats == nil {
		t.Error("expected Stats to be non-nil")
	}
}

func TestGetStoreInfo_NotFound(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetStoreInfo_EncodedPath(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "org/project", "Org project")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// URL-encoded org/project -> org%2Fproject
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/org%2Fproject", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StoreInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != "org/project" {
		t.Errorf("expected ID 'org/project', got %q", resp.ID)
	}
}

func TestGetStoreInfo_InvalidFormat(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/INVALID", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateStore_Success(t *testing.T) {
	manager, rootPath := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	body := `{"store_id": "newstore", "description": "New store"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreateStoreResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != "newstore" {
		t.Errorf("expected ID 'newstore', got %q", resp.ID)
	}
	if resp.Description != "New store" {
		t.Errorf("expected description 'New store', got %q", resp.Description)
	}

	// Verify store directory was created
	storeDir := filepath.Join(rootPath, "newstore")
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		t.Error("store directory should exist")
	}
}

func TestCreateStore_InvalidID(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	body := `{"store_id": "INVALID_ID"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestCreateStore_AlreadyExists(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "existing", "Existing store")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	body := `{"store_id": "existing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}
}

func TestCreateStore_MissingBody(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDeleteStore_Success(t *testing.T) {
	manager, rootPath := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "todelete", "To delete")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/todelete?confirm=true", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify store directory was deleted
	storeDir := filepath.Join(rootPath, "todelete")
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Error("store directory should be deleted")
	}
}

func TestDeleteStore_MissingConfirm(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "todelete", "To delete")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Missing confirm=true
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/todelete", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDeleteStore_NotFound(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/nonexistent?confirm=true", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteStore_DefaultForbidden(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.GetStore(ctx, "default") // Create default store

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/default?confirm=true", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestDeleteStore_EncodedPath(t *testing.T) {
	manager, rootPath := setupStoreManager(t)
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "org/project", "Org project")

	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(s, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// URL-encoded org/project -> org%2Fproject
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/org%2Fproject?confirm=true", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify store directory was deleted
	storeDir := filepath.Join(rootPath, "org", "project")
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Error("store directory should be deleted")
	}
}

func TestStoreHandlers_NoStoreManager(t *testing.T) {
	s := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	// No store manager (nil)
	handler := NewHandler(s, nil, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, nil)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/stores", ""},
		{http.MethodGet, "/api/v1/stores/default", ""},
		{http.MethodPost, "/api/v1/stores", `{"store_id": "test"}`},
		{http.MethodDelete, "/api/v1/stores/test?confirm=true", ""},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			req.Header.Set("Authorization", "Bearer test-api-key")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("expected status 503, got %d", w.Code)
			}
		})
	}
}

// --- Store-Scoped Lore Operations Tests (Story 7.3) ---

func TestStoreScopedIngest_Success(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Create a test store
	_, err := manager.CreateStore(context.Background(), "test-store", "Test")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	body := `{"source_id": "test-source", "lore": [{"content": "test lore", "category": "PATTERN_OUTCOME", "confidence": 0.8}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/lore", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp types.IngestResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Accepted != 1 {
		t.Errorf("expected 1 accepted, got %d", resp.Accepted)
	}
}

func TestStoreScopedIngest_StoreNotFound(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Don't create the store

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	body := `{"source_id": "test-source", "lore": [{"content": "test lore", "category": "PATTERN_OUTCOME", "confidence": 0.8}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/nonexistent/lore", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestStoreScopedDelta_Success(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	_, err := manager.CreateStore(context.Background(), "test-store", "Test")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	since := "2020-01-01T00:00:00Z"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/lore/delta?since="+since, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStoreScopedFeedback_RouteExists(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	_, err := manager.CreateStore(context.Background(), "test-store", "Test")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Empty feedback array will return 422 validation error,
	// which proves the route exists and reaches the handler
	body := `{"source_id": "test-source", "feedback": []}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/lore/feedback", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// 422 proves the request reached the handler (not 404 from middleware)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422 (validation error), got %d: %s", w.Code, w.Body.String())
	}
}

func TestBackwardCompatibility_LoreRoutes(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Ensure default store exists
	_, err := manager.GetStore(context.Background(), multistore.DefaultStoreID)
	if err != nil {
		t.Fatalf("GetStore(default) error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Test that existing /lore route still works
	body := `{"source_id": "test-source", "lore": [{"content": "test lore", "category": "PATTERN_OUTCOME", "confidence": 0.8}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/lore", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should work and use default store
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for backward-compatible route, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBackwardCompatibility_DeltaRoute(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Ensure default store exists
	_, err := manager.GetStore(context.Background(), multistore.DefaultStoreID)
	if err != nil {
		t.Fatalf("GetStore(default) error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	since := "2020-01-01T00:00:00Z"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore/delta?since="+since, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for backward-compatible delta route, got %d: %s", w.Code, w.Body.String())
	}
}

func TestStoreScopedDelete_NotFound_Store(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Store doesn't exist
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/nonexistent/lore/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestStoreIsolation_IngestThenQueryOther(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Create two stores
	_, err := manager.CreateStore(context.Background(), "store-a", "Store A")
	if err != nil {
		t.Fatalf("CreateStore(store-a) error = %v", err)
	}
	_, err = manager.CreateStore(context.Background(), "store-b", "Store B")
	if err != nil {
		t.Fatalf("CreateStore(store-b) error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Ingest to store-a
	body := `{"source_id": "test-source", "lore": [{"content": "secret data for store A", "category": "PATTERN_OUTCOME", "confidence": 0.8}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/store-a/lore", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ingest to store-a failed: %d %s", w.Code, w.Body.String())
	}

	// Query delta from store-b
	since := "2020-01-01T00:00:00Z"
	req = httptest.NewRequest(http.MethodGet, "/api/v1/stores/store-b/lore/delta?since="+since, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delta from store-b failed: %d %s", w.Code, w.Body.String())
	}

	var deltaResp types.DeltaResult
	if err := json.NewDecoder(w.Body).Decode(&deltaResp); err != nil {
		t.Fatalf("failed to decode delta response: %v", err)
	}

	// Store B should have no lore (isolation)
	if len(deltaResp.Lore) != 0 {
		t.Errorf("store-b should have 0 lore entries, got %d (isolation violation)", len(deltaResp.Lore))
	}

	// Query delta from store-a - should have the ingested lore
	req = httptest.NewRequest(http.MethodGet, "/api/v1/stores/store-a/lore/delta?since="+since, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delta from store-a failed: %d %s", w.Code, w.Body.String())
	}

	if err := json.NewDecoder(w.Body).Decode(&deltaResp); err != nil {
		t.Fatalf("failed to decode delta response: %v", err)
	}

	if len(deltaResp.Lore) != 1 {
		t.Errorf("store-a should have 1 lore entry, got %d", len(deltaResp.Lore))
	}
}

func TestEncodedStoreID_InPath(t *testing.T) {
	manager, _ := setupStoreManager(t)
	defer manager.Close()

	// Create store with path separator
	_, err := manager.CreateStore(context.Background(), "org/project", "Org Project Store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")
	router := NewRouter(handler, manager)

	// Use URL-encoded path: org%2Fproject
	body := `{"source_id": "test-source", "lore": [{"content": "test", "category": "PATTERN_OUTCOME", "confidence": 0.8}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/org%2Fproject/lore", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for encoded store ID, got %d: %s", w.Code, w.Body.String())
	}
}
