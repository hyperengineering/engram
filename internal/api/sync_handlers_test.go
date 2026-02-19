package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/plugin/recall"
	"github.com/hyperengineering/engram/internal/plugin/tract"
	"github.com/hyperengineering/engram/internal/snapshot"
	"github.com/hyperengineering/engram/internal/store"
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
	handler := NewHandler(defaultStore, manager, embedder, nil, "test-api-key", "1.0.0")

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

// --- SyncSnapshot Handler Tests ---

func TestSyncSnapshot_Success(t *testing.T) {
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := newTestHandler(s, embedder, "test-api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if !bytes.Equal(w.Body.Bytes(), testData) {
		t.Errorf("body = %q, want %q", w.Body.String(), string(testData))
	}
}

func TestSyncSnapshot_ContentType(t *testing.T) {
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := newTestHandler(s, embedder, "test-api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/octet-stream")
	}
}

func TestSyncSnapshot_NotAvailable(t *testing.T) {
	s := &mockStore{
		stats:       &types.StoreStats{},
		snapshotErr: store.ErrSnapshotNotAvailable,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := newTestHandler(s, embedder, "test-api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "60")
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/problem+json")
	}
}

func TestSyncSnapshot_StoreError(t *testing.T) {
	s := &mockStore{
		stats:       &types.StoreStats{},
		snapshotErr: errors.New("disk failure"),
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := newTestHandler(s, embedder, "test-api-key", "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestSyncSnapshot_StoreNotFound(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/nonexistent/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSyncSnapshot_Unauthorized(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSyncSnapshot_RouteRegistered(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// Generate a snapshot so we get 200 instead of 503
	ctx := context.Background()
	managed, err := manager.GetStore(ctx, "test-store")
	if err != nil {
		t.Fatalf("GetStore() error = %v", err)
	}
	if err := managed.Store.GenerateSnapshot(ctx); err != nil {
		t.Fatalf("GenerateSnapshot() error = %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for registered route, got %d: %s", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/octet-stream")
	}

	// Verify it looks like a SQLite file
	body := w.Body.String()
	if !strings.HasPrefix(body, "SQLite format 3") {
		t.Errorf("body doesn't look like a SQLite file, got prefix: %q", body[:min(len(body), 20)])
	}
}

// --- Pre-signed URL Redirect Tests ---

// mockSnapshotUploader implements snapshot.Uploader for API handler tests.
type mockSnapshotUploader struct {
	presignedURL string
	presignedErr error
	uploadErr    error
}

func (m *mockSnapshotUploader) Upload(ctx context.Context, storeID string, filePath string) error {
	return m.uploadErr
}

func (m *mockSnapshotUploader) PresignedURL(ctx context.Context, storeID string) (string, time.Time, error) {
	if m.presignedErr != nil {
		return "", time.Time{}, m.presignedErr
	}
	return m.presignedURL, time.Now().Add(15 * time.Minute), nil
}

func TestSyncSnapshot_RedirectWhenS3Configured(t *testing.T) {
	uploader := &mockSnapshotUploader{
		presignedURL: "https://s3.example.com/bucket/test-store/snapshot/current.db?presigned=true",
	}

	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := &Handler{
		store:    s,
		embedder: embedder,
		uploader: uploader,
		apiKey:   "test-api-key",
		version:  "1.0.0",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (302 redirect)", w.Code, http.StatusFound)
	}

	location := w.Header().Get("Location")
	if location != uploader.presignedURL {
		t.Errorf("Location = %q, want %q", location, uploader.presignedURL)
	}
}

func TestSyncSnapshot_FallbackWhenPresignedURLFails(t *testing.T) {
	uploader := &mockSnapshotUploader{
		presignedErr: errors.New("S3 service unavailable"),
	}

	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := &Handler{
		store:    s,
		embedder: embedder,
		uploader: uploader,
		apiKey:   "test-api-key",
		version:  "1.0.0",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	// Should fall back to local streaming (200 OK, not redirect)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (local streaming fallback)", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/octet-stream")
	}

	if !bytes.Equal(w.Body.Bytes(), testData) {
		t.Errorf("body = %q, want %q", w.Body.String(), string(testData))
	}
}

func TestSyncSnapshot_FallbackWhenNoopUploader(t *testing.T) {
	// NoopUploader returns ErrNotConfigured for PresignedURL
	uploader := &snapshot.NoopUploader{}

	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := &Handler{
		store:    s,
		embedder: embedder,
		uploader: uploader,
		apiKey:   "test-api-key",
		version:  "1.0.0",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	// Should fall back to local streaming when S3 is not configured
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (local streaming fallback)", w.Code, http.StatusOK)
	}

	if !bytes.Equal(w.Body.Bytes(), testData) {
		t.Errorf("body = %q, want %q", w.Body.String(), string(testData))
	}
}

func TestSyncSnapshot_NilUploaderUsesLocalStreaming(t *testing.T) {
	testData := []byte("SQLite format 3\x00test snapshot data")
	reader := io.NopCloser(bytes.NewReader(testData))

	s := &mockStore{
		stats:          &types.StoreStats{},
		snapshotReader: reader,
	}
	embedder := &mockEmbedder{model: "test-model"}
	handler := &Handler{
		store:    s,
		embedder: embedder,
		uploader: nil, // No uploader configured
		apiKey:   "test-api-key",
		version:  "1.0.0",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	w := httptest.NewRecorder()

	handler.SyncSnapshot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if !bytes.Equal(w.Body.Bytes(), testData) {
		t.Errorf("body = %q, want %q", w.Body.String(), string(testData))
	}
}

// --- Tract Integration Tests ---

// setupTractTestEnv creates a tract-typed store with plugin migrations applied.
func setupTractTestEnv(t *testing.T) (*multistore.StoreManager, *Handler, *multistore.ManagedStore) {
	t.Helper()

	// Register tract plugin (idempotent, may already be registered)
	func() {
		defer func() { recover() }()
		plugin.Register(tract.New())
	}()

	manager, _ := setupStoreManager(t)

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "tract-store", "tract", "Tract test store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	// Set schema version to 1
	if err := managed.Store.SetSyncMeta(ctx, "schema_version", "1"); err != nil {
		t.Fatalf("SetSyncMeta() error = %v", err)
	}

	defaultStore := &mockStore{stats: &types.StoreStats{}}
	embedder := &mockEmbedder{model: "test-model"}
	handler := NewHandler(defaultStore, manager, embedder, nil, "test-api-key", "1.0.0")

	return manager, handler, managed
}

func validGoalPayload(t *testing.T, id string, parentGoalID *string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":             id,
		"title":          "Goal " + id,
		"description":    "Test goal description",
		"status":         "active",
		"priority":       1,
		"parent_goal_id": parentGoalID,
		"created_at":     "2026-01-01T00:00:00Z",
		"updated_at":     "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal goal payload: %v", err)
	}
	return json.RawMessage(b)
}

func validCSFPayload(t *testing.T, id, goalID string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":          id,
		"goal_id":     goalID,
		"title":       "CSF " + id,
		"description": "Test CSF",
		"status":      "tracking",
		"created_at":  "2026-01-01T00:00:00Z",
		"updated_at":  "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal csf payload: %v", err)
	}
	return json.RawMessage(b)
}

func validFWUPayload(t *testing.T, id, csfID string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"csf_id":     csfID,
		"title":      "FWU " + id,
		"priority":   0,
		"status":     "planned",
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fwu payload: %v", err)
	}
	return json.RawMessage(b)
}

func validICPayload(t *testing.T, id, fwuID string) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"fwu_id":     fwuID,
		"content":    "Test implementation context",
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal ic payload: %v", err)
	}
	return json.RawMessage(b)
}

// tractDB returns the underlying *sql.DB from a managed store via type assertion.
func tractDB(t *testing.T, managed *multistore.ManagedStore) *store.SQLiteStore {
	t.Helper()
	sqliteStore, ok := managed.Store.(*store.SQLiteStore)
	if !ok {
		t.Fatalf("managed store is not *store.SQLiteStore, got %T", managed.Store)
	}
	return sqliteStore
}

// Seed 8.2: Push a goal through the API to a Tract-typed store
func TestTractIntegration_PushGoal(t *testing.T) {
	manager, handler, managed := setupTractTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	goalID := "goal-001"
	req := engramsync.PushRequest{
		PushID:        "tract-push-goal",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries: []engramsync.ChangeLogEntry{
			{
				TableName: "goals",
				EntityID:  goalID,
				Operation: "upsert",
				Payload:   validGoalPayload(t, goalID, nil),
			},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/tract-store/sync/push", makePushBody(t, req))
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
	if resp.Accepted != 1 {
		t.Errorf("expected accepted=1, got %d", resp.Accepted)
	}
	if resp.RemoteSequence <= 0 {
		t.Errorf("expected remote_sequence > 0, got %d", resp.RemoteSequence)
	}

	// Verify the goal appears in the goals table
	sqliteStore := tractDB(t, managed)
	db := sqliteStore.DB()

	var title, status string
	err := db.QueryRow("SELECT title, status FROM goals WHERE id = ?", goalID).Scan(&title, &status)
	if err != nil {
		t.Fatalf("query goals table: %v", err)
	}
	if title != "Goal "+goalID {
		t.Errorf("title = %q, want %q", title, "Goal "+goalID)
	}
	if status != "active" {
		t.Errorf("status = %q, want %q", status, "active")
	}

	// Verify the change_log has the entry with correct table_name
	ctx := context.Background()
	entries, err := managed.Store.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 change log entry, got %d", len(entries))
	}
	if entries[0].TableName != "goals" {
		t.Errorf("change log table_name = %q, want %q", entries[0].TableName, "goals")
	}
	if entries[0].EntityID != goalID {
		t.Errorf("change log entity_id = %q, want %q", entries[0].EntityID, goalID)
	}
}

// Seed 8.3: Full graph push with all 4 tables in wrong FK order
func TestTractIntegration_FullGraphPush(t *testing.T) {
	manager, handler, managed := setupTractTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	goalID := "goal-100"
	csfID := "csf-100"
	fwuID := "fwu-100"
	icID := "ic-100"

	// Deliberately send entries in WRONG FK order:
	// IC -> FWU -> CSF -> Goal (leaf-first instead of root-first)
	req := engramsync.PushRequest{
		PushID:        "tract-full-graph-push",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries: []engramsync.ChangeLogEntry{
			{
				TableName: "implementation_contexts",
				EntityID:  icID,
				Operation: "upsert",
				Payload:   validICPayload(t, icID, fwuID),
			},
			{
				TableName: "fwus",
				EntityID:  fwuID,
				Operation: "upsert",
				Payload:   validFWUPayload(t, fwuID, csfID),
			},
			{
				TableName: "csfs",
				EntityID:  csfID,
				Operation: "upsert",
				Payload:   validCSFPayload(t, csfID, goalID),
			},
			{
				TableName: "goals",
				EntityID:  goalID,
				Operation: "upsert",
				Payload:   validGoalPayload(t, goalID, nil),
			},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/tract-store/sync/push", makePushBody(t, req))
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
	if resp.Accepted != 4 {
		t.Errorf("expected accepted=4, got %d", resp.Accepted)
	}

	// Verify all entries appear in their correct domain tables
	sqliteStore := tractDB(t, managed)
	db := sqliteStore.DB()

	// Check goals
	var goalTitle string
	if err := db.QueryRow("SELECT title FROM goals WHERE id = ?", goalID).Scan(&goalTitle); err != nil {
		t.Fatalf("query goals: %v", err)
	}
	if goalTitle != "Goal "+goalID {
		t.Errorf("goal title = %q, want %q", goalTitle, "Goal "+goalID)
	}

	// Check csfs
	var csfGoalID string
	if err := db.QueryRow("SELECT goal_id FROM csfs WHERE id = ?", csfID).Scan(&csfGoalID); err != nil {
		t.Fatalf("query csfs: %v", err)
	}
	if csfGoalID != goalID {
		t.Errorf("csf goal_id = %q, want %q", csfGoalID, goalID)
	}

	// Check fwus
	var fwuCSFID string
	if err := db.QueryRow("SELECT csf_id FROM fwus WHERE id = ?", fwuID).Scan(&fwuCSFID); err != nil {
		t.Fatalf("query fwus: %v", err)
	}
	if fwuCSFID != csfID {
		t.Errorf("fwu csf_id = %q, want %q", fwuCSFID, csfID)
	}

	// Check implementation_contexts
	var icFWUID string
	if err := db.QueryRow("SELECT fwu_id FROM implementation_contexts WHERE id = ?", icID).Scan(&icFWUID); err != nil {
		t.Fatalf("query implementation_contexts: %v", err)
	}
	if icFWUID != fwuID {
		t.Errorf("ic fwu_id = %q, want %q", icFWUID, fwuID)
	}

	// Verify change_log entries follow FK order (goals before csfs before fwus before ICs)
	ctx := context.Background()
	changeLog, err := managed.Store.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter() error = %v", err)
	}
	if len(changeLog) != 4 {
		t.Fatalf("expected 4 change log entries, got %d", len(changeLog))
	}

	expectedOrder := []string{"goals", "csfs", "fwus", "implementation_contexts"}
	for i, expected := range expectedOrder {
		if changeLog[i].TableName != expected {
			t.Errorf("change log[%d] table_name = %q, want %q", i, changeLog[i].TableName, expected)
		}
	}
}

// Seed 8.4: Mixed deletes and upserts with pre-populated data
func TestTractIntegration_MixedDeletesAndUpserts(t *testing.T) {
	manager, handler, managed := setupTractTestEnv(t)
	defer manager.Close()
	router := NewRouter(handler, manager)

	// Step 1: Pre-populate with a full graph
	goalID := "goal-200"
	csfID := "csf-200"
	fwuID := "fwu-200"
	icID := "ic-200"

	setupReq := engramsync.PushRequest{
		PushID:        "tract-setup-push",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "goals", EntityID: goalID, Operation: "upsert", Payload: validGoalPayload(t, goalID, nil)},
			{TableName: "csfs", EntityID: csfID, Operation: "upsert", Payload: validCSFPayload(t, csfID, goalID)},
			{TableName: "fwus", EntityID: fwuID, Operation: "upsert", Payload: validFWUPayload(t, fwuID, csfID)},
			{TableName: "implementation_contexts", EntityID: icID, Operation: "upsert", Payload: validICPayload(t, icID, fwuID)},
		},
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/tract-store/sync/push", makePushBody(t, setupReq))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("setup push failed: %d: %s", w.Code, w.Body.String())
	}

	// Step 2: Push mixed deletes and upserts
	// Delete the IC, add a new goal and CSF
	newGoalID := "goal-201"
	newCSFID := "csf-201"

	mixedReq := engramsync.PushRequest{
		PushID:        "tract-mixed-push",
		SourceID:      "client-1",
		SchemaVersion: 1,
		Entries: []engramsync.ChangeLogEntry{
			// New upserts
			{TableName: "goals", EntityID: newGoalID, Operation: "upsert", Payload: validGoalPayload(t, newGoalID, nil)},
			{TableName: "csfs", EntityID: newCSFID, Operation: "upsert", Payload: validCSFPayload(t, newCSFID, newGoalID)},
			// Delete existing IC
			{TableName: "implementation_contexts", EntityID: icID, Operation: "delete"},
		},
	}

	httpReq = httptest.NewRequest(http.MethodPost, "/api/v1/stores/tract-store/sync/push", makePushBody(t, mixedReq))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("mixed push failed: %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 3 {
		t.Errorf("expected accepted=3, got %d", resp.Accepted)
	}

	// Verify the IC was soft-deleted (deleted_at set)
	sqliteStore := tractDB(t, managed)
	db := sqliteStore.DB()

	var deletedAt *string
	err := db.QueryRow("SELECT deleted_at FROM implementation_contexts WHERE id = ?", icID).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query IC deleted_at: %v", err)
	}
	if deletedAt == nil || *deletedAt == "" {
		t.Error("expected IC deleted_at to be set, got nil or empty")
	}

	// Verify new goal was created
	var newGoalTitle string
	err = db.QueryRow("SELECT title FROM goals WHERE id = ? AND deleted_at IS NULL", newGoalID).Scan(&newGoalTitle)
	if err != nil {
		t.Fatalf("query new goal: %v", err)
	}
	if newGoalTitle != "Goal "+newGoalID {
		t.Errorf("new goal title = %q, want %q", newGoalTitle, "Goal "+newGoalID)
	}

	// Verify new CSF was created
	var newCSFTitle string
	err = db.QueryRow("SELECT title FROM csfs WHERE id = ? AND deleted_at IS NULL", newCSFID).Scan(&newCSFTitle)
	if err != nil {
		t.Fatalf("query new csf: %v", err)
	}
	if newCSFTitle != "CSF "+newCSFID {
		t.Errorf("new csf title = %q, want %q", newCSFTitle, "CSF "+newCSFID)
	}

	// Verify original entities are still intact (not deleted)
	var origGoalTitle string
	err = db.QueryRow("SELECT title FROM goals WHERE id = ? AND deleted_at IS NULL", goalID).Scan(&origGoalTitle)
	if err != nil {
		t.Fatalf("original goal should still exist: %v", err)
	}
}
