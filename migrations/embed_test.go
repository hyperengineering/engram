package migrations

import (
	"testing"
)

func TestEmbeddedFS_ContainsMigrationFiles(t *testing.T) {
	// Given: The embedded filesystem
	// When: We read the directory
	entries, err := FS.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read embedded FS: %v", err)
	}

	// Then: It contains the initial schema migration
	found := false
	for _, entry := range entries {
		if entry.Name() == "001_initial_schema.sql" {
			found = true
			break
		}
	}

	if !found {
		t.Error("001_initial_schema.sql not found in embedded FS")
	}
}

func TestEmbeddedFS_MigrationFileReadable(t *testing.T) {
	// Given: The embedded filesystem
	// When: We read the migration file
	content, err := FS.ReadFile("001_initial_schema.sql")
	if err != nil {
		t.Fatalf("failed to read migration file: %v", err)
	}

	// Then: It contains goose directives
	contentStr := string(content)
	if len(contentStr) == 0 {
		t.Error("migration file is empty")
	}

	// Verify goose markers are present
	if !contains(contentStr, "-- +goose Up") {
		t.Error("migration missing '-- +goose Up' directive")
	}
	if !contains(contentStr, "-- +goose Down") {
		t.Error("migration missing '-- +goose Down' directive")
	}
	if !contains(contentStr, "CREATE TABLE lore_entries") {
		t.Error("migration missing lore_entries table creation")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
