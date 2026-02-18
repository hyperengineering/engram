package recall

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// Compile-time check: Plugin must implement DomainPlugin.
var _ plugin.DomainPlugin = (*Plugin)(nil)

// --- Mock ReplayStore ---

type mockReplayStore struct {
	upsertCalls    []upsertCall
	deleteCalls    []deleteCall
	embeddingCalls []string
	upsertErr      error
	deleteErr      error
	embeddingErr   error
}

type upsertCall struct {
	tableName string
	entityID  string
	payload   []byte
}

type deleteCall struct {
	tableName string
	entityID  string
}

func (m *mockReplayStore) UpsertRow(_ context.Context, tableName, entityID string, payload []byte) error {
	m.upsertCalls = append(m.upsertCalls, upsertCall{tableName, entityID, payload})
	return m.upsertErr
}

func (m *mockReplayStore) DeleteRow(_ context.Context, tableName, entityID string) error {
	m.deleteCalls = append(m.deleteCalls, deleteCall{tableName, entityID})
	return m.deleteErr
}

func (m *mockReplayStore) QueueEmbedding(_ context.Context, entryID string) error {
	m.embeddingCalls = append(m.embeddingCalls, entryID)
	return m.embeddingErr
}

// --- Helper ---

func validLorePayload(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(`{
		"id": "entry-1",
		"content": "Test lore content",
		"category": "TESTING_STRATEGY",
		"confidence": 0.5,
		"source_id": "src-1",
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z"
	}`)
}

func payloadWithOverrides(overrides map[string]interface{}) json.RawMessage {
	base := map[string]interface{}{
		"id":         "entry-1",
		"content":    "Test content",
		"category":   "TESTING_STRATEGY",
		"confidence": 0.5,
		"source_id":  "src-1",
		"created_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
	}
	for k, v := range overrides {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	b, _ := json.Marshal(base)
	return b
}

// --- Type tests ---

func TestRecallPlugin_Type(t *testing.T) {
	p := New()
	if got := p.Type(); got != "recall" {
		t.Errorf("Type() = %q, want %q", got, "recall")
	}
}

func TestRecallPlugin_Migrations(t *testing.T) {
	p := New()
	migs := p.Migrations()
	if migs != nil {
		t.Errorf("Migrations() = %v, want nil", migs)
	}
}

// --- ValidatePush tests ---

func TestValidatePush_ValidEntry(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
	}

	got, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ValidatePush() returned %d entries, want 1", len(got))
	}
	if got[0].Sequence != 1 {
		t.Errorf("entry.Sequence = %d, want 1", got[0].Sequence)
	}
}

func TestValidatePush_EmptySlice(t *testing.T) {
	p := New()
	got, err := p.ValidatePush(context.Background(), []engramsync.ChangeLogEntry{})
	if err != nil {
		t.Fatalf("ValidatePush([]) error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ValidatePush([]) returned %d entries, want 0", len(got))
	}
}

func TestValidatePush_NilSlice(t *testing.T) {
	p := New()
	got, err := p.ValidatePush(context.Background(), nil)
	if err != nil {
		t.Fatalf("ValidatePush(nil) error = %v", err)
	}
	if got != nil {
		t.Errorf("ValidatePush(nil) = %v, want nil", got)
	}
}

func TestValidatePush_UnknownTable(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "unknown_table",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("ValidatePush() expected error for unknown table")
	}

	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	if len(ve.Errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d", len(ve.Errors))
	}
	if ve.Errors[0].TableName != "unknown_table" {
		t.Errorf("error.TableName = %q, want %q", ve.Errors[0].TableName, "unknown_table")
	}
}

func TestValidatePush_MissingID(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"id": ""}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "missing required field: id")
}

func TestValidatePush_MissingContent(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"content": ""}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "missing required field: content")
}

func TestValidatePush_MissingCategory(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"category": ""}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing category")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "missing required field: category")
}

func TestValidatePush_MissingSourceID(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"source_id": ""}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for missing source_id")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "missing required field: source_id")
}

func TestValidatePush_InvalidCategory(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"category": "UNKNOWN"}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for invalid category")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "invalid category: UNKNOWN")
}

func TestValidatePush_InvalidConfidence_TooHigh(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"confidence": 1.5}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for confidence > 1")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "confidence must be between 0 and 1")
}

func TestValidatePush_InvalidConfidence_Negative(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"confidence": -0.1}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for negative confidence")
	}
}

func TestValidatePush_NullPayload(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   nil,
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for nil payload on upsert")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "payload required for upsert")
}

func TestValidatePush_InvalidPayloadJSON(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`not valid json`),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	assertContainsMessage(t, ve.Errors, "invalid payload JSON")
}

func TestValidatePush_DeleteNoPayload(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationDelete,
			// No payload — delete doesn't need one
		},
	}

	got, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v (delete should not require payload)", err)
	}
	if len(got) != 1 {
		t.Fatalf("ValidatePush() returned %d entries, want 1", len(got))
	}
}

func TestValidatePush_MultipleErrors(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "bad_table",
			EntityID:  "e1",
			Operation: engramsync.OperationUpsert,
		},
		{
			Sequence:  2,
			TableName: "lore_entries",
			EntityID:  "e2",
			Operation: engramsync.OperationUpsert,
			Payload:   nil,
		},
		{
			Sequence:  3,
			TableName: "lore_entries",
			EntityID:  "e3",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"category": "INVALID"}),
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error for multiple invalid entries")
	}
	var ve plugin.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(ve.Errors) != 3 {
		t.Errorf("expected 3 validation errors, got %d", len(ve.Errors))
	}
}

func TestValidatePush_ConfidenceBoundaryValues(t *testing.T) {
	p := New()

	// confidence = 0 should be valid
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "e1",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"confidence": 0.0}),
		},
	}
	got, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("confidence=0 should be valid, got error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}

	// confidence = 1 should be valid
	entries[0].Payload = payloadWithOverrides(map[string]interface{}{"confidence": 1.0})
	got, err = p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("confidence=1 should be valid, got error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestValidatePush_AllValidCategories(t *testing.T) {
	p := New()

	for category := range ValidCategories {
		entries := []engramsync.ChangeLogEntry{
			{
				Sequence:  1,
				TableName: "lore_entries",
				EntityID:  "e1",
				Operation: engramsync.OperationUpsert,
				Payload:   payloadWithOverrides(map[string]interface{}{"category": category}),
			},
		}

		_, err := p.ValidatePush(context.Background(), entries)
		if err != nil {
			t.Errorf("category %q should be valid, got error: %v", category, err)
		}
	}
}

func TestValidatePush_ErrorsIsCompatible(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "bad_table",
			EntityID:  "e1",
			Operation: engramsync.OperationUpsert,
		},
	}

	_, err := p.ValidatePush(context.Background(), entries)
	if !errors.Is(err, plugin.ErrValidationFailed) {
		t.Errorf("expected errors.Is(err, ErrValidationFailed) to be true")
	}
}

func TestValidatePush_PayloadWithEmbeddingFloatArray(t *testing.T) {
	p := New()
	payload := payloadWithOverrides(map[string]interface{}{
		"embedding": []float64{0.1, 0.2, 0.3},
	})
	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payload,
		},
	}

	got, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v; payload with embedding float array should be valid", err)
	}
	if len(got) != 1 {
		t.Fatalf("ValidatePush() returned %d entries, want 1", len(got))
	}
}

// --- OnReplay tests ---

func TestOnReplay_Upsert(t *testing.T) {
	p := New()
	store := &mockReplayStore{}
	payload := validLorePayload(t)

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   payload,
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 1 {
		t.Fatalf("expected 1 upsert call, got %d", len(store.upsertCalls))
	}
	call := store.upsertCalls[0]
	if call.tableName != "lore_entries" {
		t.Errorf("upsert tableName = %q, want %q", call.tableName, "lore_entries")
	}
	if call.entityID != "entry-1" {
		t.Errorf("upsert entityID = %q, want %q", call.entityID, "entry-1")
	}
}

func TestOnReplay_Delete(t *testing.T) {
	p := New()
	store := &mockReplayStore{}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationDelete,
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(store.deleteCalls))
	}
	call := store.deleteCalls[0]
	if call.tableName != "lore_entries" {
		t.Errorf("delete tableName = %q, want %q", call.tableName, "lore_entries")
	}
	if call.entityID != "entry-1" {
		t.Errorf("delete entityID = %q, want %q", call.entityID, "entry-1")
	}
}

func TestOnReplay_UpsertQueuesEmbedding(t *testing.T) {
	p := New()
	store := &mockReplayStore{}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.embeddingCalls) != 1 {
		t.Fatalf("expected 1 embedding call, got %d", len(store.embeddingCalls))
	}
	if store.embeddingCalls[0] != "entry-1" {
		t.Errorf("embedding entryID = %q, want %q", store.embeddingCalls[0], "entry-1")
	}
}

func TestOnReplay_UpsertError(t *testing.T) {
	p := New()
	store := &mockReplayStore{
		upsertErr: errors.New("upsert failed"),
	}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err == nil {
		t.Fatal("expected error from UpsertRow failure")
	}
}

func TestOnReplay_DeleteError(t *testing.T) {
	p := New()
	store := &mockReplayStore{
		deleteErr: errors.New("delete failed"),
	}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationDelete,
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err == nil {
		t.Fatal("expected error from DeleteRow failure")
	}
}

func TestOnReplay_EmbeddingErrorNonFatal(t *testing.T) {
	p := New()
	store := &mockReplayStore{
		embeddingErr: errors.New("embedding queue failed"),
	}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "entry-1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
	}

	// Should NOT return error even if QueueEmbedding fails
	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() should not fail on QueueEmbedding error, got: %v", err)
	}

	// UpsertRow should still have been called
	if len(store.upsertCalls) != 1 {
		t.Fatalf("expected 1 upsert call, got %d", len(store.upsertCalls))
	}
}

func TestOnReplay_SkipsUnknownTable(t *testing.T) {
	p := New()
	store := &mockReplayStore{}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "other_table",
			EntityID:  "e1",
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{}`),
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("expected 0 upsert calls for unknown table, got %d", len(store.upsertCalls))
	}
}

func TestOnReplay_EmptyEntries(t *testing.T) {
	p := New()
	store := &mockReplayStore{}

	err := p.OnReplay(context.Background(), store, []engramsync.ChangeLogEntry{})
	if err != nil {
		t.Fatalf("OnReplay([]) error = %v", err)
	}

	err = p.OnReplay(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("OnReplay(nil) error = %v", err)
	}
}

func TestOnReplay_MixedOperations(t *testing.T) {
	p := New()
	store := &mockReplayStore{}

	entries := []engramsync.ChangeLogEntry{
		{
			Sequence:  1,
			TableName: "lore_entries",
			EntityID:  "e1",
			Operation: engramsync.OperationUpsert,
			Payload:   validLorePayload(t),
		},
		{
			Sequence:  2,
			TableName: "lore_entries",
			EntityID:  "e2",
			Operation: engramsync.OperationDelete,
		},
		{
			Sequence:  3,
			TableName: "lore_entries",
			EntityID:  "e3",
			Operation: engramsync.OperationUpsert,
			Payload:   payloadWithOverrides(map[string]interface{}{"id": "e3"}),
		},
	}

	err := p.OnReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 2 {
		t.Errorf("expected 2 upsert calls, got %d", len(store.upsertCalls))
	}
	if len(store.deleteCalls) != 1 {
		t.Errorf("expected 1 delete call, got %d", len(store.deleteCalls))
	}
	if len(store.embeddingCalls) != 2 {
		t.Errorf("expected 2 embedding calls, got %d", len(store.embeddingCalls))
	}
}

// --- Helper assertions ---

func assertContainsMessage(t *testing.T, errs []plugin.ValidationError, substr string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Message, substr) {
			return
		}
	}
	t.Errorf("expected validation error containing %q, got: %v", substr, errs)
}
