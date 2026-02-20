package tract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

func makeEntry(table, entityID, operation string, payload interface{}) sync.ChangeLogEntry {
	var raw json.RawMessage
	if payload != nil {
		b, _ := json.Marshal(payload)
		raw = b
	}
	return sync.ChangeLogEntry{
		Sequence:  1,
		TableName: table,
		EntityID:  entityID,
		Operation: operation,
		Payload:   raw,
	}
}

// --- Table name validation ---

func TestValidatePush_AcceptsAnyValidTableName(t *testing.T) {
	p := New()
	// These are all valid table names that should be accepted
	tables := []string{
		"goals", "csfs", "fwus", "implementation_contexts",
		"sos", "ncs", "so_ncs", "capabilities", "epics", "features",
		"fwu_boundaries", "fwu_dependencies", "fwu_design_decisions",
		"fwu_interface_contracts", "fwu_verification_gates",
		"design_decisions", "entity_specs", "test_seeds",
		"file_actions", "followups", "some_future_table",
	}

	for _, table := range tables {
		entries := []sync.ChangeLogEntry{
			makeEntry(table, "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
		}

		_, err := p.ValidatePush(context.Background(), entries)
		if err != nil {
			t.Errorf("ValidatePush() rejected valid table %q: %v", table, err)
		}
	}
}

func TestValidatePush_RejectsInvalidTableName(t *testing.T) {
	p := New()
	invalidNames := []string{
		"BAD_TABLE",       // uppercase
		"1bad",            // starts with digit
		"drop;table",      // SQL injection
		"table name",      // space
		"table-name",      // hyphen
		"",                // empty
		"Table",           // mixed case
		"_leading_under",  // starts with underscore
	}

	for _, name := range invalidNames {
		entries := []sync.ChangeLogEntry{
			makeEntry(name, "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
		}

		_, err := p.ValidatePush(context.Background(), entries)
		if err == nil {
			t.Errorf("ValidatePush() accepted invalid table name %q", name)
		}

		var ve plugin.ValidationErrors
		if !errors.As(err, &ve) {
			t.Errorf("error type = %T, want plugin.ValidationErrors for table %q", err, name)
		}
	}
}

// --- Payload validation ---

func TestValidatePush_ValidGoal(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{
			"id": "g1", "title": "Goal 1", "status": "active",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_AcceptsGoalWithoutTitleOrStatus(t *testing.T) {
	p := New()
	// Tract CLI may send goals with different field names
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{
			"id": "g1", "name": "Goal 1",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_AcceptsFWUWithoutCSFID(t *testing.T) {
	p := New()
	// Tract CLI may use feature_id instead of csf_id
	entries := []sync.ChangeLogEntry{
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{
			"id": "f1", "feature_id": "feat1", "intent": "Do something",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_ValidCSF(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{
			"id": "c1", "goal_id": "g1", "title": "CSF 1", "status": "tracking",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_ValidFWU(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{
			"id": "f1", "csf_id": "c1", "title": "FWU 1", "status": "planned",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_ValidIC(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("implementation_contexts", "ic1", sync.OperationUpsert, map[string]interface{}{
			"id": "ic1", "fwu_id": "f1",
		}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestValidatePush_EmptySlice(t *testing.T) {
	p := New()
	result, err := p.ValidatePush(context.Background(), []sync.ChangeLogEntry{})
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestValidatePush_NilSlice(t *testing.T) {
	p := New()
	result, err := p.ValidatePush(context.Background(), nil)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if result != nil {
		t.Fatalf("result = %v, want nil", result)
	}
}

func TestValidatePush_NilPayloadOnUpsert(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "goals",
			EntityID:  "g1",
			Operation: sync.OperationUpsert,
			Payload:   nil,
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for nil payload on upsert")
	}
}

func TestValidatePush_InvalidJSON(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "goals",
			EntityID:  "g1",
			Operation: sync.OperationUpsert,
			Payload:   json.RawMessage(`not json`),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidatePush_DeleteNoPayload(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "goals",
			EntityID:  "g1",
			Operation: sync.OperationDelete,
			Payload:   nil,
		},
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

// --- Multiple errors ---

func TestValidatePush_MultipleErrors(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("BAD_TABLE", "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
		{Sequence: 2, TableName: "goals", EntityID: "g1", Operation: sync.OperationUpsert, Payload: nil},
		{Sequence: 3, TableName: "1invalid", EntityID: "c1", Operation: sync.OperationUpsert, Payload: json.RawMessage(`{"id":"c1"}`)},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected errors")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	// 3 errors: invalid table name, nil payload, invalid table name
	if len(ve.Errors) != 3 {
		t.Errorf("error count = %d, want 3", len(ve.Errors))
	}
}

func TestValidatePush_ErrorsIsCompatible(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("BAD_TABLE", "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, plugin.ErrValidationFailed) {
		t.Error("errors.Is(err, ErrValidationFailed) = false, want true")
	}
}

// --- Tract-specific table acceptance ---

func TestValidatePush_AcceptsTractTables(t *testing.T) {
	p := New()
	// All tables from the Tract CLI that should be accepted
	entries := []sync.ChangeLogEntry{
		makeEntry("sos", "SO-1", sync.OperationUpsert, map[string]interface{}{"id": "SO-1"}),
		makeEntry("ncs", "NC-1", sync.OperationUpsert, map[string]interface{}{"id": "NC-1"}),
		makeEntry("so_ncs", "SO-1:NC-1", sync.OperationUpsert, map[string]interface{}{"id": "SO-1:NC-1"}),
		makeEntry("capabilities", "CAP-01", sync.OperationUpsert, map[string]interface{}{"id": "CAP-01"}),
		makeEntry("epics", "E-01", sync.OperationUpsert, map[string]interface{}{"id": "E-01"}),
		makeEntry("features", "F-01.1", sync.OperationUpsert, map[string]interface{}{"id": "F-01.1"}),
		makeEntry("fwu_boundaries", "FWU-01:BN:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:BN:1"}),
		makeEntry("fwu_dependencies", "FWU-01:DP:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:DP:1"}),
		makeEntry("fwu_design_decisions", "FWU-01:PD:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:PD:1"}),
		makeEntry("fwu_interface_contracts", "FWU-01:CT:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:CT:1"}),
		makeEntry("fwu_verification_gates", "FWU-01:VG:tests", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:VG:tests"}),
		makeEntry("design_decisions", "FWU-01:DD:DD-001", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:DD:DD-001"}),
		makeEntry("entity_specs", "FWU-01:ES:Session", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:ES:Session"}),
		makeEntry("test_seeds", "FWU-01:TS:TS-001", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:TS:TS-001"}),
		makeEntry("file_actions", "FWU-01:FA:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:FA:1"}),
		makeEntry("followups", "FWU-01:FU:1", sync.OperationUpsert, map[string]interface{}{"id": "FWU-01:FU:1"}),
	}

	result, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(result) != len(entries) {
		t.Errorf("result length = %d, want %d", len(result), len(entries))
	}
}
