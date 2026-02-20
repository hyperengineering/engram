package tract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperengineering/engram/internal/sync"
)

// mockReplayStore tracks calls to UpsertRow, DeleteRow, QueueEmbedding.
type mockReplayStore struct {
	upsertCalls    []upsertCall
	deleteCalls    []deleteCall
	embeddingCalls []string
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
	return nil
}

func (m *mockReplayStore) DeleteRow(_ context.Context, tableName, entityID string) error {
	m.deleteCalls = append(m.deleteCalls, deleteCall{tableName, entityID})
	return nil
}

func (m *mockReplayStore) QueueEmbedding(_ context.Context, entryID string) error {
	m.embeddingCalls = append(m.embeddingCalls, entryID)
	return nil
}

// onReplay is a no-op — all tests verify that no store calls are made.

func TestOnReplay_NoOp_Upserts(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1", "title": "Goal"}),
		makeEntry("csfs", "c1", sync.OperationUpsert, map[string]interface{}{"id": "c1"}),
		makeEntry("fwus", "f1", sync.OperationUpsert, map[string]interface{}{"id": "f1"}),
		makeEntry("implementation_contexts", "ic1", sync.OperationUpsert, map[string]interface{}{"id": "ic1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 0 {
		t.Errorf("upsertCalls = %d, want 0 (onReplay is a no-op)", len(store.upsertCalls))
	}
}

func TestOnReplay_NoOp_Deletes(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		{TableName: "goals", EntityID: "g1", Operation: sync.OperationDelete},
		{TableName: "csfs", EntityID: "c1", Operation: sync.OperationDelete},
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.deleteCalls) != 0 {
		t.Errorf("deleteCalls = %d, want 0 (onReplay is a no-op)", len(store.deleteCalls))
	}
}

func TestOnReplay_NoOp_NoEmbeddings(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		makeEntry("goals", "g1", sync.OperationUpsert, map[string]interface{}{"id": "g1"}),
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.embeddingCalls) != 0 {
		t.Errorf("embeddingCalls = %d, want 0 (onReplay is a no-op)", len(store.embeddingCalls))
	}
}

func TestOnReplay_NoOp_EmptyEntries(t *testing.T) {
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

func TestOnReplay_NoOp_UnknownTables(t *testing.T) {
	store := &mockReplayStore{}
	entries := []sync.ChangeLogEntry{
		{TableName: "sos", EntityID: "SO-1", Operation: sync.OperationUpsert, Payload: json.RawMessage(`{"id":"SO-1"}`)},
		{TableName: "capabilities", EntityID: "CAP-01", Operation: sync.OperationUpsert, Payload: json.RawMessage(`{"id":"CAP-01"}`)},
	}

	err := onReplay(context.Background(), store, entries)
	if err != nil {
		t.Fatalf("OnReplay() error = %v", err)
	}

	if len(store.upsertCalls) != 0 {
		t.Errorf("upsertCalls = %d, want 0 (onReplay is a no-op)", len(store.upsertCalls))
	}
}

func TestOnReplay_NoOp_MixedOperations(t *testing.T) {
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

	if len(store.upsertCalls) != 0 {
		t.Errorf("upsertCalls = %d, want 0 (onReplay is a no-op)", len(store.upsertCalls))
	}
	if len(store.deleteCalls) != 0 {
		t.Errorf("deleteCalls = %d, want 0 (onReplay is a no-op)", len(store.deleteCalls))
	}
}
