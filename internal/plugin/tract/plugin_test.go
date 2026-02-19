package tract

import (
	"database/sql"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
	_ "modernc.org/sqlite"
)

// --- Seed 3.1: Plugin skeleton ---

func TestTractPlugin_Type(t *testing.T) {
	p := New()
	if got := p.Type(); got != "tract" {
		t.Errorf("Type() = %q, want %q", got, "tract")
	}
}

func TestTractPlugin_ImplementsDomainPlugin(t *testing.T) {
	var _ plugin.DomainPlugin = (*Plugin)(nil)
}

// --- Seed 3.2: Migrations ---

func TestTractPlugin_Migrations_NotNil(t *testing.T) {
	p := New()
	migs := p.Migrations()
	if migs == nil {
		t.Fatal("Migrations() returned nil")
	}
	if len(migs) == 0 {
		t.Fatal("Migrations() returned empty slice")
	}
}

func TestTractPlugin_Migrations_HasVersion100(t *testing.T) {
	p := New()
	migs := p.Migrations()
	if migs[0].Version != 100 {
		t.Errorf("Migrations()[0].Version = %d, want 100", migs[0].Version)
	}
	if migs[0].Name != "create_tract_tables" {
		t.Errorf("Migrations()[0].Name = %q, want %q", migs[0].Name, "create_tract_tables")
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	return db
}

func TestTractPlugin_MigrationSQL_CreatesGoalsTable(t *testing.T) {
	db := newTestDB(t)
	p := New()
	migs := p.Migrations()

	if _, err := db.Exec(migs[0].UpSQL); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	// Verify goals table exists with expected columns
	rows, err := db.Query("PRAGMA table_info(goals)")
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typeName string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		columns[name] = true
	}

	expectedCols := []string{"id", "title", "description", "status", "priority", "parent_goal_id", "created_at", "updated_at", "deleted_at"}
	for _, col := range expectedCols {
		if !columns[col] {
			t.Errorf("goals table missing column %q", col)
		}
	}
}

func TestTractPlugin_MigrationSQL_CreatesAllFourTables(t *testing.T) {
	db := newTestDB(t)
	p := New()
	migs := p.Migrations()

	if _, err := db.Exec(migs[0].UpSQL); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	tables := []string{"goals", "csfs", "fwus", "implementation_contexts"}
	for _, table := range tables {
		var count int
		err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Fatalf("query sqlite_master for %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("table %q not created", table)
		}
	}
}

func TestTractPlugin_MigrationSQL_ForeignKeys(t *testing.T) {
	db := newTestDB(t)
	p := New()
	migs := p.Migrations()

	if _, err := db.Exec(migs[0].UpSQL); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	// Try to insert a CSF without a parent goal — should fail with FK violation
	_, err := db.Exec(`INSERT INTO csfs (id, goal_id, title, status, created_at, updated_at) VALUES ('c1', 'nonexistent', 'Test', 'tracking', '2026-01-01', '2026-01-01')`)
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

// --- Seed 3.3: TableSchemas ---

func TestTractPlugin_TableSchemas_Returns4(t *testing.T) {
	p := New()
	schemas := p.TableSchemas()
	if len(schemas) != 4 {
		t.Fatalf("TableSchemas() returned %d schemas, want 4", len(schemas))
	}
}

func TestTractPlugin_TableSchemas_AllSoftDelete(t *testing.T) {
	p := New()
	schemas := p.TableSchemas()
	for _, s := range schemas {
		if !s.SoftDelete {
			t.Errorf("table %q SoftDelete = false, want true", s.Name)
		}
	}
}

func TestTractPlugin_TableSchemas_ColumnsIncludeID(t *testing.T) {
	p := New()
	schemas := p.TableSchemas()
	for _, s := range schemas {
		if len(s.Columns) == 0 || s.Columns[0] != "id" {
			t.Errorf("table %q first column = %v, want 'id'", s.Name, s.Columns)
		}
	}
}

func TestTractPlugin_TableSchemas_GoalsColumns(t *testing.T) {
	p := New()
	schemas := p.TableSchemas()

	// Find goals schema
	var goals *plugin.TableSchema
	for _, s := range schemas {
		if s.Name == "goals" {
			s := s // avoid loop variable capture
			goals = &s
			break
		}
	}
	if goals == nil {
		t.Fatal("goals schema not found")
	}

	expectedCols := []string{"id", "title", "description", "status", "priority", "parent_goal_id", "created_at", "updated_at", "deleted_at"}
	if len(goals.Columns) != len(expectedCols) {
		t.Fatalf("goals columns = %v, want %v", goals.Columns, expectedCols)
	}
	for i, col := range goals.Columns {
		if col != expectedCols[i] {
			t.Errorf("goals.Columns[%d] = %q, want %q", i, col, expectedCols[i])
		}
	}
}
