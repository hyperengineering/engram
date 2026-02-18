package generic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// Compile-time check: Plugin must implement DomainPlugin.
var _ plugin.DomainPlugin = (*Plugin)(nil)

func TestGenericPlugin_Type(t *testing.T) {
	p := New()
	if got := p.Type(); got != "generic" {
		t.Errorf("Type() = %q, want %q", got, "generic")
	}
}

func TestGenericPlugin_Migrations(t *testing.T) {
	p := New()
	migs := p.Migrations()
	if migs != nil {
		t.Errorf("Migrations() = %v, want nil", migs)
	}
}

func TestGenericPlugin_ValidatePush_PassThrough(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{Sequence: 1, TableName: "lore", EntityID: "e1", Operation: engramsync.OperationUpsert, Payload: json.RawMessage(`{"content":"test"}`)},
		{Sequence: 2, TableName: "lore", EntityID: "e2", Operation: engramsync.OperationDelete},
	}

	got, err := p.ValidatePush(context.Background(), entries)
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("ValidatePush() returned %d entries, want %d", len(got), len(entries))
	}
	for i := range entries {
		if got[i].Sequence != entries[i].Sequence {
			t.Errorf("entry[%d].Sequence = %d, want %d", i, got[i].Sequence, entries[i].Sequence)
		}
		if got[i].EntityID != entries[i].EntityID {
			t.Errorf("entry[%d].EntityID = %q, want %q", i, got[i].EntityID, entries[i].EntityID)
		}
	}
}

func TestGenericPlugin_ValidatePush_EmptySlice(t *testing.T) {
	p := New()
	got, err := p.ValidatePush(context.Background(), []engramsync.ChangeLogEntry{})
	if err != nil {
		t.Fatalf("ValidatePush() error = %v", err)
	}
	if got == nil {
		t.Fatal("ValidatePush([]) returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ValidatePush([]) returned %d entries, want 0", len(got))
	}
}

func TestGenericPlugin_ValidatePush_NilSlice(t *testing.T) {
	p := New()

	// Nil input is passed through unchanged: the generic plugin returns
	// the entries slice as-is, so nil in → nil out.  This documents the
	// nil-vs-empty contract noted in Blade's review (Finding 2).
	got, err := p.ValidatePush(context.Background(), nil)
	if err != nil {
		t.Fatalf("ValidatePush(nil) error = %v", err)
	}
	if got != nil {
		t.Errorf("ValidatePush(nil) = %v (len %d), want nil", got, len(got))
	}
}

func TestGenericPlugin_OnReplay_NoOp(t *testing.T) {
	p := New()
	entries := []engramsync.ChangeLogEntry{
		{Sequence: 1, TableName: "lore", EntityID: "e1"},
	}

	err := p.OnReplay(context.Background(), &mockReplayStore{}, entries)
	if err != nil {
		t.Errorf("OnReplay() error = %v, want nil", err)
	}
}

// mockReplayStore is a minimal mock for testing OnReplay.
type mockReplayStore struct{}

func (m *mockReplayStore) UpsertRow(ctx context.Context, tableName, entityID string, payload []byte) error {
	return nil
}

func (m *mockReplayStore) DeleteRow(ctx context.Context, tableName, entityID string) error {
	return nil
}

func (m *mockReplayStore) QueueEmbedding(ctx context.Context, entryID string) error {
	return nil
}
