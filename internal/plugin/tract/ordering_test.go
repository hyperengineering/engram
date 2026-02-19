package tract

import (
	"encoding/json"
	"testing"

	"github.com/hyperengineering/engram/internal/sync"
)

func upsertEntry(table, entityID string, payload interface{}) sync.ChangeLogEntry {
	var raw json.RawMessage
	if payload != nil {
		b, _ := json.Marshal(payload)
		raw = b
	}
	return sync.ChangeLogEntry{
		TableName: table,
		EntityID:  entityID,
		Operation: sync.OperationUpsert,
		Payload:   raw,
	}
}

func deleteEntry(table, entityID string) sync.ChangeLogEntry {
	return sync.ChangeLogEntry{
		TableName: table,
		EntityID:  entityID,
		Operation: sync.OperationDelete,
	}
}

// --- Seed 5.1: Cross-table ordering for upserts ---

func TestReorder_ParentBeforeChild_CrossTable(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("fwus", "f1", map[string]interface{}{"id": "f1", "csf_id": "c1", "title": "FWU", "status": "planned"}),
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Goal", "status": "active"}),
		upsertEntry("csfs", "c1", map[string]interface{}{"id": "c1", "goal_id": "g1", "title": "CSF", "status": "tracking"}),
		upsertEntry("implementation_contexts", "ic1", map[string]interface{}{"id": "ic1", "fwu_id": "f1"}),
	}

	result := reorderForFK(entries)

	// Expected order: goals, csfs, fwus, implementation_contexts
	expectedTables := []string{"goals", "csfs", "fwus", "implementation_contexts"}
	if len(result) != 4 {
		t.Fatalf("result length = %d, want 4", len(result))
	}
	for i, expected := range expectedTables {
		if result[i].TableName != expected {
			t.Errorf("result[%d].TableName = %q, want %q", i, result[i].TableName, expected)
		}
	}
}

func TestReorder_UpsertsParentFirst(t *testing.T) {
	// Insert in completely reversed order
	entries := []sync.ChangeLogEntry{
		upsertEntry("implementation_contexts", "ic1", map[string]interface{}{"id": "ic1", "fwu_id": "f1"}),
		upsertEntry("fwus", "f1", map[string]interface{}{"id": "f1", "csf_id": "c1", "title": "FWU", "status": "planned"}),
		upsertEntry("csfs", "c1", map[string]interface{}{"id": "c1", "goal_id": "g1", "title": "CSF", "status": "tracking"}),
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Goal", "status": "active"}),
	}

	result := reorderForFK(entries)

	expectedOrder := []string{"goals", "csfs", "fwus", "implementation_contexts"}
	for i, expected := range expectedOrder {
		if result[i].TableName != expected {
			t.Errorf("result[%d].TableName = %q, want %q", i, result[i].TableName, expected)
		}
	}
}

// --- Seed 5.2: Delete ordering ---

func TestReorder_DeletesChildFirst(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		deleteEntry("goals", "g1"),
		deleteEntry("csfs", "c1"),
		deleteEntry("fwus", "f1"),
		deleteEntry("implementation_contexts", "ic1"),
	}

	result := reorderForFK(entries)

	// Deletes: children first (ic, fwu, csf, goal)
	expectedOrder := []string{"implementation_contexts", "fwus", "csfs", "goals"}
	for i, expected := range expectedOrder {
		if result[i].TableName != expected {
			t.Errorf("result[%d].TableName = %q, want %q", i, result[i].TableName, expected)
		}
	}
}

func TestReorder_AllDeletes(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		deleteEntry("goals", "g1"),
		deleteEntry("implementation_contexts", "ic1"),
		deleteEntry("csfs", "c1"),
		deleteEntry("fwus", "f1"),
	}

	result := reorderForFK(entries)

	// All deletes ordered child-first
	if result[0].TableName != "implementation_contexts" {
		t.Errorf("first delete should be implementation_contexts, got %q", result[0].TableName)
	}
	if result[len(result)-1].TableName != "goals" {
		t.Errorf("last delete should be goals, got %q", result[len(result)-1].TableName)
	}
}

// --- Seed 5.3: Mixed operations ---

func TestReorder_DeletesBeforeUpserts(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g2", map[string]interface{}{"id": "g2", "title": "New", "status": "active"}),
		deleteEntry("goals", "g1"),
	}

	result := reorderForFK(entries)

	// Delete should come first
	if result[0].Operation != sync.OperationDelete {
		t.Errorf("result[0].Operation = %q, want delete", result[0].Operation)
	}
	if result[1].Operation != sync.OperationUpsert {
		t.Errorf("result[1].Operation = %q, want upsert", result[1].Operation)
	}
}

func TestReorder_MixedDeletesAndUpserts(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g2", map[string]interface{}{"id": "g2", "title": "New", "status": "active"}),
		deleteEntry("csfs", "c1"),
		upsertEntry("csfs", "c2", map[string]interface{}{"id": "c2", "goal_id": "g2", "title": "CSF", "status": "tracking"}),
		deleteEntry("goals", "g1"),
	}

	result := reorderForFK(entries)

	// First: deletes (child-first: csfs before goals)
	if result[0].Operation != sync.OperationDelete {
		t.Errorf("result[0] should be delete, got %q", result[0].Operation)
	}
	if result[0].TableName != "csfs" {
		t.Errorf("first delete should be csfs, got %q", result[0].TableName)
	}
	if result[1].Operation != sync.OperationDelete {
		t.Errorf("result[1] should be delete, got %q", result[1].Operation)
	}
	if result[1].TableName != "goals" {
		t.Errorf("second delete should be goals, got %q", result[1].TableName)
	}

	// Then: upserts (parent-first: goals before csfs)
	if result[2].Operation != sync.OperationUpsert {
		t.Errorf("result[2] should be upsert, got %q", result[2].Operation)
	}
	if result[2].TableName != "goals" {
		t.Errorf("first upsert should be goals, got %q", result[2].TableName)
	}
	if result[3].Operation != sync.OperationUpsert {
		t.Errorf("result[3] should be upsert, got %q", result[3].Operation)
	}
	if result[3].TableName != "csfs" {
		t.Errorf("second upsert should be csfs, got %q", result[3].TableName)
	}
}

// --- Seed 5.4: Intra-goal parentage sorting ---

func TestReorder_GoalParentageSort(t *testing.T) {
	parentID := "g1"
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g2", map[string]interface{}{"id": "g2", "title": "Child", "status": "active", "parent_goal_id": &parentID}),
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Parent", "status": "active", "parent_goal_id": nil}),
	}

	result := reorderForFK(entries)

	if result[0].EntityID != "g1" {
		t.Errorf("first goal should be parent g1, got %q", result[0].EntityID)
	}
	if result[1].EntityID != "g2" {
		t.Errorf("second goal should be child g2, got %q", result[1].EntityID)
	}
}

func TestReorder_GoalTreeThreeLevels(t *testing.T) {
	mid := "g1"
	leaf := "g2"
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g3", map[string]interface{}{"id": "g3", "title": "Leaf", "status": "active", "parent_goal_id": &leaf}),
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Root", "status": "active", "parent_goal_id": nil}),
		upsertEntry("goals", "g2", map[string]interface{}{"id": "g2", "title": "Mid", "status": "active", "parent_goal_id": &mid}),
	}

	result := reorderForFK(entries)

	// Expected order: g1 (root), g2 (mid), g3 (leaf)
	if result[0].EntityID != "g1" {
		t.Errorf("result[0] = %q, want g1 (root)", result[0].EntityID)
	}
	if result[1].EntityID != "g2" {
		t.Errorf("result[1] = %q, want g2 (mid)", result[1].EntityID)
	}
	if result[2].EntityID != "g3" {
		t.Errorf("result[2] = %q, want g3 (leaf)", result[2].EntityID)
	}
}

func TestReorder_GoalOrphanParent(t *testing.T) {
	orphanParent := "g-external"
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Orphan", "status": "active", "parent_goal_id": &orphanParent}),
	}

	result := reorderForFK(entries)

	// Goal with parent not in batch should be treated as root
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
	if result[0].EntityID != "g1" {
		t.Errorf("result[0] = %q, want g1", result[0].EntityID)
	}
}

func TestReorder_NullParentGoalID(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Root", "status": "active", "parent_goal_id": nil}),
	}

	result := reorderForFK(entries)
	if len(result) != 1 || result[0].EntityID != "g1" {
		t.Errorf("null parent_goal_id should be treated as root")
	}
}

func TestReorder_MissingParentGoalIDField(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Root", "status": "active"}),
	}

	result := reorderForFK(entries)
	if len(result) != 1 || result[0].EntityID != "g1" {
		t.Errorf("missing parent_goal_id field should be treated as root")
	}
}

// --- Seed 5.5: Stability and edge cases ---

func TestReorder_StableWithinSameLevel(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "First", "status": "active"}),
		upsertEntry("goals", "g2", map[string]interface{}{"id": "g2", "title": "Second", "status": "active"}),
		upsertEntry("goals", "g3", map[string]interface{}{"id": "g3", "title": "Third", "status": "active"}),
	}

	result := reorderForFK(entries)

	// All roots, should preserve original order
	if result[0].EntityID != "g1" {
		t.Errorf("result[0] = %q, want g1", result[0].EntityID)
	}
	if result[1].EntityID != "g2" {
		t.Errorf("result[1] = %q, want g2", result[1].EntityID)
	}
	if result[2].EntityID != "g3" {
		t.Errorf("result[2] = %q, want g3", result[2].EntityID)
	}
}

func TestReorder_EmptySlice(t *testing.T) {
	result := reorderForFK([]sync.ChangeLogEntry{})
	if len(result) != 0 {
		t.Errorf("result length = %d, want 0", len(result))
	}
}

func TestReorder_SingleTable(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("csfs", "c1", map[string]interface{}{"id": "c1", "goal_id": "g1", "title": "CSF1", "status": "tracking"}),
		upsertEntry("csfs", "c2", map[string]interface{}{"id": "c2", "goal_id": "g1", "title": "CSF2", "status": "tracking"}),
	}

	result := reorderForFK(entries)

	// Same table, same level: preserve order
	if result[0].EntityID != "c1" {
		t.Errorf("result[0] = %q, want c1", result[0].EntityID)
	}
	if result[1].EntityID != "c2" {
		t.Errorf("result[1] = %q, want c2", result[1].EntityID)
	}
}

func TestReorder_AllUpserts(t *testing.T) {
	entries := []sync.ChangeLogEntry{
		upsertEntry("implementation_contexts", "ic1", map[string]interface{}{"id": "ic1", "fwu_id": "f1"}),
		upsertEntry("goals", "g1", map[string]interface{}{"id": "g1", "title": "Goal", "status": "active"}),
		upsertEntry("fwus", "f1", map[string]interface{}{"id": "f1", "csf_id": "c1", "title": "FWU", "status": "planned"}),
		upsertEntry("csfs", "c1", map[string]interface{}{"id": "c1", "goal_id": "g1", "title": "CSF", "status": "tracking"}),
	}

	result := reorderForFK(entries)

	expectedOrder := []string{"goals", "csfs", "fwus", "implementation_contexts"}
	for i, expected := range expectedOrder {
		if result[i].TableName != expected {
			t.Errorf("result[%d].TableName = %q, want %q", i, result[i].TableName, expected)
		}
	}
}
