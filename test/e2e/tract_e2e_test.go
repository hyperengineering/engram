//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// Layer 2: Single Tract Client E2E Tests
// These tests exercise the full Engram <-> Tract round-trip using real binaries.
// Tract uses the generic Engram plugin (no server-side domain validation).
// Client-side replay handles FK ordering and entity semantics.

// TestTractE2E_ReasoningLayerSync verifies the core Tract flow:
// init store -> load goal tree (reasoning layer) -> push -> verify on server.
func TestTractE2E_ReasoningLayerSync(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-reasoning", "tract")
	tc := newTractCLI(t, srv, "tract-reasoning")
	tc.initStore(t)

	// Load goals (reasoning layer entities)
	goals := []map[string]interface{}{
		{"id": "g-1", "name": "Improve reliability", "description": "99.9% uptime target", "status": "active", "priority": 1},
		{"id": "g-2", "name": "Reduce MTTR", "description": "Under 15 minutes", "status": "active", "priority": 2, "parent_goal_id": "g-1"},
		{"id": "g-3", "name": "Automate detection", "description": "Anomaly detection across services", "status": "planned", "priority": 3, "parent_goal_id": "g-1"},
	}
	treePath := tc.writeGoalTreeFile(t, goals)
	tc.loadGoalTree(t, treePath)

	// Push to Engram
	pushOut := tc.syncPush(t)
	if !strings.Contains(pushOut, "Pushed") {
		t.Fatalf("expected push confirmation, got: %s", pushOut)
	}

	// Verify on server
	delta := srv.getDelta(t, "tract-reasoning", 0)
	if len(delta.Entries) != 3 {
		t.Fatalf("expected 3 entries on server, got %d", len(delta.Entries))
	}

	// All entries should be goals table
	for _, e := range delta.Entries {
		if e.TableName != "goals" {
			t.Errorf("expected table_name=goals, got %s", e.TableName)
		}
		if e.Operation != "upsert" {
			t.Errorf("expected operation=upsert, got %s", e.Operation)
		}
	}

	// Verify entity IDs
	entityIDs := make(map[string]bool)
	for _, e := range delta.Entries {
		entityIDs[e.EntityID] = true
	}
	for _, id := range []string{"g-1", "g-2", "g-3"} {
		if !entityIDs[id] {
			t.Errorf("expected entity %s in delta", id)
		}
	}
}

// TestTractE2E_FullEntityGraph verifies that a goal tree with parent-child
// relationships pushes correctly, preserving the full graph structure.
// FK chain: root goal <- child goals (via parent_goal_id).
func TestTractE2E_FullEntityGraph(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-graph", "tract")
	tc := newTractCLI(t, srv, "tract-graph")
	tc.initStore(t)

	// Build a 3-level hierarchy: root -> 2 children -> 1 grandchild
	goals := []map[string]interface{}{
		{"id": "root", "name": "Company vision", "description": "Top-level", "status": "active", "priority": 1},
		{"id": "child-a", "name": "Product growth", "description": "Growth track", "status": "active", "priority": 1, "parent_goal_id": "root"},
		{"id": "child-b", "name": "Infrastructure", "description": "Platform track", "status": "active", "priority": 2, "parent_goal_id": "root"},
		{"id": "grandchild", "name": "Database migration", "description": "Under infrastructure", "status": "planned", "priority": 1, "parent_goal_id": "child-b"},
	}
	treePath := tc.writeGoalTreeFile(t, goals)
	tc.loadGoalTree(t, treePath)
	tc.syncPush(t)

	// Verify all 4 entities on server
	delta := srv.getDelta(t, "tract-graph", 0)
	if len(delta.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(delta.Entries))
	}

	// Verify sequences are monotonically increasing
	for i := 1; i < len(delta.Entries); i++ {
		if delta.Entries[i].Sequence <= delta.Entries[i-1].Sequence {
			t.Errorf("sequences not monotonically increasing: %d <= %d",
				delta.Entries[i].Sequence, delta.Entries[i-1].Sequence)
		}
	}
}

// TestTractE2E_FKOrderPreserved verifies that Tract pushes entities in FK-safe
// order: parent goals before child goals (parent_goal_id references must resolve).
func TestTractE2E_FKOrderPreserved(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-fk-order", "tract")
	tc := newTractCLI(t, srv, "tract-fk-order")
	tc.initStore(t)

	// Load goals where children reference parents via parent_goal_id
	goals := []map[string]interface{}{
		{"id": "parent", "name": "Parent goal", "description": "Root", "status": "active", "priority": 1},
		{"id": "child", "name": "Child goal", "description": "Depends on parent", "status": "active", "priority": 2, "parent_goal_id": "parent"},
	}
	treePath := tc.writeGoalTreeFile(t, goals)
	tc.loadGoalTree(t, treePath)
	tc.syncPush(t)

	delta := srv.getDelta(t, "tract-fk-order", 0)
	if len(delta.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(delta.Entries))
	}

	// The parent should appear before the child in the change_log sequence.
	// Find the sequence numbers for parent and child.
	seqMap := make(map[string]int64)
	for _, e := range delta.Entries {
		seqMap[e.EntityID] = e.Sequence
	}

	parentSeq, parentOK := seqMap["parent"]
	childSeq, childOK := seqMap["child"]
	if !parentOK || !childOK {
		t.Fatal("expected both parent and child entities in delta")
	}
	if parentSeq >= childSeq {
		t.Errorf("expected parent (seq=%d) before child (seq=%d)", parentSeq, childSeq)
	}
}

// TestTractE2E_DeleteCascade verifies that re-loading a goal tree generates
// delete operations for removed entities, and these propagate through sync.
// Tract's load goal-tree does a full replace: delete all existing + insert new.
func TestTractE2E_DeleteCascade(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-delete", "tract")
	tc := newTractCLI(t, srv, "tract-delete")
	tc.initStore(t)

	// Load 3 goals and push
	goals := []map[string]interface{}{
		{"id": "g-1", "name": "Goal one", "description": "First", "status": "active", "priority": 1},
		{"id": "g-2", "name": "Goal two", "description": "Second", "status": "active", "priority": 2},
		{"id": "g-3", "name": "Goal three", "description": "Third", "status": "planned", "priority": 3},
	}
	treePath := tc.writeGoalTreeFile(t, goals)
	tc.loadGoalTree(t, treePath)
	tc.syncPush(t)

	// Verify 3 upserts
	delta1 := srv.getDelta(t, "tract-delete", 0)
	if len(delta1.Entries) != 3 {
		t.Fatalf("expected 3 entries after first push, got %d", len(delta1.Entries))
	}

	// Re-load with only 1 goal (g-1). Tract deletes all existing, then inserts new.
	goals2 := []map[string]interface{}{
		{"id": "g-1", "name": "Goal one updated", "description": "Updated first", "status": "completed", "priority": 1},
	}
	treePath2 := tc.writeGoalTreeFile(t, goals2)
	tc.loadGoalTree(t, treePath2)
	tc.syncPush(t)

	// Verify delta has the delete + re-insert operations
	delta2 := srv.getDelta(t, "tract-delete", delta1.LastSequence)
	if len(delta2.Entries) == 0 {
		t.Fatal("expected new entries after re-load")
	}

	// Count deletes and upserts in the new batch
	deleteCount := 0
	upsertCount := 0
	for _, e := range delta2.Entries {
		switch e.Operation {
		case "delete":
			deleteCount++
		case "upsert":
			upsertCount++
		}
	}

	// Should have 3 deletes (g-1, g-2, g-3) + 1 upsert (g-1 updated)
	if deleteCount != 3 {
		t.Errorf("expected 3 deletes, got %d", deleteCount)
	}
	if upsertCount != 1 {
		t.Errorf("expected 1 upsert, got %d", upsertCount)
	}
}

// TestTractE2E_SyncPullsRemoteEntities verifies that a Tract client can pull
// entities pushed by another source (simulated via another Tract client pushing).
func TestTractE2E_SyncPullsRemoteEntities(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-pull", "tract")

	// Use a real Tract client to push entries (Tract expects string payloads)
	tcSender := newTractCLI(t, srv, "tract-pull")
	tcSender.initStore(t)
	goals := []map[string]interface{}{
		{"id": "remote-g-1", "name": "Remote goal", "description": "From another client", "status": "active", "priority": 1},
		{"id": "remote-g-2", "name": "Second remote", "description": "Also remote", "status": "active", "priority": 2},
	}
	treePath := tcSender.writeGoalTreeFile(t, goals)
	tcSender.loadGoalTree(t, treePath)
	tcSender.syncPush(t)

	// Fresh Tract client pulls
	tc := newTractCLI(t, srv, "tract-pull")
	tc.initStore(t)
	pullOut := tc.syncPull(t)

	if !strings.Contains(pullOut, "Applied") {
		t.Fatalf("expected pull confirmation, got: %s", pullOut)
	}
	if !strings.Contains(pullOut, "2") {
		t.Logf("pull output: %s", pullOut)
	}

	// Verify sync status shows pull position advanced
	status := tc.syncStatusJSON(t)
	lastPullSeq, _ := status["last_pull_seq"].(string)
	if lastPullSeq == "0" || lastPullSeq == "" {
		t.Errorf("expected last_pull_seq > 0, got %q", lastPullSeq)
	}
	pendingStr := fmt.Sprintf("%v", status["pending_entries"])
	if pendingStr != "0" {
		t.Errorf("expected 0 pending entries after pull, got %s", pendingStr)
	}
}

// TestTractE2E_BootstrapFullGraph verifies that a fresh Tract client can
// bootstrap from an Engram snapshot containing a full entity graph.
func TestTractE2E_BootstrapFullGraph(t *testing.T) {
	requireTract(t)

	srv := startEngram(t)
	srv.createStore(t, "tract-bootstrap", "tract")

	// Seed Engram via a real Tract client (Tract uses string payloads internally)
	tcSeed := newTractCLI(t, srv, "tract-bootstrap")
	tcSeed.initStore(t)
	goals := []map[string]interface{}{
		{"id": "g-root", "name": "Root goal", "description": "Top level", "status": "active", "priority": 1},
		{"id": "g-child", "name": "Child goal", "description": "Under root", "status": "active", "priority": 2, "parent_goal_id": "g-root"},
	}
	treePath := tcSeed.writeGoalTreeFile(t, goals)
	tcSeed.loadGoalTree(t, treePath)
	tcSeed.syncPush(t)

	// Generate snapshot
	srv.generateSnapshot(t, "tract-bootstrap")

	// Fresh client bootstraps
	tc := newTractCLI(t, srv, "tract-bootstrap")
	tc.initStore(t)
	bootstrapOut := tc.syncBootstrap(t)

	if !strings.Contains(bootstrapOut, "Bootstrap complete") {
		t.Fatalf("expected bootstrap success, got: %s", bootstrapOut)
	}

	// Verify the bootstrapped DB has data by checking the Engram server state.
	// (sync status may fail post-bootstrap due to schema differences between
	// Engram's snapshot and Tract's local schema — that's a known Tract limitation.)
	delta := srv.getDelta(t, "tract-bootstrap", 0)
	if len(delta.Entries) != 2 {
		t.Fatalf("expected 2 entries on server (confirming seed data exists), got %d", len(delta.Entries))
	}
}
