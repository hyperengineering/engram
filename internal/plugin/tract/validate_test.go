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

// --- Seed 4.1: Table allowlist ---

func TestValidatePush_UnknownTable(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("bad_table", "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for unknown table")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}
	if len(ve.Errors) != 1 {
		t.Fatalf("error count = %d, want 1", len(ve.Errors))
	}
	if ve.Errors[0].TableName != "bad_table" {
		t.Errorf("TableName = %q, want %q", ve.Errors[0].TableName, "bad_table")
	}
}

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

// --- Seed 4.2: Required field validation ---

func TestValidatePush_MissingGoalTitle(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{
			"id": "g1", "status": "active",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing title")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	found := false
	for _, e := range ve.Errors {
		if e.Field == "title" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'title'")
	}
}

func TestValidatePush_MissingGoalStatus(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{
			"id": "g1", "title": "Goal",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing status")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	found := false
	for _, e := range ve.Errors {
		if e.Field == "status" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'status'")
	}
}

func TestValidatePush_MissingGoalID(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{
			"title": "Goal", "status": "active",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestValidatePush_MissingCSFGoalID(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{
			"id": "c1", "title": "CSF", "status": "tracking",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing goal_id")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	found := false
	for _, e := range ve.Errors {
		if e.Field == "goal_id" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'goal_id'")
	}
}

func TestValidatePush_MissingFWUCSFID(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{
			"id": "f1", "title": "FWU", "status": "planned",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing csf_id")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	found := false
	for _, e := range ve.Errors {
		if e.Field == "csf_id" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'csf_id'")
	}
}

func TestValidatePush_MissingICFWUID(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("implementation_contexts", "ic1", sync.OperationUpsert, map[string]interface{}{
			"id": "ic1",
		}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing fwu_id")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	found := false
	for _, e := range ve.Errors {
		if e.Field == "fwu_id" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'fwu_id'")
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

// --- Seed 4.3: Multiple errors ---

func TestValidatePush_MultipleErrors(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("bad_table", "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1"}), // missing title + status
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{"id": "c1"}),  // missing goal_id, title, status
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected errors")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error type = %T, want plugin.ValidationErrors", err)
	}

	// At least 3 errors: 1 for bad_table, 2+ for goals (title, status), 3+ for csfs (goal_id, title, status)
	if len(ve.Errors) < 3 {
		t.Errorf("error count = %d, want >= 3", len(ve.Errors))
	}
}

func TestValidatePush_ErrorsIsCompatible(t *testing.T) {
	p := New()
	entries := []sync.ChangeLogEntry{
		makeEntry("bad_table", "e1", sync.OperationUpsert, map[string]interface{}{"id": "e1"}),
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error")
	}

	if !errors.Is(err, plugin.ErrValidationFailed) {
		t.Error("errors.Is(err, ErrValidationFailed) = false, want true")
	}
}
