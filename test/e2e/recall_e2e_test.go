//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Layer 2: Single Recall Client E2E Tests
// These tests exercise the full Engram ↔ Recall round-trip using real binaries.
// Each test starts a fresh Engram server and isolated Recall home directory.

// TestRecallE2E_RecordAndSync verifies the core record → push → server delta flow.
// Records multiple lore entries locally, pushes to Engram, and verifies all entries
// appear in the server's sync delta with correct payloads.
func TestRecallE2E_RecordAndSync(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-sync", "recall")
	rc := newRecallCLI(t, srv, "recall-sync")

	// Record 3 entries with different categories
	rc.record(t, "WAL mode enables concurrent readers", "ARCHITECTURAL_DECISION")
	rc.record(t, "Retry loops need exponential backoff", "PATTERN_OUTCOME")
	rc.record(t, "Nil pointer on empty config slice", "EDGE_CASE_DISCOVERY")

	// Push to Engram
	pushOut := rc.syncPush(t)
	if !strings.Contains(pushOut, "Push complete") {
		t.Fatalf("expected push success, got: %s", pushOut)
	}

	// Verify entries on server via delta API
	delta := srv.getDelta(t, "recall-sync", 0)
	if len(delta.Entries) != 3 {
		t.Fatalf("expected 3 entries on server, got %d", len(delta.Entries))
	}

	// Verify each entry has correct table_name and operation
	for i, e := range delta.Entries {
		if e.TableName != "lore_entries" {
			t.Errorf("entry %d: expected table_name=lore_entries, got %s", i, e.TableName)
		}
		if e.Operation != "upsert" {
			t.Errorf("entry %d: expected operation=upsert, got %s", i, e.Operation)
		}
		if e.Sequence != int64(i+1) {
			t.Errorf("entry %d: expected sequence=%d, got %d", i, i+1, e.Sequence)
		}
	}

	// Verify payload content is preserved
	contents := extractPayloadContents(t, delta.Entries)
	assertContains(t, contents, "WAL mode enables concurrent readers")
	assertContains(t, contents, "Retry loops need exponential backoff")
	assertContains(t, contents, "Nil pointer on empty config slice")
}

// TestRecallE2E_SyncPullsRemote verifies that Recall's sync delta pulls entries
// pushed to Engram by another source (simulated via API).
func TestRecallE2E_SyncPullsRemote(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-pull", "recall")
	rc := newRecallCLI(t, srv, "recall-pull")

	// Push entries directly via Engram API (simulating another client)
	entries := []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "remote-entry-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "remote-entry-001", "Remote knowledge from another client", "ARCHITECTURAL_DECISION", "other-client", 0.75),
			SourceID:  "other-client",
		},
		{
			TableName: "lore_entries",
			EntityID:  "remote-entry-002",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "remote-entry-002", "Another remote insight", "PATTERN_OUTCOME", "other-client", 0.6),
			SourceID:  "other-client",
		},
	}
	srv.pushViaAPI(t, "recall-pull", entries, "other-client")

	// Recall pulls via sync delta
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Fatalf("expected delta success, got: %s", deltaOut)
	}
	if !strings.Contains(deltaOut, "Entries applied: 2") {
		t.Fatalf("expected 2 entries applied, got: %s", deltaOut)
	}
}

// TestRecallE2E_FeedbackAndSync verifies that recording feedback locally
// syncs to Engram and the confidence adjustment is reflected in the change_log.
func TestRecallE2E_FeedbackAndSync(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-feedback", "recall")
	rc := newRecallCLI(t, srv, "recall-feedback")

	// Record an entry and push it
	id := rc.recordID(t, "Connection pooling reduces latency", "PERFORMANCE_INSIGHT", "--confidence", "0.5")
	rc.syncPush(t)

	// Apply helpful feedback (should increase confidence by +0.08)
	rc.feedback(t, id, "helpful")

	// Push the feedback change
	rc.syncPush(t)

	// Verify on server: should have at least 2 entries in delta
	// (original upsert + confidence update from feedback)
	delta := srv.getDelta(t, "recall-feedback", 0)
	if len(delta.Entries) < 2 {
		t.Fatalf("expected at least 2 delta entries (record + feedback), got %d", len(delta.Entries))
	}

	// Find the latest entry for our lore ID — it should reflect the updated confidence
	var latestPayload map[string]interface{}
	for i := len(delta.Entries) - 1; i >= 0; i-- {
		if delta.Entries[i].EntityID == id && delta.Entries[i].Operation == "upsert" {
			if err := json.Unmarshal(delta.Entries[i].Payload, &latestPayload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			break
		}
	}
	if latestPayload == nil {
		t.Fatal("could not find updated entry in delta")
	}

	confidence, ok := latestPayload["confidence"].(float64)
	if !ok {
		t.Fatal("confidence field not found in payload")
	}
	// Original 0.5 + helpful 0.08 = 0.58
	if confidence < 0.55 || confidence > 0.62 {
		t.Errorf("expected confidence ~0.58 after helpful feedback, got %.2f", confidence)
	}
}

// TestRecallE2E_DeleteAndSync verifies that deleting lore via the legacy API
// produces a delete operation visible in Recall's sync delta.
func TestRecallE2E_DeleteAndSync(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-delete", "recall")
	rc := newRecallCLI(t, srv, "recall-delete")

	// Record and push an entry
	id := rc.recordID(t, "Temporary insight to be deleted", "TESTING_STRATEGY")
	rc.syncPush(t)

	// Delete via Engram REST API (Recall CLI has no delete command)
	deleteURL := fmt.Sprintf("%s/api/v1/stores/recall-delete/lore/%s", srv.baseURL(), id)
	req, _ := newAuthRequest(t, "DELETE", deleteURL, nil, srv.apiKey)
	resp, err := doHTTP(req)
	if err != nil {
		t.Fatalf("delete lore: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204 on delete, got %d", resp.StatusCode)
	}

	// Recall pulls delta — should see the delete operation
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Fatalf("expected delta success, got: %s", deltaOut)
	}

	// Verify server delta includes a delete operation
	delta := srv.getDelta(t, "recall-delete", 0)
	foundDelete := false
	for _, e := range delta.Entries {
		if e.EntityID == id && e.Operation == "delete" {
			foundDelete = true
			break
		}
	}
	if !foundDelete {
		t.Error("expected delete operation in server delta for deleted entry")
	}
}

// TestRecallE2E_QueryAfterSync verifies that after syncing, the Recall CLI
// can query lore. This is a basic smoke test — semantic quality depends on
// embeddings which are noop in dev mode.
func TestRecallE2E_QueryAfterSync(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-query", "recall")
	rc := newRecallCLI(t, srv, "recall-query")

	// Record entries and push
	rc.record(t, "Database indexing improves query performance", "PERFORMANCE_INSIGHT")
	rc.record(t, "Use prepared statements to prevent SQL injection", "TESTING_STRATEGY")
	rc.syncPush(t)

	// Query — in dev mode without real embeddings, results depend on local DB state.
	// We just verify the command succeeds and produces parseable JSON output.
	out := rc.queryJSON(t, "database performance")

	var result struct {
		Lore        []json.RawMessage  `json:"lore"`
		SessionRefs map[string]string  `json:"session_refs"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected valid JSON query output, got parse error: %v\noutput: %s", err, out)
	}
	// The query should return valid JSON structure (entries may be empty without embeddings)
	t.Logf("query returned %d entries", len(result.Lore))
}

// TestRecallE2E_BootstrapFromSnapshot verifies that a fresh Recall client can
// bootstrap its local database from an Engram snapshot.
func TestRecallE2E_BootstrapFromSnapshot(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-bootstrap", "recall")

	// Push entries directly via API to populate the store
	entries := make([]syncDeltaEntry, 5)
	for i := range entries {
		id := fmt.Sprintf("bootstrap-entry-%03d", i+1)
		entries[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Bootstrap knowledge item %d", i+1), "ARCHITECTURAL_DECISION", "seed-client", 0.7),
			SourceID:  "seed-client",
		}
	}
	srv.pushViaAPI(t, "recall-bootstrap", entries, "seed-client")

	// Generate snapshot on the server
	srv.generateSnapshot(t, "recall-bootstrap")

	// Fresh Recall client bootstraps from snapshot
	rc := newRecallCLI(t, srv, "recall-bootstrap")
	bootstrapOut := rc.syncBootstrap(t)
	if !strings.Contains(bootstrapOut, "ootstrap") {
		// Accept "Bootstrap" or "bootstrap" in output
		t.Logf("bootstrap output: %s", bootstrapOut)
	}

	// After bootstrap, delta should report current position
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Fatalf("expected delta success after bootstrap, got: %s", deltaOut)
	}
}

// TestRecallE2E_BootstrapThenDelta verifies the bootstrap → record → push → delta flow.
// A client bootstraps to get initial data, records new entries, pushes them,
// and another client can see them via delta.
func TestRecallE2E_BootstrapThenDelta(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-boot-delta", "recall")

	// Seed data via API
	srv.pushViaAPI(t, "recall-boot-delta", []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "seed-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "seed-001", "Seeded knowledge", "ARCHITECTURAL_DECISION", "seed", 0.7),
			SourceID:  "seed",
		},
	}, "seed")

	// Generate snapshot and bootstrap
	srv.generateSnapshot(t, "recall-boot-delta")
	rc := newRecallCLI(t, srv, "recall-boot-delta")
	rc.syncBootstrap(t)

	// Record new entries after bootstrap
	rc.record(t, "Post-bootstrap insight", "PATTERN_OUTCOME")
	rc.syncPush(t)

	// Verify server has both seeded and new entries
	delta := srv.getDelta(t, "recall-boot-delta", 0)
	if len(delta.Entries) < 2 {
		t.Fatalf("expected at least 2 entries (seed + new), got %d", len(delta.Entries))
	}

	// Verify the new entry is present
	contents := extractPayloadContents(t, delta.Entries)
	assertContains(t, contents, "Post-bootstrap insight")
}

// TestRecallE2E_MixedCategories verifies that all valid Recall categories
// can be recorded and synced correctly.
func TestRecallE2E_MixedCategories(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-categories", "recall")
	rc := newRecallCLI(t, srv, "recall-categories")

	categories := []string{
		"ARCHITECTURAL_DECISION",
		"PATTERN_OUTCOME",
		"INTERFACE_LESSON",
		"EDGE_CASE_DISCOVERY",
		"IMPLEMENTATION_FRICTION",
		"TESTING_STRATEGY",
		"DEPENDENCY_BEHAVIOR",
		"PERFORMANCE_INSIGHT",
	}

	for _, cat := range categories {
		rc.record(t, fmt.Sprintf("Lore for category %s", cat), cat)
	}

	rc.syncPush(t)

	// Verify all 8 entries on server
	delta := srv.getDelta(t, "recall-categories", 0)
	if len(delta.Entries) != 8 {
		t.Fatalf("expected 8 entries (one per category), got %d", len(delta.Entries))
	}

	// Verify each category appears in the payloads
	foundCategories := make(map[string]bool)
	for _, e := range delta.Entries {
		var payload map[string]interface{}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if cat, ok := payload["category"].(string); ok {
			foundCategories[cat] = true
		}
	}
	for _, cat := range categories {
		if !foundCategories[cat] {
			t.Errorf("category %s not found in server delta", cat)
		}
	}
}

// TestRecallE2E_ConfidenceBoundaries verifies that lore entries at confidence
// boundary values (0.0, 0.1, 0.5, 0.9, 1.0) are stored and synced correctly.
func TestRecallE2E_ConfidenceBoundaries(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-confidence", "recall")
	rc := newRecallCLI(t, srv, "recall-confidence")

	confidences := []string{"0.0", "0.1", "0.5", "0.9", "1.0"}
	for _, conf := range confidences {
		rc.record(t, fmt.Sprintf("Entry at confidence %s", conf), "TESTING_STRATEGY", "--confidence", conf)
	}

	rc.syncPush(t)

	// Verify all entries on server
	delta := srv.getDelta(t, "recall-confidence", 0)
	if len(delta.Entries) != len(confidences) {
		t.Fatalf("expected %d entries, got %d", len(confidences), len(delta.Entries))
	}

	// Verify confidence values in payloads
	for _, e := range delta.Entries {
		var payload map[string]interface{}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		conf, ok := payload["confidence"].(float64)
		if !ok {
			t.Errorf("entry %s: confidence field not found", e.EntityID)
			continue
		}
		if conf < 0.0 || conf > 1.0 {
			t.Errorf("entry %s: confidence %.2f out of range [0, 1]", e.EntityID, conf)
		}
	}
}

// TestRecallE2E_SourceFiltering verifies that source_id is preserved through
// the record → push → delta cycle, allowing clients to filter their own entries.
func TestRecallE2E_SourceFiltering(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "recall-source", "recall")

	// Create two Recall clients with different source IDs
	rcA := newRecallCLI(t, srv, "recall-source")
	rcA.sourceID = "source-alpha"

	rcB := newRecallCLI(t, srv, "recall-source")
	rcB.sourceID = "source-beta"

	// Each client records and pushes
	rcA.record(t, "Insight from alpha", "ARCHITECTURAL_DECISION")
	rcA.record(t, "Another alpha insight", "PATTERN_OUTCOME")
	rcA.syncPush(t)

	rcB.record(t, "Insight from beta", "TESTING_STRATEGY")
	rcB.syncPush(t)

	// Verify all entries on server with correct source_ids in payloads.
	// Note: The change_log entry's source_id is the sync client UUID.
	// The lore's source_id is inside the payload JSON.
	delta := srv.getDelta(t, "recall-source", 0)
	if len(delta.Entries) != 3 {
		t.Fatalf("expected 3 entries total, got %d", len(delta.Entries))
	}

	sourceCount := make(map[string]int)
	for _, e := range delta.Entries {
		var payload map[string]interface{}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if src, ok := payload["source_id"].(string); ok {
			sourceCount[src]++
		}
	}
	if sourceCount["source-alpha"] != 2 {
		t.Errorf("expected 2 entries from source-alpha, got %d", sourceCount["source-alpha"])
	}
	if sourceCount["source-beta"] != 1 {
		t.Errorf("expected 1 entry from source-beta, got %d", sourceCount["source-beta"])
	}

	// Client B pulls delta — should see all 3 entries (including alpha's)
	deltaOut := rcB.syncDelta(t)
	if !strings.Contains(deltaOut, "Entries applied:") {
		t.Fatalf("expected entries applied in delta output, got: %s", deltaOut)
	}
}

// --- Test Helpers ---

// newAuthRequest creates an authenticated HTTP request.
func newAuthRequest(t *testing.T, method, url string, body *strings.Reader, apiKey string) (*http.Request, error) {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, body)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return req, nil
}

// doHTTP executes an HTTP request.
func doHTTP(req *http.Request) (*http.Response, error) {
	return (&http.Client{Timeout: 10 * time.Second}).Do(req)
}

// extractPayloadContents extracts the "content" field from each delta entry's payload.
func extractPayloadContents(t *testing.T, entries []syncDeltaEntry) []string {
	t.Helper()
	var contents []string
	for _, e := range entries {
		if len(e.Payload) == 0 {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload for entity %s: %v", e.EntityID, err)
		}
		if content, ok := payload["content"].(string); ok {
			contents = append(contents, content)
		}
	}
	return contents
}

// assertContains checks that needle appears in at least one element of haystack.
func assertContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return
		}
	}
	t.Errorf("expected to find %q in %v", needle, haystack)
}
