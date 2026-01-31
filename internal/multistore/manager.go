package multistore

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// StoreManager manages multiple isolated stores with lazy loading.
type StoreManager struct {
	rootPath string

	mu     sync.RWMutex
	stores map[string]*ManagedStore
}

// NewStoreManager creates a manager with the given root path.
// Creates the root directory if it doesn't exist.
func NewStoreManager(rootPath string) (*StoreManager, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(rootPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		rootPath = filepath.Join(home, rootPath[2:])
	}

	// Create root directory
	if err := os.MkdirAll(rootPath, 0755); err != nil {
		return nil, fmt.Errorf("create stores root directory: %w", err)
	}

	return &StoreManager{
		rootPath: rootPath,
		stores:   make(map[string]*ManagedStore),
	}, nil
}

// GetStore returns the store for the given ID, loading it if necessary.
// For non-default stores, returns ErrStoreNotFound if store doesn't exist.
// For the default store, creates it if it doesn't exist.
func (m *StoreManager) GetStore(ctx context.Context, storeID string) (*ManagedStore, error) {
	// Validate store ID
	if err := ValidateStoreID(storeID); err != nil {
		return nil, err
	}

	// Fast path: check if already loaded
	m.mu.RLock()
	if managed, ok := m.stores[storeID]; ok {
		m.mu.RUnlock()
		managed.TouchAccessed()
		return managed, nil
	}
	m.mu.RUnlock()

	// Slow path: load or create store
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if managed, ok := m.stores[storeID]; ok {
		managed.TouchAccessed()
		return managed, nil
	}

	storePath := m.storePath(storeID)

	// Check if store directory exists
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		// Only auto-create default store
		if !IsDefaultStore(storeID) {
			return nil, ErrStoreNotFound
		}

		// Create default store
		if err := m.createStoreDir(storeID, "Default store (auto-created)"); err != nil {
			return nil, err
		}
	}

	// Load the store
	managed, err := NewManagedStore(storeID, storePath)
	if err != nil {
		return nil, fmt.Errorf("load store %q: %w", storeID, err)
	}

	m.stores[storeID] = managed

	slog.Info("store loaded",
		"component", "multistore",
		"action", "store_loaded",
		"store_id", storeID,
	)

	managed.TouchAccessed()
	return managed, nil
}

// CreateStore creates a new store with the given ID.
// Returns ErrStoreAlreadyExists if store already exists.
func (m *StoreManager) CreateStore(ctx context.Context, storeID, description string) (*ManagedStore, error) {
	if err := ValidateStoreID(storeID); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	storePath := m.storePath(storeID)

	// Check if already exists
	if _, err := os.Stat(storePath); err == nil {
		return nil, ErrStoreAlreadyExists
	}

	// Create store directory and metadata
	if err := m.createStoreDir(storeID, description); err != nil {
		return nil, err
	}

	// Load the new store
	managed, err := NewManagedStore(storeID, storePath)
	if err != nil {
		return nil, fmt.Errorf("load new store %q: %w", storeID, err)
	}

	m.stores[storeID] = managed

	slog.Info("store created",
		"component", "multistore",
		"action", "store_created",
		"store_id", storeID,
	)

	return managed, nil
}

// DeleteStore removes a store and its data.
// Returns ErrStoreNotFound if store doesn't exist.
func (m *StoreManager) DeleteStore(ctx context.Context, storeID string) error {
	if err := ValidateStoreID(storeID); err != nil {
		return err
	}

	// Prevent deletion of default store
	if IsDefaultStore(storeID) {
		return fmt.Errorf("cannot delete default store")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	storePath := m.storePath(storeID)

	// Check if exists
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		return ErrStoreNotFound
	}

	// Close if loaded
	if managed, ok := m.stores[storeID]; ok {
		if err := managed.Close(); err != nil {
			slog.Warn("error closing store before deletion",
				"store_id", storeID, "error", err)
		}
		delete(m.stores, storeID)
	}

	// Remove directory
	if err := os.RemoveAll(storePath); err != nil {
		return fmt.Errorf("remove store directory: %w", err)
	}

	slog.Info("store deleted",
		"component", "multistore",
		"action", "store_deleted",
		"store_id", storeID,
	)

	return nil
}

// ListStores returns metadata for all existing stores.
func (m *StoreManager) ListStores(ctx context.Context) ([]StoreInfo, error) {
	entries, err := os.ReadDir(m.rootPath)
	if err != nil {
		return nil, fmt.Errorf("read stores directory: %w", err)
	}

	var result []StoreInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Recursively find stores (handles nested paths like org/project)
		stores, err := m.findStoresRecursive(entry.Name(), "")
		if err != nil {
			slog.Warn("error scanning store directory",
				"path", entry.Name(), "error", err)
			continue
		}
		result = append(result, stores...)
	}

	return result, nil
}

// findStoresRecursive discovers stores in nested directories.
func (m *StoreManager) findStoresRecursive(currentPath, prefix string) ([]StoreInfo, error) {
	fullPath := filepath.Join(m.rootPath, currentPath)
	if prefix != "" {
		fullPath = filepath.Join(m.rootPath, prefix, currentPath)
	}

	// Check if this is a store (has meta.yaml)
	metaPath := filepath.Join(fullPath, "meta.yaml")
	if _, err := os.Stat(metaPath); err == nil {
		storeID := currentPath
		if prefix != "" {
			storeID = prefix + "/" + currentPath
		}

		info, err := m.getStoreInfo(storeID, fullPath)
		if err != nil {
			return nil, err
		}
		return []StoreInfo{info}, nil
	}

	// Otherwise, scan subdirectories
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var result []StoreInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		newPrefix := currentPath
		if prefix != "" {
			newPrefix = prefix + "/" + currentPath
		}

		stores, err := m.findStoresRecursive(entry.Name(), newPrefix)
		if err != nil {
			continue // Skip problematic directories
		}
		result = append(result, stores...)
	}

	return result, nil
}

// getStoreInfo collects information about a single store.
func (m *StoreManager) getStoreInfo(storeID, basePath string) (StoreInfo, error) {
	metaPath := filepath.Join(basePath, "meta.yaml")
	meta, err := LoadStoreMeta(metaPath)
	if err != nil {
		return StoreInfo{}, err
	}

	// Get database size
	dbPath := filepath.Join(basePath, "engram.db")
	var sizeBytes int64
	if info, err := os.Stat(dbPath); err == nil {
		sizeBytes = info.Size()
	}

	return StoreInfo{
		ID:           storeID,
		Created:      meta.Created,
		LastAccessed: meta.LastAccessed,
		Description:  meta.Description,
		SizeBytes:    sizeBytes,
	}, nil
}

// storePath returns the filesystem path for a store ID.
func (m *StoreManager) storePath(storeID string) string {
	// Store ID segments map to directory structure
	return filepath.Join(m.rootPath, storeID)
}

// createStoreDir creates a new store directory with metadata.
func (m *StoreManager) createStoreDir(storeID, description string) error {
	storePath := m.storePath(storeID)

	if err := os.MkdirAll(storePath, 0755); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}

	meta := NewStoreMeta(description)
	metaPath := filepath.Join(storePath, "meta.yaml")

	if err := SaveStoreMeta(metaPath, meta); err != nil {
		// Clean up directory on failure
		os.RemoveAll(storePath)
		return fmt.Errorf("write store metadata: %w", err)
	}

	return nil
}

// Close closes all loaded stores.
func (m *StoreManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for id, managed := range m.stores {
		if err := managed.Close(); err != nil {
			slog.Error("error closing store", "store_id", id, "error", err)
			lastErr = err
		}
	}

	return lastErr
}
