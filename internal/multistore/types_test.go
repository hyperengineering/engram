package multistore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStoreMeta(t *testing.T) {
	before := time.Now().UTC()
	meta := NewStoreMeta("Test store description")
	after := time.Now().UTC()

	if meta.Description != "Test store description" {
		t.Errorf("expected description 'Test store description', got %q", meta.Description)
	}

	if meta.Created.Before(before) || meta.Created.After(after) {
		t.Errorf("Created timestamp %v should be between %v and %v", meta.Created, before, after)
	}

	if meta.LastAccessed.Before(before) || meta.LastAccessed.After(after) {
		t.Errorf("LastAccessed timestamp %v should be between %v and %v", meta.LastAccessed, before, after)
	}

	if meta.Created != meta.LastAccessed {
		t.Errorf("expected Created and LastAccessed to be equal for new store, got %v and %v",
			meta.Created, meta.LastAccessed)
	}
}

func TestStoreMeta_SaveLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	original := &StoreMeta{
		Created:      time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC),
		LastAccessed: time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC),
		Description:  "Test store for round-trip",
	}

	// Save
	if err := SaveStoreMeta(metaPath, original); err != nil {
		t.Fatalf("SaveStoreMeta failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatal("meta.yaml file was not created")
	}

	// Load
	loaded, err := LoadStoreMeta(metaPath)
	if err != nil {
		t.Fatalf("LoadStoreMeta failed: %v", err)
	}

	// Compare
	if !loaded.Created.Equal(original.Created) {
		t.Errorf("Created mismatch: got %v, want %v", loaded.Created, original.Created)
	}
	if !loaded.LastAccessed.Equal(original.LastAccessed) {
		t.Errorf("LastAccessed mismatch: got %v, want %v", loaded.LastAccessed, original.LastAccessed)
	}
	if loaded.Description != original.Description {
		t.Errorf("Description mismatch: got %q, want %q", loaded.Description, original.Description)
	}
}

func TestStoreMeta_LoadNonExistent(t *testing.T) {
	_, err := LoadStoreMeta("/nonexistent/path/meta.yaml")
	if err == nil {
		t.Error("expected error loading non-existent file, got nil")
	}
}

func TestStoreMeta_LoadMalformed(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	// Write malformed YAML
	if err := os.WriteFile(metaPath, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatalf("failed to write malformed file: %v", err)
	}

	_, err := LoadStoreMeta(metaPath)
	if err == nil {
		t.Error("expected error loading malformed YAML, got nil")
	}
}

func TestStoreMeta_EmptyDescription(t *testing.T) {
	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "meta.yaml")

	original := NewStoreMeta("")

	if err := SaveStoreMeta(metaPath, original); err != nil {
		t.Fatalf("SaveStoreMeta failed: %v", err)
	}

	loaded, err := LoadStoreMeta(metaPath)
	if err != nil {
		t.Fatalf("LoadStoreMeta failed: %v", err)
	}

	if loaded.Description != "" {
		t.Errorf("expected empty description, got %q", loaded.Description)
	}
}

func TestStoreInfo_Fields(t *testing.T) {
	now := time.Now().UTC()
	info := StoreInfo{
		ID:           "test/store",
		Created:      now,
		LastAccessed: now,
		Description:  "Test store",
		SizeBytes:    1024,
	}

	if info.ID != "test/store" {
		t.Errorf("expected ID 'test/store', got %q", info.ID)
	}
	if info.SizeBytes != 1024 {
		t.Errorf("expected SizeBytes 1024, got %d", info.SizeBytes)
	}
}
