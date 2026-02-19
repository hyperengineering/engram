package plugin

import (
	"context"
	"sort"
	gosync "sync"
	"testing"

	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// stubPlugin is a minimal DomainPlugin for testing the registry.
type stubPlugin struct {
	typeName string
	schemas  []TableSchema
}

func (s *stubPlugin) Type() string { return s.typeName }
func (s *stubPlugin) Migrations() []Migration { return nil }
func (s *stubPlugin) ValidatePush(_ context.Context, entries []engramsync.ChangeLogEntry) ([]engramsync.ChangeLogEntry, error) {
	return entries, nil
}
func (s *stubPlugin) OnReplay(_ context.Context, _ ReplayStore, _ []engramsync.ChangeLogEntry) error {
	return nil
}
func (s *stubPlugin) TableSchemas() []TableSchema { return s.schemas }

func TestRegister_NewPlugin(t *testing.T) {
	Reset()
	p := &stubPlugin{typeName: "recall"}
	Register(p)

	got, ok := Get("recall")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if got.Type() != "recall" {
		t.Errorf("Get().Type() = %q, want %q", got.Type(), "recall")
	}
}

func TestRegister_Duplicate(t *testing.T) {
	Reset()
	Register(&stubPlugin{typeName: "recall"})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register duplicate did not panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T(%v), want string", r, r)
		}
		if msg != "plugin already registered: recall" {
			t.Errorf("panic message = %q, want %q", msg, "plugin already registered: recall")
		}
	}()

	Register(&stubPlugin{typeName: "recall"})
}

func TestGet_Registered(t *testing.T) {
	Reset()
	Register(&stubPlugin{typeName: "recall"})

	p, ok := Get("recall")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}
	if p.Type() != "recall" {
		t.Errorf("Type() = %q, want %q", p.Type(), "recall")
	}
}

func TestGet_NotRegistered_WithGeneric(t *testing.T) {
	Reset()
	gen := &stubPlugin{typeName: "generic"}
	SetGeneric(gen)

	p, ok := Get("tract")
	if ok {
		t.Error("Get() ok = true, want false for unregistered type")
	}
	if p == nil {
		t.Fatal("Get() returned nil, want generic plugin")
	}
	if p.Type() != "generic" {
		t.Errorf("Type() = %q, want %q", p.Type(), "generic")
	}
}

func TestGet_NoGeneric(t *testing.T) {
	Reset()

	p, ok := Get("unknown")
	if ok {
		t.Error("Get() ok = true, want false")
	}
	if p != nil {
		t.Errorf("Get() = %v, want nil", p)
	}
}

func TestMustGet_Found(t *testing.T) {
	Reset()
	Register(&stubPlugin{typeName: "recall"})

	p := MustGet("recall")
	if p.Type() != "recall" {
		t.Errorf("Type() = %q, want %q", p.Type(), "recall")
	}
}

func TestMustGet_NotFound_Panics(t *testing.T) {
	Reset()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustGet did not panic for unregistered type")
		}
	}()

	MustGet("unknown")
}

func TestSetGeneric(t *testing.T) {
	Reset()
	gen := &stubPlugin{typeName: "generic"}
	SetGeneric(gen)

	p, ok := Get("anything")
	if ok {
		t.Error("Get() ok = true, want false")
	}
	if p == nil {
		t.Fatal("Get() returned nil, want generic")
	}
	if p.Type() != "generic" {
		t.Errorf("Type() = %q, want %q", p.Type(), "generic")
	}
}

func TestRegisteredTypes(t *testing.T) {
	Reset()
	Register(&stubPlugin{typeName: "recall"})
	Register(&stubPlugin{typeName: "tract"})

	types := RegisteredTypes()
	sort.Strings(types)

	if len(types) != 2 {
		t.Fatalf("RegisteredTypes() returned %d types, want 2", len(types))
	}
	if types[0] != "recall" {
		t.Errorf("types[0] = %q, want %q", types[0], "recall")
	}
	if types[1] != "tract" {
		t.Errorf("types[1] = %q, want %q", types[1], "tract")
	}
}

func TestReset(t *testing.T) {
	Reset()
	SetGeneric(&stubPlugin{typeName: "generic"})
	Register(&stubPlugin{typeName: "recall"})

	Reset()

	p, ok := Get("recall")
	if ok {
		t.Error("Get() ok = true after Reset, want false")
	}
	if p != nil {
		t.Error("Get() returned non-nil after Reset, want nil")
	}

	types := RegisteredTypes()
	if len(types) != 0 {
		t.Errorf("RegisteredTypes() = %v, want empty", types)
	}
}

func TestPluginRegistry_ConcurrentAccess(t *testing.T) {
	Reset()
	SetGeneric(&stubPlugin{typeName: "generic"})

	// Register 10 plugins sequentially first (Register panics on duplicate).
	for i := 0; i < 10; i++ {
		Register(&stubPlugin{typeName: stubType(i)})
	}

	// Concurrent reads — 100 goroutines calling Get simultaneously.
	var wg gosync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			storeType := stubType(idx % 10)
			p, ok := Get(storeType)
			if !ok {
				t.Errorf("Get(%q) ok = false, want true", storeType)
			}
			if p.Type() != storeType {
				t.Errorf("Type() = %q, want %q", p.Type(), storeType)
			}
		}(i)
	}

	// Also mix in concurrent reads for unregistered types.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, ok := Get("nonexistent")
			if ok {
				t.Error("Get(nonexistent) ok = true, want false")
			}
			if p == nil {
				t.Error("Get(nonexistent) returned nil, want generic")
			}
		}()
	}

	wg.Wait()
}

func stubType(i int) string {
	return "type-" + string(rune('a'+i))
}

// --- Table Schema Registry Tests ---

func TestRegister_RegistersTableSchemas(t *testing.T) {
	Reset()
	p := &stubPlugin{
		typeName: "test-plugin",
		schemas: []TableSchema{
			{Name: "test_table", Columns: []string{"id", "name"}, SoftDelete: false},
			{Name: "other_table", Columns: []string{"id", "value"}, SoftDelete: true},
		},
	}
	Register(p)

	s, ok := GetTableSchema("test_table")
	if !ok {
		t.Fatal("GetTableSchema(test_table) ok = false, want true")
	}
	if s.Name != "test_table" {
		t.Errorf("Name = %q, want %q", s.Name, "test_table")
	}
	if len(s.Columns) != 2 {
		t.Errorf("Columns length = %d, want 2", len(s.Columns))
	}

	s2, ok2 := GetTableSchema("other_table")
	if !ok2 {
		t.Fatal("GetTableSchema(other_table) ok = false, want true")
	}
	if !s2.SoftDelete {
		t.Error("SoftDelete = false, want true")
	}
}

func TestGetTableSchema_NotFound(t *testing.T) {
	Reset()

	_, ok := GetTableSchema("nonexistent")
	if ok {
		t.Error("GetTableSchema(nonexistent) ok = true, want false")
	}
}

func TestReset_ClearsTableSchemas(t *testing.T) {
	Reset()
	p := &stubPlugin{
		typeName: "schema-plugin",
		schemas: []TableSchema{
			{Name: "some_table", Columns: []string{"id"}, SoftDelete: false},
		},
	}
	Register(p)

	// Verify it was registered
	_, ok := GetTableSchema("some_table")
	if !ok {
		t.Fatal("GetTableSchema should return true before Reset")
	}

	Reset()

	_, ok = GetTableSchema("some_table")
	if ok {
		t.Error("GetTableSchema should return false after Reset")
	}
}

func TestRegister_NilSchemas_NoError(t *testing.T) {
	Reset()
	p := &stubPlugin{typeName: "nil-schemas", schemas: nil}
	Register(p) // should not panic

	_, ok := GetTableSchema("anything")
	if ok {
		t.Error("no schemas should be registered for nil TableSchemas()")
	}
}
