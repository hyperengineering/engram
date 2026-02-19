package tract

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hyperengineering/engram/internal/sync"
)

// mockReplayStore tracks calls to UpsertRow, DeleteRow, QueueEmbedding.
type mockReplayStore struct {
	upsertCalls     []upsertCall
	deleteCalls     []deleteCall
	embeddingCalls  []string
	upsertErr       error
	deleteErr       error
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
	return nil
}

// --- Seed 6.1: Upsert dispatch ---

func TestOnReplay_UpsertGoal(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1", "title": "Goal"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 1 {
		t.Fatalf("upsertCalls = %d, want 1", len(store.upsertCalls))
	}
	if store.upsertCalls[0].tableName != "goals" {
		t.Errorf("tableName = %q, want %q", store.upsertCalls[0].tableName, "goals")
	}
	if store.upsertCalls[0].entityID != "g1" {
		t.Errorf("entityID = %q, want %q", store.upsertCalls[0].entityID, "g1")
	}
}

func TestOnReplay_UpsertCSF(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{"id": "c1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.upsertCalls) != 1 || store.upsertCalls[0].tableName != "csfs" {
		t.Error("expected upsert call for csfs")
	}
}

func TestOnReplay_UpsertFWU(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{"id": "f1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.upsertCalls) != 1 || store.upsertCalls[0].tableName != "fwus" {
		t.Error("expected upsert call for fwus")
	}
}

func TestOnReplay_UpsertIC(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("implementation_contexts", "ic1", sync.OperationUpsert, map[string]interface{}{"id": "ic1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.upsertCalls) != 1 || store.upsertCalls[0].tableName != "implementation_contexts" {
		t.Error("expected upsert call for implementation_contexts")
	}
}

// --- Seed 6.2: Delete dispatch and edge cases ---

func TestOnReplay_DeleteGoal(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		{TableName: "goals", EntityID: "g1", Operation: sync.OperationDelete},
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.deleteCalls) != 1 {
		t.Fatalf("deleteCalls = %d, want 1", len(store.deleteCalls))
	}
	if store.deleteCalls[0].tableName != "goals" {
		t.Errorf("tableName = %q, want %q", store.deleteCalls[0].tableName, "goals")
	}
}

func TestOnReplay_NoEmbeddingQueued(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1"}),
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{"id": "c1"}),
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{"id": "f1"}),
		makeEntry("implementation_contexts", "ic1", sync.OperationUpsert, map[string]interface{}{"id": "ic1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.embeddingCalls) != 0 {
		t.Errorf("embeddingCalls = %d, want 0 (Tract should not queue embeddings)", len(store.embeddingCalls))
	}
}

func TestOnReplay_UpsertError(t *testing.T) {
	store := &mockReplayStore{upsertErr: fmt.Errorf("db error")}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err == nil {
		t.Fatal("expected error from UpsertRow")
	}
}

func TestOnReplay_DeleteError(t *testing.T) {
	store := &mockReplayStore{deleteErr: fmt.Errorf("db error")}
	entries := []sync.ChangeLogEntry{
		{TableName: "goals", EntityID: "g1", Operation: sync.OperationDelete},
	}

	err := onReplay(context.Background(), store, entries)
	if err == nil {
		t.Fatal("expected error from DeleteRow")
	}
}

func TestOnReplay_SkipsUnknownTable(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		{TableName: "unknown", EntityID: "x1", Operation: sync.OperationUpsert, Payload: json.RawMessage(`{"id":"x1"}`)},
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}
	if len(store.upsertCalls) != 0 {
		t.Errorf("upsertCalls = %d, want 0 for unknown table", len(store.upsertCalls))
	}
}

func TestOnReplay_EmptyEntries(t *testing.T) {
	store := &mockReplayStore{}

	err := onReplay(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("OnReplay(nil) error = %v", err)
	}

	err = onReplay(context.Background(), store, []sync.ChangeLogEntry{})
	if err != nil {
		t.Fatalf("OnReplay([]) error = %v", err)
	}
}

func TestOnReplay_MixedOperations(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1"}),
		{TableName: "csfs", EntityID: "c1", Operation: sync.OperationDelete},
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{"id": "f1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 2 {
		t.Errorf("upsertCalls = %d, want 2", len(store.upsertCalls))
	}
	if len(store.deleteCalls) != 1 {
		t.Errorf("deleteCalls = %d, want 1", len(store.deleteCalls))
	}
}
