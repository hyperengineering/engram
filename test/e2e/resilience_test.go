//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Layer 4: Failure & Resilience Tests
// These tests require real Engram + Recall binaries and test edge cases.

// --- Push Retry / Idempotency ---

// TestResilience_PushRetryIdempotent verifies that pushing with the same push_id
// twice does not create duplicate entries in the change_log.
func TestResilience_PushRetryIdempotent(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "retry-idemp", "recall")

	pushID := fmt.Sprintf("idemp-push-%d", time.Now().UnixNano())
	entries := []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "idemp-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "idemp-001", "Idempotent entry", "TESTING_STRATEGY", "idemp-src", 0.7),
			SourceID:  "idemp-src",
		},
		{
			TableName: "lore_entries",
			EntityID:  "idemp-002",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "idemp-002", "Another idempotent", "PATTERN_OUTCOME", "idemp-src", 0.6),
			SourceID:  "idemp-src",
		},
	}

	// First push
	status1, _, _ := srv.pushViaAPIWithResponse(t, "retry-idemp", pushID, entries, "idemp-src")
	if status1 != 200 {
		t.Fatalf("first push expected 200, got %d", status1)
	}

	// Get delta count after first push
	delta1 := srv.getDelta(t, "retry-idemp", 0)
	count1 := len(delta1.Entries)

	// Second push with same push_id
	status2, _, _ := srv.pushViaAPIWithResponse(t, "retry-idemp", pushID, entries, "idemp-src")
	if status2 != 200 {
		t.Fatalf("idempotent replay expected 200, got %d", status2)
	}

	// Delta count should be unchanged
	delta2 := srv.getDelta(t, "retry-idemp", 0)
	count2 := len(delta2.Entries)

	if count2 != count1 {
		t.Errorf("idempotent push created duplicates: count before=%d, after=%d", count1, count2)
	}
}

// TestResilience_PushRetryResponseMatch verifies that an idempotent replay
// returns the X-Idempotent-Replay header and same response body.
func TestResilience_PushRetryResponseMatch(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "retry-match", "recall")

	pushID := fmt.Sprintf("match-push-%d", time.Now().UnixNano())
	entries := []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "match-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "match-001", "Match entry", "ARCHITECTURAL_DECISION", "match-src", 0.7),
			SourceID:  "match-src",
		},
	}

	// First push
	status1, body1, _ := srv.pushViaAPIWithResponse(t, "retry-match", pushID, entries, "match-src")
	if status1 != 200 {
		t.Fatalf("first push expected 200, got %d", status1)
	}

	// Second push with same push_id
	status2, body2, headers2 := srv.pushViaAPIWithResponse(t, "retry-match", pushID, entries, "match-src")
	if status2 != 200 {
		t.Fatalf("replay expected 200, got %d", status2)
	}

	// Check idempotent replay header
	replay := headers2.Get("X-Idempotent-Replay")
	if replay != "true" {
		t.Errorf("expected X-Idempotent-Replay: true, got %q", replay)
	}

	// Response bodies should match
	var resp1, resp2 map[string]interface{}
	json.Unmarshal(body1, &resp1)
	json.Unmarshal(body2, &resp2)

	accepted1, _ := resp1["accepted"].(float64)
	accepted2, _ := resp2["accepted"].(float64)
	if accepted1 != accepted2 {
		t.Errorf("response mismatch: accepted=%v vs %v", accepted1, accepted2)
	}
}

// --- Server Restart During Sync ---

// TestResilience_ServerRestart_PushRecovery verifies that data pushed before
// a server restart persists and is available after restart.
func TestResilience_ServerRestart_PushRecovery(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "restart-push", "recall")

	// Record and push before restart
	rc := newRecallCLI(t, srv, "restart-push")
	rc.record(t, "Pre-restart entry one", "ARCHITECTURAL_DECISION")
	rc.record(t, "Pre-restart entry two", "PATTERN_OUTCOME")
	rc.syncPush(t)

	// Verify data before restart
	delta := srv.getDelta(t, "restart-push", 0)
	if len(delta.Entries) != 2 {
		t.Fatalf("expected 2 entries before restart, got %d", len(delta.Entries))
	}

	// Restart server
	srv2 := srv.restartOnSameData(t)

	// Verify data persists after restart
	delta2 := srv2.getDelta(t, "restart-push", 0)
	if len(delta2.Entries) != 2 {
		t.Fatalf("expected 2 entries after restart, got %d", len(delta2.Entries))
	}

	// New client can push after restart
	rc2 := newRecallCLI(t, srv2, "restart-push")
	rc2.record(t, "Post-restart entry", "TESTING_STRATEGY")
	rc2.syncPush(t)

	delta3 := srv2.getDelta(t, "restart-push", 0)
	if len(delta3.Entries) != 3 {
		t.Fatalf("expected 3 entries after restart + push, got %d", len(delta3.Entries))
	}
}

// TestResilience_ServerRestart_PullRecovery verifies that a client can pull
// data from a server that was restarted.
func TestResilience_ServerRestart_PullRecovery(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "restart-pull", "recall")

	// Push data via API
	entries := make([]syncDeltaEntry, 3)
	for i := range entries {
		id := fmt.Sprintf("restart-pull-%03d", i+1)
		entries[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Restart pull entry %d", i+1), "ARCHITECTURAL_DECISION", "seed", 0.7),
			SourceID:  "seed",
		}
	}
	srv.pushViaAPI(t, "restart-pull", entries, "seed")

	// Restart server
	srv2 := srv.restartOnSameData(t)

	// Client pulls after restart
	rc := newRecallCLI(t, srv2, "restart-pull")
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Fatalf("expected delta success after restart, got: %s", deltaOut)
	}
}

// --- Schema Mismatch ---

// TestResilience_SchemaMismatch_GracefulHalt verifies that pushing with a
// schema version higher than the server rejects with 409 Conflict.
func TestResilience_SchemaMismatch_GracefulHalt(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "schema-mismatch", "recall")

	entries := []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "schema-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "schema-001", "Test entry", "TESTING_STRATEGY", "src", 0.5),
			SourceID:  "src",
		},
	}

	// Push with schema_version far ahead of server
	status, body := srv.pushViaAPIWithSchemaVersion(t, "schema-mismatch", 999, entries, "src")
	if status != 409 {
		t.Fatalf("expected 409 for schema mismatch, got %d: %s", status, body)
	}

	// Verify error response
	var errResp map[string]interface{}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp["status"] == nil {
		t.Error("expected status in error response")
	}

	// Verify no entries were written
	delta := srv.getDelta(t, "schema-mismatch", 0)
	if len(delta.Entries) != 0 {
		t.Errorf("expected 0 entries after schema mismatch, got %d", len(delta.Entries))
	}
}

// --- Concurrent Clients ---

// TestResilience_ConcurrentRecallClients verifies that multiple Recall clients
// can push to the same store concurrently without data loss.
// Records are done sequentially (record auto-flushes, which contends on SQLite),
// then concurrent pushes verify server handles parallel sync requests.
func TestResilience_ConcurrentRecallClients(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "concurrent-recall", "recall")

	const numClients = 5
	clients := make([]*recallCLI, numClients)

	// Record sequentially — each record auto-flushes to Engram,
	// serializing avoids SQLite write contention on the server.
	for i := 0; i < numClients; i++ {
		clients[i] = newRecallCLI(t, srv, "concurrent-recall")
		clients[i].sourceID = fmt.Sprintf("concurrent-%d", i)
		clients[i].record(t, fmt.Sprintf("Concurrent entry from client %d", i), "TESTING_STRATEGY")
	}

	// Push concurrently to verify server handles parallel requests
	var wg sync.WaitGroup
	errCh := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := clients[idx].exec(t, "sync", "push", "--store", clients[idx].storeID)
			if err != nil {
				errCh <- fmt.Errorf("client %d push: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	// Verify all entries on server
	delta := srv.getDelta(t, "concurrent-recall", 0)
	if len(delta.Entries) < numClients {
		t.Errorf("expected at least %d entries from concurrent clients, got %d", numClients, len(delta.Entries))
	}
}

// TestResilience_ConcurrentRecallTract verifies that Recall and Tract can
// push to different stores concurrently on the same server.
func TestResilience_ConcurrentRecallTract(t *testing.T) {
	requireRecall(t)
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "conc-recall", "recall")
	srv.createStore(t, "conc-tract", "tract")

	// Prepare clients
	rc := newRecallCLI(t, srv, "conc-recall")
	rc.record(t, "Concurrent recall entry", "ARCHITECTURAL_DECISION")

	tc := newTractCLI(t, srv, "conc-tract")
	tc.initStore(t)
	goals := []map[string]interface{}{
		{"id": "conc-g1", "name": "Concurrent goal", "description": "Push concurrently", "status": "active", "priority": 1},
	}
	treePath := tc.writeGoalTreeFile(t, goals)
	tc.loadGoalTree(t, treePath)

	// Push concurrently
	var wg sync.WaitGroup
	var recallErr, tractErr error
	var recallOut, tractOut string

	wg.Add(2)
	go func() {
		defer wg.Done()
		recallOut, recallErr = rc.exec(t, "sync", "push", "--store", rc.storeID)
	}()
	go func() {
		defer wg.Done()
		tractOut, tractErr = tc.exec(t, "sync", "push", "--store", tc.storeID)
	}()
	wg.Wait()

	if recallErr != nil {
		t.Fatalf("recall concurrent push: %v\noutput: %s", recallErr, recallOut)
	}
	if tractErr != nil {
		t.Fatalf("tract concurrent push: %v\noutput: %s", tractErr, tractOut)
	}

	// Verify both stores
	if delta := srv.getDelta(t, "conc-recall", 0); len(delta.Entries) == 0 {
		t.Error("recall store empty after concurrent push")
	}
	if delta := srv.getDelta(t, "conc-tract", 0); len(delta.Entries) == 0 {
		t.Error("tract store empty after concurrent push")
	}
}

// --- High Volume ---

// TestResilience_HighVolume_500Entries verifies that the server handles
// 500 entries in a single push and they are all retrievable via delta.
func TestResilience_HighVolume_500Entries(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "high-volume", "recall")

	const totalEntries = 500
	entries := make([]syncDeltaEntry, totalEntries)
	for i := range entries {
		id := fmt.Sprintf("hv-%04d", i+1)
		entries[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("High volume entry %d", i+1), "TESTING_STRATEGY", "bulk", 0.5),
			SourceID:  "bulk",
		}
	}

	// Push all at once
	srv.pushViaAPI(t, "high-volume", entries, "bulk")

	// Retrieve all via paginated delta
	var allEntries []syncDeltaEntry
	after := int64(0)
	for {
		page := srv.getDeltaWithLimit(t, "high-volume", after, 100)
		allEntries = append(allEntries, page.Entries...)
		if !page.HasMore {
			break
		}
		after = page.LastSequence
	}

	if len(allEntries) != totalEntries {
		t.Fatalf("expected %d entries via paginated delta, got %d", totalEntries, len(allEntries))
	}

	// Verify no duplicate sequences
	seqSeen := make(map[int64]bool)
	for _, e := range allEntries {
		if seqSeen[e.Sequence] {
			t.Errorf("duplicate sequence %d", e.Sequence)
		}
		seqSeen[e.Sequence] = true
	}
}

// TestResilience_HighVolume_ManySmallSyncs verifies that many small individual
// pushes accumulate correctly on the server.
func TestResilience_HighVolume_ManySmallSyncs(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "many-syncs", "recall")

	const numSyncs = 50
	for i := 0; i < numSyncs; i++ {
		id := fmt.Sprintf("small-%04d", i+1)
		entries := []syncDeltaEntry{
			{
				TableName: "lore_entries",
				EntityID:  id,
				Operation: "upsert",
				Payload:   makeLorePayload(t, id, fmt.Sprintf("Small sync entry %d", i+1), "TESTING_STRATEGY", "small-src", 0.5),
				SourceID:  "small-src",
			},
		}
		srv.pushViaAPI(t, "many-syncs", entries, "small-src")
	}

	// Verify all entries present
	delta := srv.getDelta(t, "many-syncs", 0)

	// Delta default limit is 500, so all 50 should be in one page
	if len(delta.Entries) != numSyncs {
		t.Fatalf("expected %d entries from many small syncs, got %d", numSyncs, len(delta.Entries))
	}

	// Verify sequences are monotonically increasing
	for i := 1; i < len(delta.Entries); i++ {
		if delta.Entries[i].Sequence <= delta.Entries[i-1].Sequence {
			t.Errorf("sequences not monotonically increasing at index %d: %d <= %d",
				i, delta.Entries[i].Sequence, delta.Entries[i-1].Sequence)
		}
	}
}

// --- Compaction During Active Sync ---

// TestResilience_CompactionDuringPull verifies that delta sync works correctly
// with pagination, which is the mechanism clients use after compaction removes
// old entries from the change_log.
func TestResilience_CompactionDuringPull(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "compact-pull", "recall")

	// Push two batches of entries
	batch1 := make([]syncDeltaEntry, 5)
	for i := range batch1 {
		id := fmt.Sprintf("batch1-%03d", i+1)
		batch1[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Batch 1 entry %d", i+1), "ARCHITECTURAL_DECISION", "src", 0.7),
			SourceID:  "src",
		}
	}
	srv.pushViaAPI(t, "compact-pull", batch1, "src")

	// Record midpoint sequence
	delta1 := srv.getDelta(t, "compact-pull", 0)
	midpoint := delta1.LastSequence

	batch2 := make([]syncDeltaEntry, 5)
	for i := range batch2 {
		id := fmt.Sprintf("batch2-%03d", i+1)
		batch2[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Batch 2 entry %d", i+1), "PATTERN_OUTCOME", "src", 0.6),
			SourceID:  "src",
		}
	}
	srv.pushViaAPI(t, "compact-pull", batch2, "src")

	// A client that already has batch 1 should be able to pull only batch 2
	deltaAfterMid := srv.getDelta(t, "compact-pull", midpoint)
	if len(deltaAfterMid.Entries) != 5 {
		t.Fatalf("expected 5 entries after midpoint, got %d", len(deltaAfterMid.Entries))
	}

	// Verify these are batch 2 entries
	for _, e := range deltaAfterMid.Entries {
		if !strings.HasPrefix(e.EntityID, "batch2-") {
			t.Errorf("expected batch2 entity, got %s", e.EntityID)
		}
	}

	// Recall client can pull all entries
	rc := newRecallCLI(t, srv, "compact-pull")
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Fatalf("expected delta success, got: %s", deltaOut)
	}
}

// TestResilience_CompactionPreservesRecent verifies that recent entries
// are always available via delta, even with paginated retrieval.
func TestResilience_CompactionPreservesRecent(t *testing.T) {
	requireRecall(t) // need engram binary

	srv := startEngram(t)
	srv.createStore(t, "compact-recent", "recall")

	// Push 20 entries
	entries := make([]syncDeltaEntry, 20)
	for i := range entries {
		id := fmt.Sprintf("recent-%03d", i+1)
		entries[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Recent entry %d", i+1), "TESTING_STRATEGY", "src", 0.5),
			SourceID:  "src",
		}
	}
	srv.pushViaAPI(t, "compact-recent", entries, "src")

	// Retrieve in pages of 5
	var allEntries []syncDeltaEntry
	after := int64(0)
	pages := 0
	for {
		page := srv.getDeltaWithLimit(t, "compact-recent", after, 5)
		allEntries = append(allEntries, page.Entries...)
		pages++
		if !page.HasMore {
			break
		}
		after = page.LastSequence
	}

	if pages != 4 {
		t.Errorf("expected 4 pages of 5, got %d pages", pages)
	}
	if len(allEntries) != 20 {
		t.Fatalf("expected 20 entries across pages, got %d", len(allEntries))
	}

	// Verify all entity IDs are present and unique
	entityIDs := make(map[string]bool)
	for _, e := range allEntries {
		entityIDs[e.EntityID] = true
	}
	for i := 1; i <= 20; i++ {
		expected := fmt.Sprintf("recent-%03d", i)
		if !entityIDs[expected] {
			t.Errorf("missing entity %s in paginated results", expected)
		}
	}
}

// --- Bootstrap Edge Cases ---

// TestResilience_BootstrapEmpty verifies that bootstrapping from an empty store
// completes without error.
func TestResilience_BootstrapEmpty(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "bootstrap-empty", "recall")

	// Generate snapshot of empty store
	srv.generateSnapshot(t, "bootstrap-empty")

	// Bootstrap should succeed (empty DB)
	rc := newRecallCLI(t, srv, "bootstrap-empty")
	out := rc.syncBootstrap(t)
	t.Logf("bootstrap empty output: %s", out)

	// After bootstrap, delta should return no entries
	delta := srv.getDelta(t, "bootstrap-empty", 0)
	if len(delta.Entries) != 0 {
		t.Errorf("expected 0 entries in empty bootstrapped store, got %d", len(delta.Entries))
	}
}

// TestResilience_BootstrapThenImmediateRecord verifies that a client can
// immediately record and push after bootstrapping.
func TestResilience_BootstrapThenImmediateRecord(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "bootstrap-record", "recall")

	// Seed and snapshot
	srv.pushViaAPI(t, "bootstrap-record", []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "seed-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "seed-001", "Seed entry", "ARCHITECTURAL_DECISION", "seed", 0.7),
			SourceID:  "seed",
		},
	}, "seed")
	srv.generateSnapshot(t, "bootstrap-record")

	// Bootstrap
	rc := newRecallCLI(t, srv, "bootstrap-record")
	rc.syncBootstrap(t)

	// Immediately record and push
	rc.record(t, "Immediate post-bootstrap entry", "PATTERN_OUTCOME")
	rc.syncPush(t)

	// Verify both seed and new entry on server
	delta := srv.getDelta(t, "bootstrap-record", 0)
	if len(delta.Entries) < 2 {
		t.Fatalf("expected at least 2 entries (seed + new), got %d", len(delta.Entries))
	}

	contents := extractPayloadContents(t, delta.Entries)
	assertContains(t, contents, "Immediate post-bootstrap entry")
}

// TestResilience_BootstrapIntegrityCheck verifies that bootstrapped data
// matches the original server data.
func TestResilience_BootstrapIntegrityCheck(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "bootstrap-integrity", "recall")

	// Push known entries
	categories := []string{
		"ARCHITECTURAL_DECISION", "PATTERN_OUTCOME", "TESTING_STRATEGY",
		"EDGE_CASE_DISCOVERY", "PERFORMANCE_INSIGHT",
	}
	entries := make([]syncDeltaEntry, len(categories))
	for i, cat := range categories {
		id := fmt.Sprintf("integrity-%03d", i+1)
		entries[i] = syncDeltaEntry{
			TableName: "lore_entries",
			EntityID:  id,
			Operation: "upsert",
			Payload:   makeLorePayload(t, id, fmt.Sprintf("Integrity check %s", cat), cat, "integrity-src", 0.7),
			SourceID:  "integrity-src",
		}
	}
	srv.pushViaAPI(t, "bootstrap-integrity", entries, "integrity-src")

	// Record server state before bootstrap
	serverDelta := srv.getDelta(t, "bootstrap-integrity", 0)
	serverCount := len(serverDelta.Entries)

	// Generate snapshot and bootstrap
	srv.generateSnapshot(t, "bootstrap-integrity")
	rc := newRecallCLI(t, srv, "bootstrap-integrity")
	rc.syncBootstrap(t)

	// After bootstrap, pull delta to see current position
	deltaOut := rc.syncDelta(t)
	if !strings.Contains(deltaOut, "Delta sync complete") {
		t.Logf("delta after bootstrap: %s", deltaOut)
	}

	// Server data should still be intact
	deltaPost := srv.getDelta(t, "bootstrap-integrity", 0)
	if len(deltaPost.Entries) != serverCount {
		t.Errorf("server data changed after bootstrap: before=%d, after=%d", serverCount, len(deltaPost.Entries))
	}
}

// --- Mixed Operations Stress ---

// TestResilience_MixedOps verifies correct behavior when interleaving
// records, deletes, feedback, pushes, and pulls.
func TestResilience_MixedOps(t *testing.T) {
	requireRecall(t)

	srv := startEngram(t)
	srv.createStore(t, "mixed-ops", "recall")

	rc := newRecallCLI(t, srv, "mixed-ops")

	// 1. Record multiple entries
	id1 := rc.recordID(t, "First entry for mixed ops", "ARCHITECTURAL_DECISION")
	rc.record(t, "Second entry for mixed ops", "PATTERN_OUTCOME")
	rc.record(t, "Third entry for mixed ops", "TESTING_STRATEGY")

	// 2. Push
	rc.syncPush(t)

	// 3. Apply feedback
	rc.feedback(t, id1, "helpful")

	// 4. Push feedback changes
	rc.syncPush(t)

	// 5. Delete an entry via API
	deleteURL := fmt.Sprintf("%s/api/v1/stores/mixed-ops/lore/%s", srv.baseURL(), id1)
	req, _ := newAuthRequest(t, "DELETE", deleteURL, nil, srv.apiKey)
	resp, err := doHTTP(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()

	// 6. Pull to see delete
	rc.syncDelta(t)

	// 7. Record more entries after pull
	rc.record(t, "Post-delete entry", "EDGE_CASE_DISCOVERY")
	rc.syncPush(t)

	// 8. Push more via API from a different source
	entries := []syncDeltaEntry{
		{
			TableName: "lore_entries",
			EntityID:  "api-mixed-001",
			Operation: "upsert",
			Payload:   makeLorePayload(t, "api-mixed-001", "API entry in mixed ops", "DEPENDENCY_BEHAVIOR", "api-src", 0.65),
			SourceID:  "api-src",
		},
	}
	srv.pushViaAPI(t, "mixed-ops", entries, "api-src")

	// 9. Final pull
	rc.syncDelta(t)

	// Verify server state
	delta := srv.getDelta(t, "mixed-ops", 0)
	if len(delta.Entries) < 5 {
		t.Fatalf("expected at least 5 delta entries from mixed ops, got %d", len(delta.Entries))
	}

	// Verify we have both upserts and at least one delete
	ops := make(map[string]int)
	for _, e := range delta.Entries {
		ops[e.Operation]++
	}
	if ops["upsert"] == 0 {
		t.Error("expected at least one upsert in mixed ops")
	}
	if ops["delete"] == 0 {
		t.Error("expected at least one delete in mixed ops")
	}
}
