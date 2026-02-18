package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/plugin/recall"
	engramsync "github.com/hyperengineering/engram/internal/sync"
	"github.com/hyperengineering/engram/internal/types"
)

// --- Request Validation Tests ---

func TestValidatePushRequest_Valid(t *testing.T) {
	req := engramsync.PushRequest{
		PushID:        "550e8400-e29b-41d4-a716-446655440000",
		SourceID:      "client-uuid",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert"}},
	}
	if err := validatePushRequest(req); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidatePushRequest_MissingPushID(t *testing.T) {
	req := engramsync.PushRequest{
		SourceID:      "client-uuid",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert"}},
	}
	err := validatePushRequest(req)
	if err == nil {
		t.Fatal("expected error for missing push_id")
	}
	if err.Error() != "push_id is required" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestValidatePushRequest_MissingSourceID(t *testing.T) {
	req := engramsync.PushRequest{
		PushID:        "550e8400-e29b-41d4-a716-446655440000",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert"}},
	}
	err := validatePushRequest(req)
	if err == nil {
		t.Fatal("expected error for missing source_id")
	}
	if err.Error() != "source_id is required" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestValidatePushRequest_InvalidSchemaVersion(t *testing.T) {
	req := engramsync.PushRequest{
		PushID:        "550e8400-e29b-41d4-a716-446655440000",
		SourceID:      "client-uuid",
		SchemaVersion: 0,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert"}},
	}
	err := validatePushRequest(req)
	if err == nil {
		t.Fatal("expected error for schema_version=0")
	}
}

func TestValidatePushRequest_EmptyEntries(t *testing.T) {
	req := engramsync.PushRequest{
		PushID:        "550e8400-e29b-41d4-a716-446655440000",
		SourceID:      "client-uuid",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{},
	}
	err := validatePushRequest(req)
	if err == nil {
		t.Fatal("expected error for empty entries")
	}
}

func TestValidatePushRequest_TooManyEntries(t *testing.T) {
	entries := make([]engramsync.ChangeLogEntry, MaxPushEntries+1)
	req := engramsync.PushRequest{
		PushID:        "550e8400-e29b-41d4-a716-446655440000",
		SourceID:      "client-uuid",
		SchemaVersion: 1,
		Entries:       entries,
	}
	err := validatePushRequest(req)
	if err == nil {
		t.Fatal("expected error for too many entries")
	}
}

// --- Helper: set up a real store manager with a recall store ---

func setupSyncTestEnv(t *testing.T) (*multistore.StoreManager, *Handler, *multistore.ManagedStore) {
	t.Helper()

	// Register recall plugin (idempotent, may already be registered)
	func() {
		defer func() { recover() }() // Ignore if already registered
		plugin.Register(recall.New())
	}()

	manager, _ := setupStoreManager(t)

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "test-store", "recall", "Test store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	// Set schema version to 2
	if err := managed.Store.SetSyncMeta(ctx, "schema_version", "2"); err != nil {
		t.Fatalf("SetSyncMeta() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, "test-api-key", "1.0.0")

	return manager, handler, managed
}

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
		"content":    "Test lore content",
		"context":    "Test context",
		"category":   "TESTING_STRATEGY",
		"confidence": 0.5,
		"source_id":  "src-1",
		"sources":    []string{"src-1"},
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return json.RawMessage(b)
}

// --- Idempotency Tests ---

func TestSyncPush_IdempotentReplay(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	ctx := context.Background()
	pushID := "push-1"

	// Pre-cache an idempotency response
	cachedResp := `{"accepted":5,"remote_sequence":100}`
	if err := managed.Store.RecordPushIdempotency(ctx, pushID, "test-store", []byte(cachedResp), IdempotencyTTL); err != nil {
		t.Fatalf("RecordPushIdempotency() error = %v", err)
	}

	req := engramsync.PushRequest{
		PushID:        pushID,
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")}},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check idempotent replay header
	if w.Header().Get("X-Idempotent-Replay") != "true" {
		t.Error("expected X-Idempotent-Replay header")
	}

	// Verify cached response is returned
	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 5 {
		t.Errorf("expected accepted=5, got %d", resp.Accepted)
	}
	if resp.RemoteSequence != 100 {
		t.Errorf("expected remote_sequence=100, got %d", resp.RemoteSequence)
	}
}

func TestSyncPush_NewPushID(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "new-push-id",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// No replay header
	if w.Header().Get("X-Idempotent-Replay") != "" {
		t.Error("should not have X-Idempotent-Replay header for new push")
	}

	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 1 {
		t.Errorf("expected accepted=1, got %d", resp.Accepted)
	}
	if resp.RemoteSequence <= 0 {
		t.Errorf("expected remote_sequence > 0, got %d", resp.RemoteSequence)
	}
}

func TestSyncPush_CachesResponse(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushID := "cache-test-push"
	req := engramsync.PushRequest{
		PushID:        pushID,
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify response was cached
	ctx := context.Background()
	cached, found, err := managed.Store.CheckPushIdempotency(ctx, pushID)
	if err != nil {
		t.Fatalf("CheckPushIdempotency() error = %v", err)
	}
	if !found {
		t.Fatal("expected push response to be cached")
	}
	if len(cached) == 0 {
		t.Fatal("cached response should not be empty")
	}
}

// --- Schema Version Tests ---

func TestSyncPush_SchemaMatch(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "schema-match-push",
		SourceID:      "client-1",
		SchemaVersion: 2, // Matches server
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncPush_ClientBehind(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "client-behind-push",
		SourceID:      "client-1",
		SchemaVersion: 1, // Behind server (2)
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (backward compatible), got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncPush_ClientAhead(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "client-ahead-push",
		SourceID:      "client-1",
		SchemaVersion: 3, // Ahead of server (2)
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
	}

	// Verify schema mismatch details in response
	var resp struct {
		Type          string `json:"type"`
		ClientVersion int    `json:"client_version"`
		ServerVersion int    `json:"server_version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != "https://engram.dev/errors/schema-mismatch" {
		t.Errorf("expected schema-mismatch type, got %q", resp.Type)
	}
	if resp.ClientVersion != 3 {
		t.Errorf("expected client_version=3, got %d", resp.ClientVersion)
	}
	if resp.ServerVersion != 2 {
		t.Errorf("expected server_version=2, got %d", resp.ServerVersion)
	}
}

// --- Plugin Validation Tests ---

func TestSyncPush_ValidationSuccess(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "valid-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
			{TableName: "lore_entries", EntityID: "e2", Operation: "upsert", Payload: validLorePayload(t, "e2")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 2 {
		t.Errorf("expected accepted=2, got %d", resp.Accepted)
	}
}

func TestSyncPush_ValidationFailure(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// Entry with invalid table name
	req := engramsync.PushRequest{
		PushID:        "invalid-table-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{Sequence: 42, TableName: "unknown_table", EntityID: "e1", Operation: "upsert", Payload: json.RawMessage(`{}`)},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 0 {
		t.Errorf("expected accepted=0, got %d", resp.Accepted)
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected validation errors")
	}
}

func TestSyncPush_AllOrNothing(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// 1 valid entry + 1 invalid entry = all rejected
	req := engramsync.PushRequest{
		PushID:        "all-or-nothing-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{Sequence: 1, TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
			{Sequence: 2, TableName: "unknown_table", EntityID: "e2", Operation: "upsert", Payload: json.RawMessage(`{}`)},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 0 {
		t.Errorf("all-or-nothing: expected accepted=0, got %d", resp.Accepted)
	}
}

// --- Transaction Tests ---

func TestSyncPush_ReplaySuccess(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	entryID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	req := engramsync.PushRequest{
		PushID:        "replay-success-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: entryID, Operation: "upsert", Payload: validLorePayload(t, entryID)},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify entry exists in domain table
	ctx := context.Background()
	entry, err := managed.Store.GetLore(ctx, entryID)
	if err != nil {
		t.Fatalf("GetLore() error = %v", err)
	}
	if entry.Content != "Test lore content" {
		t.Errorf("Content = %q, want %q", entry.Content, "Test lore content")
	}
}

func TestSyncPush_ChangeLogRecorded(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "changelog-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")},
			{TableName: "lore_entries", EntityID: "e2", Operation: "upsert", Payload: validLorePayload(t, "e2")},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify change log entries were recorded
	ctx := context.Background()
	entries, err := managed.Store.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 change log entries, got %d", len(entries))
	}

	// Verify entries have source_id set
	for _, e := range entries {
		if e.SourceID != "client-1" {
			t.Errorf("expected source_id=client-1, got %q", e.SourceID)
		}
	}

	// Verify response remote_sequence matches last change log entry
	var resp engramsync.PushResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.RemoteSequence != entries[len(entries)-1].Sequence {
		t.Errorf("remote_sequence=%d, want %d", resp.RemoteSequence, entries[len(entries)-1].Sequence)
	}
}

// --- Bad Request Tests ---

func TestSyncPush_InvalidJSON(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", bytes.NewBufferString("not json"))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSyncPush_StoreNotFound(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "push-1",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert", Payload: validLorePayload(t, "e1")}},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/nonexistent/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncPush_Unauthorized(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "push-1",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries:       []engramsync.ChangeLogEntry{{TableName: "lore_entries", EntityID: "e1", Operation: "upsert"}},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	// No Authorization header
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

// --- Delete operation test ---

func TestSyncPush_DeleteOperation(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	entryID := "01ARZ3NDEKTSV4RRFFQ69G5FAZ"

	// First insert an entry via push
	insertReq := engramsync.PushRequest{
		PushID:        "insert-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: entryID, Operation: "upsert", Payload: validLorePayload(t, entryID)},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, insertReq))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("insert push failed: %d: %s", w.Code, w.Body.String())
	}

	// Now delete it
	deleteReq := engramsync.PushRequest{
		PushID:        "delete-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: entryID, Operation: "delete"},
		},
	}

	httpReq = httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, deleteReq))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("delete push failed: %d: %s", w.Code, w.Body.String())
	}

	// Entry should be soft-deleted (GetLore returns error)
	ctx := context.Background()
	_, err := managed.Store.GetLore(ctx, entryID)
	if err == nil {
		t.Error("expected error for soft-deleted entry")
	}
}

// --- writeSchemaMismatch unit test ---

func TestWriteSchemaMismatch(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/stores/my-store/sync/push", nil)

	writeSchemaMismatch(w, r, 3, 2)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("expected content-type application/problem+json, got %q", ct)
	}

	var resp struct {
		Type          string `json:"type"`
		Title         string `json:"title"`
		Status        int    `json:"status"`
		Detail        string `json:"detail"`
		Instance      string `json:"instance"`
		ClientVersion int    `json:"client_version"`
		ServerVersion int    `json:"server_version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != 409 {
		t.Errorf("expected status=409, got %d", resp.Status)
	}
	if resp.ClientVersion != 3 {
		t.Errorf("expected client_version=3, got %d", resp.ClientVersion)
	}
	if resp.ServerVersion != 2 {
		t.Errorf("expected server_version=2, got %d", resp.ServerVersion)
	}
}

// --- parseDeltaRequest Tests ---

func TestParseDeltaRequest_Valid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=100&limit=50", nil)
	req, err := parseDeltaRequest(r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if req.After != 100 {
		t.Errorf("After = %d, want 100", req.After)
	}
	if req.Limit != 50 {
		t.Errorf("Limit = %d, want 50", req.Limit)
	}
}

func TestParseDeltaRequest_DefaultLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=100", nil)
	req, err := parseDeltaRequest(r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if req.After != 100 {
		t.Errorf("After = %d, want 100", req.After)
	}
	if req.Limit != engramsync.DefaultDeltaLimit {
		t.Errorf("Limit = %d, want %d", req.Limit, engramsync.DefaultDeltaLimit)
	}
}

func TestParseDeltaRequest_MissingAfter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta", nil)
	_, err := parseDeltaRequest(r)
	if err == nil {
		t.Fatal("expected error for missing after")
	}
	if !strings.Contains(err.Error(), "missing required") {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestParseDeltaRequest_InvalidAfter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=abc", nil)
	_, err := parseDeltaRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid after")
	}
	if !strings.Contains(err.Error(), "must be an integer") {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestParseDeltaRequest_NegativeAfter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=-1", nil)
	_, err := parseDeltaRequest(r)
	if err == nil {
		t.Fatal("expected error for negative after")
	}
	if !strings.Contains(err.Error(), "must be >= 0") {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestParseDeltaRequest_InvalidLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=100&limit=abc", nil)
	_, err := parseDeltaRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid limit")
	}
	if !strings.Contains(err.Error(), "must be an integer") {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestParseDeltaRequest_ZeroLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=100&limit=0", nil)
	_, err := parseDeltaRequest(r)
	if err == nil {
		t.Fatal("expected error for zero limit")
	}
	if !strings.Contains(err.Error(), "must be >= 1") {
		t.Errorf("unexpected error: %s", err.Error())
	}
}

func TestParseDeltaRequest_OverMaxLimit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sync/delta?after=100&limit=2000", nil)
	req, err := parseDeltaRequest(r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if req.Limit != engramsync.MaxDeltaLimit {
		t.Errorf("Limit = %d, want %d (capped)", req.Limit, engramsync.MaxDeltaLimit)
	}
}

// --- SyncDelta Handler Tests ---

// pushEntries is a helper that pushes N entries into the store via SyncPush.
func pushEntries(t *testing.T, router http.Handler, n int) {
	t.Helper()
	entries := make([]engramsync.ChangeLogEntry, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("entry-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   validLorePayload(t, id),
		}
	}
	req := engramsync.PushRequest{
		PushID:        fmt.Sprintf("push-delta-setup-%d", n),
		SourceID:      "test-client",
		SchemaVersion: 2,
		Entries:       entries,
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("pushEntries: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func deltaRequest(t *testing.T, router http.Handler, after int64, limit int) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/api/v1/stores/test-store/sync/delta?after=%d", after)
	if limit > 0 {
		url += fmt.Sprintf("&limit=%d", limit)
	}
	httpReq := httptest.NewRequest(http.MethodGet, url, nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	return w
}

func decodeDeltaResponse(t *testing.T, w *httptest.ResponseRecorder) engramsync.DeltaResponse {
	t.Helper()
	var resp engramsync.DeltaResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode delta response: %v", err)
	}
	return resp
}

func TestSyncDelta_Success(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 5)

	w := deltaRequest(t, router, 0, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeDeltaResponse(t, w)
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(resp.Entries))
	}
	if resp.HasMore {
		t.Error("expected has_more=false")
	}
}

func TestSyncDelta_EmptyResult(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 3)

	// Query after a sequence higher than all entries
	w := deltaRequest(t, router, 9999, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeDeltaResponse(t, w)
	if len(resp.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(resp.Entries))
	}
	if resp.HasMore {
		t.Error("expected has_more=false for empty result")
	}
	// last_sequence should echo back the after value
	if resp.LastSequence != 9999 {
		t.Errorf("LastSequence = %d, want 9999", resp.LastSequence)
	}
}

func TestSyncDelta_Pagination(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 10)

	w := deltaRequest(t, router, 0, 3)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeDeltaResponse(t, w)
	if len(resp.Entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(resp.Entries))
	}
	if !resp.HasMore {
		t.Error("expected has_more=true")
	}
	if resp.LatestSequence < 10 {
		t.Errorf("LatestSequence = %d, expected >= 10", resp.LatestSequence)
	}
}

func TestSyncDelta_LastPage(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 5)

	// Get first page to find sequences
	w := deltaRequest(t, router, 0, 3)
	resp := decodeDeltaResponse(t, w)

	// Get last page using last_sequence from first page
	w2 := deltaRequest(t, router, resp.LastSequence, 10)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	resp2 := decodeDeltaResponse(t, w2)
	if len(resp2.Entries) != 2 {
		t.Errorf("expected 2 remaining entries, got %d", len(resp2.Entries))
	}
	if resp2.HasMore {
		t.Error("expected has_more=false on last page")
	}
}

func TestSyncDelta_AfterZero(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 3)

	w := deltaRequest(t, router, 0, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeDeltaResponse(t, w)
	if len(resp.Entries) != 3 {
		t.Errorf("expected 3 entries from seq 1, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Sequence < 1 {
		t.Errorf("first entry sequence should be >= 1, got %d", resp.Entries[0].Sequence)
	}
}

func TestSyncDelta_LastSequence(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 5)

	w := deltaRequest(t, router, 0, 0)
	resp := decodeDeltaResponse(t, w)

	// last_sequence should equal the highest sequence in the response
	if len(resp.Entries) == 0 {
		t.Fatal("expected entries")
	}
	highestSeq := resp.Entries[len(resp.Entries)-1].Sequence
	if resp.LastSequence != highestSeq {
		t.Errorf("LastSequence = %d, want %d (highest in response)", resp.LastSequence, highestSeq)
	}
}

func TestSyncDelta_LatestSequence(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 10)

	// Request only first 3
	w := deltaRequest(t, router, 0, 3)
	resp := decodeDeltaResponse(t, w)

	// latest_sequence should reflect total entries on server
	if resp.LatestSequence < 10 {
		t.Errorf("LatestSequence = %d, expected >= 10", resp.LatestSequence)
	}
}

func TestSyncDelta_MissingAfter(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/delta", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncDelta_StoreNotFound(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/nonexistent/sync/delta?after=0", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncDelta_Unauthorized(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/delta?after=0", nil)
	// No auth header
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSyncDelta_EntriesNotNull(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// No data pushed — empty change log
	w := deltaRequest(t, router, 0, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check raw JSON: entries should be [] not null
	body := w.Body.String()
	if !strings.Contains(body, `"entries":[]`) && !strings.Contains(body, `"entries": []`) {
		t.Errorf("expected entries=[] in JSON, got: %s", body)
	}
}

func TestSyncDelta_IncludesPayload(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 1)

	w := deltaRequest(t, router, 0, 0)
	resp := decodeDeltaResponse(t, w)

	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	if len(resp.Entries[0].Payload) == 0 {
		t.Error("expected payload to be present for upsert")
	}
}

func TestSyncDelta_IncludesDeletes(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// Push an entry then delete it
	pushEntries(t, router, 1)

	deleteReq := engramsync.PushRequest{
		PushID:        "delta-delete-push",
		SourceID:      "test-client",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "entry-001", Operation: "delete"},
		},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/sync/push", makePushBody(t, deleteReq))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	wPush := httptest.NewRecorder()
	router.ServeHTTP(wPush, httpReq)
	if wPush.Code != http.StatusOK {
		t.Fatalf("delete push failed: %d: %s", wPush.Code, wPush.Body.String())
	}

	w := deltaRequest(t, router, 0, 0)
	resp := decodeDeltaResponse(t, w)

	// Should have 2 entries: upsert + delete
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries (upsert+delete), got %d", len(resp.Entries))
	}

	foundDelete := false
	for _, e := range resp.Entries {
		if e.Operation == "delete" {
			foundDelete = true
			if len(e.Payload) > 0 && string(e.Payload) != "null" {
				t.Errorf("delete entry should have null/empty payload, got %s", string(e.Payload))
			}
		}
	}
	if !foundDelete {
		t.Error("expected a delete operation in entries")
	}
}

func TestSyncDelta_OrderedBySequence(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	pushEntries(t, router, 5)

	w := deltaRequest(t, router, 0, 0)
	resp := decodeDeltaResponse(t, w)

	for i := 1; i < len(resp.Entries); i++ {
		if resp.Entries[i].Sequence <= resp.Entries[i-1].Sequence {
			t.Errorf("entries not ordered: seq[%d]=%d <= seq[%d]=%d",
				i, resp.Entries[i].Sequence, i-1, resp.Entries[i-1].Sequence)
		}
	}
}

// --- Integration: Full Pagination Walk ---

func TestSyncDelta_FullPagination(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	totalEntries := 25
	pushEntries(t, router, totalEntries)

	// Paginate with limit=7
	var allEntries []engramsync.ChangeLogEntry
	after := int64(0)
	pages := 0

	for {
		w := deltaRequest(t, router, after, 7)
		if w.Code != http.StatusOK {
			t.Fatalf("page %d: expected 200, got %d", pages, w.Code)
		}

		resp := decodeDeltaResponse(t, w)
		allEntries = append(allEntries, resp.Entries...)
		after = resp.LastSequence
		pages++

		if !resp.HasMore {
			break
		}

		if pages > 10 {
			t.Fatal("too many pages, possible infinite loop")
		}
	}

	if len(allEntries) != totalEntries {
		t.Errorf("collected %d entries across %d pages, want %d", len(allEntries), pages, totalEntries)
	}

	// Verify no duplicates
	seen := make(map[int64]bool)
	for _, e := range allEntries {
		if seen[e.Sequence] {
			t.Errorf("duplicate sequence %d", e.Sequence)
		}
		seen[e.Sequence] = true
	}
}
