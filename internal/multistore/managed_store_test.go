package multistore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/store"
)

func TestNewManagedStore_Success(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")

	// Create store directory structure
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	// Create meta.yaml
	meta := NewStoreMeta("Test store")
	metaPath := filepath.Join(storeDir, "meta.yaml")
	if err := SaveStoreMeta(metaPath, meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	// Create empty database (will be created by SQLiteStore)
	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}
	defer managed.Close()

	if managed.ID != "test-store" {
		t.Errorf("ID = %q, want %q", managed.ID, "test-store")
	}
	if managed.BasePath != storeDir {
		t.Errorf("BasePath = %q, want %q", managed.BasePath, storeDir)
	}
	if managed.Store == nil {
		t.Error("Store should not be nil")
	}
	if managed.Meta == nil {
		t.Error("Meta should not be nil")
	}
	if managed.Meta.Description != "Test store" {
		t.Errorf("Meta.Description = %q, want %q", managed.Meta.Description, "Test store")
	}
}

func TestNewManagedStore_MissingMeta(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")

	// Create store directory without meta.yaml
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	_, err := NewManagedStore("test-store", storeDir)
	if err == nil {
		t.Error("expected error when meta.yaml missing, got nil")
	}
}

func TestManagedStore_TouchAccessed(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")

	// Setup store
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	meta := NewStoreMeta("Test store")
	originalAccessed := meta.LastAccessed
	if err := SaveStoreMeta(filepath.Join(storeDir, "meta.yaml"), meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}
	defer managed.Close()

	// Wait a tiny bit to ensure time changes
	time.Sleep(10 * time.Millisecond)

	// Touch accessed
	managed.TouchAccessed()

	if !managed.Meta.LastAccessed.After(originalAccessed) {
		t.Errorf("LastAccessed should be updated after TouchAccessed")
	}
}

func TestManagedStore_FlushMeta(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")
	metaPath := filepath.Join(storeDir, "meta.yaml")

	// Setup store
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	meta := NewStoreMeta("Initial description")
	if err := SaveStoreMeta(metaPath, meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}
	defer managed.Close()

	// Modify metadata
	managed.TouchAccessed()

	// Flush
	if err := managed.FlushMeta(); err != nil {
		t.Fatalf("FlushMeta() error = %v", err)
	}

	// Reload and verify
	reloaded, err := LoadStoreMeta(metaPath)
	if err != nil {
		t.Fatalf("LoadStoreMeta() error = %v", err)
	}

	if !reloaded.LastAccessed.Equal(managed.Meta.LastAccessed) {
		t.Errorf("Flushed LastAccessed = %v, want %v", reloaded.LastAccessed, managed.Meta.LastAccessed)
	}
}

func TestManagedStore_FlushMeta_NotDirty(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")
	metaPath := filepath.Join(storeDir, "meta.yaml")

	// Setup store
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	meta := NewStoreMeta("Test")
	if err := SaveStoreMeta(metaPath, meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}
	defer managed.Close()

	// Flush without changes should be a no-op
	if err := managed.FlushMeta(); err != nil {
		t.Fatalf("FlushMeta() error = %v", err)
	}
}

func TestManagedStore_Close(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")

	// Setup store
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	meta := NewStoreMeta("Test")
	if err := SaveStoreMeta(filepath.Join(storeDir, "meta.yaml"), meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}

	// Mark dirty
	managed.TouchAccessed()

	// Close should flush and close the store
	if err := managed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify database file was created
	dbPath := filepath.Join(storeDir, "engram.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file should exist after close")
	}
}

func TestManagedStore_StoreImplementsInterface(t *testing.T) {
	tmpDir := t.TempDir()
	storeDir := filepath.Join(tmpDir, "test-store")

	// Setup store
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	meta := NewStoreMeta("Test")
	if err := SaveStoreMeta(filepath.Join(storeDir, "meta.yaml"), meta); err != nil {
		t.Fatalf("failed to save meta: %v", err)
	}

	managed, err := NewManagedStore("test-store", storeDir)
	if err != nil {
		t.Fatalf("NewManagedStore() error = %v", err)
	}
	defer managed.Close()

	// Verify the underlying store implements store.Store interface
	var _ store.Store = managed.Store
}
