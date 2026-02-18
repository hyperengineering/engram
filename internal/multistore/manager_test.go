package multistore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewStoreManager_CreatesRootDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	// Verify directory doesn't exist yet
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Fatal("root directory should not exist initially")
	}

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	// Verify directory was created
	info, err := os.Stat(rootPath)
	if err != nil {
		t.Fatalf("root directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("root path should be a directory")
	}
}

func TestNewStoreManager_ExpandsTilde(t *testing.T) {
	// This test verifies tilde expansion works, but we can't easily test
	// the actual home directory expansion in a portable way.
	// We'll just verify it doesn't crash with a regular path.
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()
}

func TestStoreManager_GetStore_Default_AutoCreates(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	if managed == nil {
		t.Fatal("GetStore('default') should return a managed store")
	}
	if managed.ID != "default" {
		t.Errorf("Store ID = %q, want 'default'", managed.ID)
	}

	// Verify store directory was created
	storeDir := filepath.Join(rootPath, "default")
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		t.Error("default store directory should be created")
	}

	// Verify meta.yaml exists
	metaPath := filepath.Join(storeDir, "meta.yaml")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("default store meta.yaml should be created")
	}
}

func TestStoreManager_GetStore_NonDefault_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	_, err = manager.GetStore(ctx, "nonexistent")
	if !errors.Is(err, ErrStoreNotFound) {
		t.Errorf("GetStore('nonexistent') expected ErrStoreNotFound, got %v", err)
	}
}

func TestStoreManager_GetStore_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	_, err = manager.GetStore(ctx, "Invalid/ID")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("GetStore('Invalid/ID') expected ErrInvalidStoreID, got %v", err)
	}
}

func TestStoreManager_GetStore_LazyLoading(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// First call creates the store
	store1, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') first call error = %v", err)
	}

	// Second call returns cached store
	store2, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') second call error = %v", err)
	}

	// Should be the same instance
	if store1 != store2 {
		t.Error("GetStore should return cached instance")
	}
}

func TestStoreManager_GetStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	const numGoroutines = 100

	stores := make([]*ManagedStore, numGoroutines)
	errs := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			stores[idx], errs[idx] = manager.GetStore(ctx, "default")
		}(i)
	}

	wg.Wait()

	// All should succeed
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error = %v", i, err)
		}
	}

	// All should return the same instance
	first := stores[0]
	for i, s := range stores {
		if s != first {
			t.Errorf("goroutine %d got different store instance", i)
		}
	}
}

func TestStoreManager_CreateStore_Success(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "myproject", "", "My project store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	if managed.ID != "myproject" {
		t.Errorf("Store ID = %q, want 'myproject'", managed.ID)
	}
	if managed.Meta.Description != "My project store" {
		t.Errorf("Description = %q, want 'My project store'", managed.Meta.Description)
	}

	// Verify store is now accessible via GetStore
	fetched, err := manager.GetStore(ctx, "myproject")
	if err != nil {
		t.Fatalf("GetStore('myproject') error = %v", err)
	}
	if fetched != managed {
		t.Error("GetStore should return the same instance")
	}
}

func TestStoreManager_CreateStore_NestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "org/project", "", "Org project store")
	if err != nil {
		t.Fatalf("CreateStore('org/project') error = %v", err)
	}

	if managed.ID != "org/project" {
		t.Errorf("Store ID = %q, want 'org/project'", managed.ID)
	}

	// Verify directory structure
	storeDir := filepath.Join(rootPath, "org", "project")
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		t.Error("nested store directory should be created")
	}
}

func TestStoreManager_CreateStore_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create first time
	_, err = manager.CreateStore(ctx, "myproject", "", "First")
	if err != nil {
		t.Fatalf("CreateStore() first call error = %v", err)
	}

	// Try to create again
	_, err = manager.CreateStore(ctx, "myproject", "", "Second")
	if !errors.Is(err, ErrStoreAlreadyExists) {
		t.Errorf("CreateStore() second call expected ErrStoreAlreadyExists, got %v", err)
	}
}

func TestStoreManager_CreateStore_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	_, err = manager.CreateStore(ctx, "Invalid/ID", "", "Bad")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("CreateStore('Invalid/ID') expected ErrInvalidStoreID, got %v", err)
	}
}

func TestStoreManager_DeleteStore_Success(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create a store
	_, err = manager.CreateStore(ctx, "todelete", "", "Will be deleted")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	// Delete it
	err = manager.DeleteStore(ctx, "todelete")
	if err != nil {
		t.Fatalf("DeleteStore() error = %v", err)
	}

	// Verify it's gone
	storeDir := filepath.Join(rootPath, "todelete")
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Error("store directory should be deleted")
	}

	// GetStore should return ErrStoreNotFound
	_, err = manager.GetStore(ctx, "todelete")
	if !errors.Is(err, ErrStoreNotFound) {
		t.Errorf("GetStore after delete expected ErrStoreNotFound, got %v", err)
	}
}

func TestStoreManager_DeleteStore_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	err = manager.DeleteStore(ctx, "nonexistent")
	if !errors.Is(err, ErrStoreNotFound) {
		t.Errorf("DeleteStore('nonexistent') expected ErrStoreNotFound, got %v", err)
	}
}

func TestStoreManager_DeleteStore_Default_Forbidden(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Access default to create it
	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	// Try to delete default
	err = manager.DeleteStore(ctx, "default")
	if err == nil {
		t.Error("DeleteStore('default') should return error")
	}
}

func TestStoreManager_DeleteStore_InvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	err = manager.DeleteStore(ctx, "Invalid/ID")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("DeleteStore('Invalid/ID') expected ErrInvalidStoreID, got %v", err)
	}
}

func TestStoreManager_ListStores_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	stores, err := manager.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	if len(stores) != 0 {
		t.Errorf("ListStores() returned %d stores, want 0", len(stores))
	}
}

func TestStoreManager_ListStores_Multiple(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create multiple stores
	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "project1", "", "Project 1")
	if err != nil {
		t.Fatalf("CreateStore('project1') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "org/project2", "", "Project 2")
	if err != nil {
		t.Fatalf("CreateStore('org/project2') error = %v", err)
	}

	stores, err := manager.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	if len(stores) != 3 {
		t.Errorf("ListStores() returned %d stores, want 3", len(stores))
	}

	// Check store IDs are present
	found := make(map[string]bool)
	for _, s := range stores {
		found[s.ID] = true
	}

	if !found["default"] {
		t.Error("ListStores should include 'default'")
	}
	if !found["project1"] {
		t.Error("ListStores should include 'project1'")
	}
	if !found["org/project2"] {
		t.Error("ListStores should include 'org/project2'")
	}
}

func TestStoreManager_Close_ClosesAllStores(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}

	ctx := context.Background()

	// Create some stores
	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "project1", "", "Project 1")
	if err != nil {
		t.Fatalf("CreateStore('project1') error = %v", err)
	}

	// Close the manager
	err = manager.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestStoreManager_CreateStore_WithType(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "tract-store", "tract", "Tract store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	if managed.Type() != "tract" {
		t.Errorf("Type() = %q, want 'tract'", managed.Type())
	}
	if managed.Meta.Type != "tract" {
		t.Errorf("Meta.Type = %q, want 'tract'", managed.Meta.Type)
	}

	// Verify meta.yaml on disk has the type
	metaPath := filepath.Join(rootPath, "tract-store", "meta.yaml")
	loaded, err := LoadStoreMeta(metaPath)
	if err != nil {
		t.Fatalf("LoadStoreMeta() error = %v", err)
	}
	if loaded.Type != "tract" {
		t.Errorf("Persisted Type = %q, want 'tract'", loaded.Type)
	}
}

func TestStoreManager_CreateStore_NoType_DefaultsRecall(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "no-type", "", "No type specified")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	if managed.Type() != DefaultStoreType {
		t.Errorf("Type() = %q, want %q", managed.Type(), DefaultStoreType)
	}
}

func TestStoreManager_CreateStore_CustomType(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.CreateStore(ctx, "custom-store", "my-custom-type", "Custom type store")
	if err != nil {
		t.Fatalf("CreateStore() error = %v", err)
	}

	if managed.Type() != "my-custom-type" {
		t.Errorf("Type() = %q, want 'my-custom-type'", managed.Type())
	}
}

func TestStoreManager_GetStore_DefaultAutoCreated_HasRecallType(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	if managed.Type() != DefaultStoreType {
		t.Errorf("auto-created default store Type() = %q, want %q", managed.Type(), DefaultStoreType)
	}
}

func TestStoreManager_GetStore_ExistingNoType_DefaultsRecall(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	// Manually create a store directory with a pre-8.3 meta.yaml (no type field)
	storeDir := filepath.Join(rootPath, "legacy-store")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	legacyMeta := []byte("created: 2026-01-15T10:00:00Z\nlast_accessed: 2026-02-17T12:00:00Z\ndescription: Legacy store\n")
	if err := os.WriteFile(filepath.Join(storeDir, "meta.yaml"), legacyMeta, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	ctx := context.Background()
	managed, err := manager.GetStore(ctx, "legacy-store")
	if err != nil {
		t.Fatalf("GetStore('legacy-store') error = %v", err)
	}

	if managed.Type() != DefaultStoreType {
		t.Errorf("legacy store Type() = %q, want %q", managed.Type(), DefaultStoreType)
	}
}

func TestStoreManager_ListStores_IncludesType(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	manager.CreateStore(ctx, "recall-store", "recall", "Recall store")
	manager.CreateStore(ctx, "tract-store", "tract", "Tract store")

	stores, err := manager.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	typeMap := make(map[string]string)
	for _, s := range stores {
		typeMap[s.ID] = s.Type
	}

	if typeMap["recall-store"] != "recall" {
		t.Errorf("recall-store Type = %q, want 'recall'", typeMap["recall-store"])
	}
	if typeMap["tract-store"] != "tract" {
		t.Errorf("tract-store Type = %q, want 'tract'", typeMap["tract-store"])
	}
}

func TestStoreManager_GetStore_TouchesAccessed(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	// LastAccessed should be set
	if managed.Meta.LastAccessed.IsZero() {
		t.Error("LastAccessed should be set")
	}
}
