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
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/api"
	engramsync "github.com/hyperengineering/engram/internal/sync"

	_ "modernc.org/sqlite"
)

// --- Push → Delta Tests ---

func TestSync_PushThenDelta(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushEntries(t, router, "test-store", 5, "test-client")

	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(resp.Entries))
	}
	// Verify server-assigned sequences are positive and monotonic
	for i, e := range resp.Entries {
		if e.Sequence <= 0 {
			t.Errorf("entry %d: expected positive sequence, got %d", i, e.Sequence)
		}
		if i > 0 && e.Sequence <= resp.Entries[i-1].Sequence {
			t.Errorf("entry %d: sequence %d not greater than previous %d", i, e.Sequence, resp.Entries[i-1].Sequence)
		}
	}
}

func TestSync_PushThenDelta_Pagination(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushEntries(t, router, "test-store", 30, "test-client")

	var allEntries []engramsync.ChangeLogEntry
	after := int64(0)
	pages := 0

	for {
		resp := deltaRequest(t, router, "test-store", after, 10)
		allEntries = append(allEntries, resp.Entries...)
		after = resp.LastSequence
		pages++

		if !resp.HasMore {
			break
		}
		if pages > 5 {
			t.Fatal("too many pages, possible infinite loop")
		}
	}

	if pages != 3 {
		t.Errorf("expected 3 pages, got %d", pages)
	}
	if len(allEntries) != 30 {
		t.Errorf("expected 30 entries total, got %d", len(allEntries))
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

func TestSync_PushThenDelta_Empty(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// No entries pushed
	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(resp.Entries))
	}
	if resp.HasMore {
		t.Error("expected has_more=false")
	}
}

func TestSync_PushMultipleSources(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushEntriesWithIDs(t, router, "test-store", "src-A", "a-001", "a-002", "a-003", "a-004", "a-005")
	pushEntriesWithIDs(t, router, "test-store", "src-B", "b-001", "b-002", "b-003", "b-004", "b-005")

	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) != 10 {
		t.Errorf("expected 10 entries from 2 sources, got %d", len(resp.Entries))
	}
}

func TestSync_DeltaSourceFiltering(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushEntries(t, router, "test-store", 5, "src-A")

	resp := deltaRequest(t, router, "test-store", 0, 0)
	for _, e := range resp.Entries {
		if e.SourceID != "src-A" {
			t.Errorf("expected source_id=src-A, got %q", e.SourceID)
		}
	}
}

// --- Idempotency Tests ---

func TestSync_PushIdempotent_NoDoubleSideEffects(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushID := "idempotent-push-001"
	req := engramsync.PushRequest{
		PushID:        pushID,
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "idem-e1", Operation: "upsert", Payload: validLorePayload(t, "idem-e1")},
		},
	}

	// First push
	doSyncPush(t, router, "test-store", req)

	// Count change_log entries after first push
	delta1 := deltaRequest(t, router, "test-store", 0, 0)
	count1 := len(delta1.Entries)

	// Second push with same push_id
	doSyncPush(t, router, "test-store", req)

	// Count should be unchanged
	delta2 := deltaRequest(t, router, "test-store", 0, 0)
	count2 := len(delta2.Entries)

	if count2 != count1 {
		t.Errorf("change_log count changed after idempotent replay: %d → %d", count1, count2)
	}
}

func TestSync_PushIdempotent_ReturnsHeader(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	pushID := "idempotent-header-push"
	req := engramsync.PushRequest{
		PushID:        pushID,
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "idem-h1", Operation: "upsert", Payload: validLorePayload(t, "idem-h1")},
		},
	}

	// First push
	doSyncPush(t, router, "test-store", req)

	// Second push — check for replay header
	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Header().Get("X-Idempotent-Replay") != "true" {
		t.Error("expected X-Idempotent-Replay: true header on second push")
	}
}

// --- Schema Version Tests ---

func TestSync_PushClientAhead(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "client-ahead-push",
		SourceID:      "client-1",
		SchemaVersion: 3, // Server is at 2
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "schema-e1", Operation: "upsert", Payload: validLorePayload(t, "schema-e1")},
		},
	}

	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSync_PushSchemaMatch(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "schema-match-push",
		SourceID:      "client-1",
		SchemaVersion: 2, // Matches server
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "lore_entries", EntityID: "schema-m1", Operation: "upsert", Payload: validLorePayload(t, "schema-m1")},
		},
	}

	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Plugin Validation Tests ---

func TestSync_RecallPlugin_AllOrNothing(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// 9 valid + 1 invalid = all rejected
	entries := make([]engramsync.ChangeLogEntry, 10)
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("valid-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   validLorePayload(t, id),
		}
	}
	// 10th entry has unknown table
	entries[9] = engramsync.ChangeLogEntry{
		TableName: "unknown_table",
		EntityID:  "invalid-001",
		Operation: "upsert",
		Payload:   json.RawMessage(`{}`),
	}

	req := engramsync.PushRequest{
		PushID:        "all-or-nothing-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries:       entries,
	}

	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Accepted != 0 {
		t.Errorf("expected accepted=0, got %d", resp.Accepted)
	}
}

func TestSync_RecallPlugin_ValidBatch(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	entries := make([]engramsync.ChangeLogEntry, 5)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("batch-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   validLorePayload(t, id),
		}
	}

	req := engramsync.PushRequest{
		PushID:        "valid-batch-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries:       entries,
	}

	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Accepted != 5 {
		t.Errorf("expected accepted=5, got %d", resp.Accepted)
	}
}

func TestSync_RecallPlugin_UnknownTable(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	req := engramsync.PushRequest{
		PushID:        "unknown-table-push",
		SourceID:      "client-1",
		SchemaVersion: 2,
		Entries: []engramsync.ChangeLogEntry{
			{TableName: "unknown", EntityID: "u1", Operation: "upsert", Payload: json.RawMessage(`{}`)},
		},
	}

	w := doSyncPushRaw(t, router, "test-store", req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Backward Compatibility Tests ---

func TestSync_LoreIngestVisibleInDelta(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// Ingest via legacy lore endpoint
	ingestBody := `{"source_id":"legacy-client","lore":[{"content":"Legacy ingested lore","category":"TESTING_STRATEGY","confidence":0.5}]}`
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/lore/", strings.NewReader(ingestBody))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("ingest failed: %d: %s", w.Code, w.Body.String())
	}

	// Check that it appears in sync delta
	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) < 1 {
		t.Fatal("expected at least 1 entry in delta after lore ingest")
	}

	found := false
	for _, e := range resp.Entries {
		if e.TableName == "lore_entries" && e.Operation == "upsert" {
			found = true
			break
		}
	}
	if !found {
		t.Error("lore ingest entry not found in sync delta")
	}
}

func TestSync_DeleteVisibleInDelta(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// Ingest an entry first
	ingestBody := `{"source_id":"delete-test","lore":[{"content":"Entry to delete","category":"TESTING_STRATEGY","confidence":0.5}]}`
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/stores/test-store/lore/", strings.NewReader(ingestBody))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest failed: %d: %s", w.Code, w.Body.String())
	}

	// Parse response to get the entry ID
	var ingestResp struct {
		Accepted int `json:"accepted"`
		IDs      []string `json:"ids"`
	}
	if err := json.NewDecoder(w.Body).Decode(&ingestResp); err != nil {
		t.Fatalf("decode ingest response: %v", err)
	}
	if len(ingestResp.IDs) == 0 {
		// Try getting ID from delta
		delta := deltaRequest(t, router, "test-store", 0, 0)
		if len(delta.Entries) == 0 {
			t.Fatal("no entries found after ingest")
		}
		entryID := delta.Entries[0].EntityID

		// Delete via legacy endpoint
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/test-store/lore/"+entryID, nil)
		delReq.Header.Set("Authorization", "Bearer test-api-key")
		delReq.Header.Set("X-Recall-Source-ID", "delete-test")
		dw := httptest.NewRecorder()
		router.ServeHTTP(dw, delReq)

		if dw.Code != http.StatusOK && dw.Code != http.StatusNoContent {
			t.Fatalf("delete failed: %d: %s", dw.Code, dw.Body.String())
		}

		// Check delta for delete operation
		deltaAfter := deltaRequest(t, router, "test-store", 0, 0)
		foundDelete := false
		for _, e := range deltaAfter.Entries {
			if e.Operation == "delete" && e.EntityID == entryID {
				foundDelete = true
				break
			}
		}
		if !foundDelete {
			t.Error("delete operation not found in sync delta")
		}
		return
	}

	entryID := ingestResp.IDs[0]

	// Delete via legacy endpoint
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/stores/test-store/lore/"+entryID, nil)
	delReq.Header.Set("Authorization", "Bearer test-api-key")
	delReq.Header.Set("X-Recall-Source-ID", "delete-test")
	dw := httptest.NewRecorder()
	router.ServeHTTP(dw, delReq)

	if dw.Code != http.StatusOK && dw.Code != http.StatusNoContent {
		t.Fatalf("delete failed: %d: %s", dw.Code, dw.Body.String())
	}

	// Check delta for delete operation
	deltaAfter := deltaRequest(t, router, "test-store", 0, 0)
	foundDelete := false
	for _, e := range deltaAfter.Entries {
		if e.Operation == "delete" && e.EntityID == entryID {
			foundDelete = true
			break
		}
	}
	if !foundDelete {
		t.Error("delete operation not found in sync delta")
	}
}

// --- Snapshot Tests ---

func TestSync_SnapshotNotAvailable(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After: 60, got %q", w.Header().Get("Retry-After"))
	}
}

func TestSync_SnapshotAfterGeneration(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// Generate snapshot
	ctx := context.Background()
	if err := managed.Store.GenerateSnapshot(ctx); err != nil {
		t.Fatalf("GenerateSnapshot() error = %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %q", w.Header().Get("Content-Type"))
	}
	// Should look like a SQLite file
	body := w.Body.String()
	if !strings.HasPrefix(body, "SQLite format 3") {
		t.Errorf("body doesn't look like SQLite file, prefix: %q", body[:min(len(body), 20)])
	}
}

func TestSync_SnapshotIncludesChangeLog(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// Push some entries
	pushEntries(t, router, "test-store", 3, "snapshot-client")

	// Generate snapshot
	ctx := context.Background()
	if err := managed.Store.GenerateSnapshot(ctx); err != nil {
		t.Fatalf("GenerateSnapshot() error = %v", err)
	}

	// Download snapshot
	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Write snapshot to temp file and open as SQLite
	snapshotDB := openSnapshotDB(t, w.Body.Bytes())

	// Verify change_log table exists and has entries
	var count int
	err := snapshotDB.QueryRow("SELECT COUNT(*) FROM change_log").Scan(&count)
	if err != nil {
		t.Fatalf("query snapshot change_log: %v", err)
	}
	if count < 3 {
		t.Errorf("expected at least 3 change_log entries in snapshot, got %d", count)
	}
}

func TestSync_SnapshotIncludesSyncMeta(t *testing.T) {
	manager, handler, managed := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	// Set a sync meta value
	ctx := context.Background()
	if err := managed.Store.SetSyncMeta(ctx, "test_key", "test_value"); err != nil {
		t.Fatalf("SetSyncMeta() error = %v", err)
	}

	// Generate and download snapshot
	if err := managed.Store.GenerateSnapshot(ctx); err != nil {
		t.Fatalf("GenerateSnapshot() error = %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/api/v1/stores/test-store/sync/snapshot", nil)
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	snapshotDB := openSnapshotDB(t, w.Body.Bytes())

	var value string
	err := snapshotDB.QueryRow("SELECT value FROM sync_meta WHERE key = 'schema_version'").Scan(&value)
	if err != nil {
		t.Fatalf("query snapshot sync_meta: %v", err)
	}
	if value != "2" {
		t.Errorf("expected schema_version=2, got %q", value)
	}
}

// --- Fixture-Driven Tests ---

func TestSync_PushFixture_ArchitecturalDecisions(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	fixtures := loadRecallFixture(t, "lore-architectural-decisions.json")
	if len(fixtures) != 5 {
		t.Fatalf("expected 5 fixture entries, got %d", len(fixtures))
	}

	entries := make([]engramsync.ChangeLogEntry, len(fixtures))
	for i, f := range fixtures {
		id := fmt.Sprintf("arch-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   fixtureToPayload(t, id, f),
		}
	}

	req := engramsync.PushRequest{
		PushID:        "fixture-arch-push",
		SourceID:      "fixture-client",
		SchemaVersion: 2,
		Entries:       entries,
	}
	doSyncPush(t, router, "test-store", req)

	// Verify all 5 in delta
	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries in delta, got %d", len(resp.Entries))
	}

	// Verify payloads match
	for _, e := range resp.Entries {
		if e.Operation != "upsert" {
			t.Errorf("expected upsert operation, got %q", e.Operation)
		}
		if len(e.Payload) == 0 {
			t.Error("expected non-empty payload")
		}
	}
}

func TestSync_PushFixture_MixedCategories(t *testing.T) {
	manager, handler, _ := setupSyncTestEnv(t)
	router := api.NewRouter(handler, manager)

	fixtures := loadRecallFixture(t, "lore-mixed-categories.json")
	if len(fixtures) != 8 {
		t.Fatalf("expected 8 fixture entries, got %d", len(fixtures))
	}

	entries := make([]engramsync.ChangeLogEntry, len(fixtures))
	for i, f := range fixtures {
		id := fmt.Sprintf("mixed-%03d", i+1)
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   fixtureToPayload(t, id, f),
		}
	}

	req := engramsync.PushRequest{
		PushID:        "fixture-mixed-push",
		SourceID:      "fixture-client",
		SchemaVersion: 2,
		Entries:       entries,
	}
	doSyncPush(t, router, "test-store", req)

	// Verify all 8 entries in delta
	resp := deltaRequest(t, router, "test-store", 0, 0)
	if len(resp.Entries) != 8 {
		t.Errorf("expected 8 entries in delta, got %d", len(resp.Entries))
	}

	// Verify all 8 categories present
	categories := make(map[string]bool)
	for _, e := range resp.Entries {
		var payload struct {
			Category string `json:"category"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Errorf("unmarshal payload: %v", err)
			continue
		}
		categories[payload.Category] = true
	}

	expectedCategories := []string{
		"ARCHITECTURAL_DECISION", "PATTERN_OUTCOME", "INTERFACE_LESSON",
		"EDGE_CASE_DISCOVERY", "IMPLEMENTATION_FRICTION", "TESTING_STRATEGY",
		"DEPENDENCY_BEHAVIOR", "PERFORMANCE_INSIGHT",
	}
	for _, c := range expectedCategories {
		if !categories[c] {
			t.Errorf("category %s not found in delta entries", c)
		}
	}
}

// --- Internal Helpers ---

// pushEntriesWithIDs pushes entries with specific IDs from a given source.
func pushEntriesWithIDs(t *testing.T, router http.Handler, storeID, sourceID string, ids ...string) {
	t.Helper()
	entries := make([]engramsync.ChangeLogEntry, len(ids))
	for i, id := range ids {
		entries[i] = engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   validLorePayloadWithSource(t, id, sourceID),
		}
	}
	req := engramsync.PushRequest{
		PushID:        fmt.Sprintf("push-%s-%d", sourceID, len(ids)),
		SourceID:      sourceID,
		SchemaVersion: 2,
		Entries:       entries,
	}
	doSyncPush(t, router, storeID, req)
}

// doSyncPush sends a push request and asserts 200 OK.
func doSyncPush(t *testing.T, router http.Handler, storeID string, req engramsync.PushRequest) engramsync.PushResponse {
	t.Helper()
	w := doSyncPushRaw(t, router, storeID, req)
	if w.Code != http.StatusOK {
		t.Fatalf("push failed: %d: %s", w.Code, w.Body.String())
	}
	var resp engramsync.PushResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	return resp
}

// doSyncPushRaw sends a push request and returns the raw recorder (for status code checks).
func doSyncPushRaw(t *testing.T, router http.Handler, storeID string, req engramsync.PushRequest) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/api/v1/stores/%s/sync/push", storeID)
	httpReq := httptest.NewRequest(http.MethodPost, url, makePushBody(t, req))
	httpReq.Header.Set("Authorization", "Bearer test-api-key")
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httpReq)
	return w
}

// openSnapshotDB writes snapshot bytes to a temp file and opens as SQLite.
func openSnapshotDB(t *testing.T, data []byte) *sql.DB {
	t.Helper()
	tmpFile := fmt.Sprintf("%s/snapshot.db", t.TempDir())
	if err := writeFile(tmpFile, data); err != nil {
		t.Fatalf("write snapshot file: %v", err)
	}

	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("open snapshot DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func writeFile(path string, data []byte) error {
	f, err := createFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, bytes.NewReader(data))
	return err
}

func createFile(path string) (*fileWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &fileWriter{f}, nil
}

type fileWriter struct {
	*os.File
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
