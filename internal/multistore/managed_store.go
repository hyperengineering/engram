package multistore

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/hyperengineering/engram/internal/store"
)

// ManagedStore wraps a SQLiteStore with metadata and access tracking.
type ManagedStore struct {
	ID       string
	Store    store.Store
	Meta     *StoreMeta
	BasePath string // Directory containing this store

	mu        sync.Mutex
	metaDirty bool // Track if metadata needs saving
}

// NewManagedStore creates a managed store from an existing directory.
func NewManagedStore(id, basePath string) (*ManagedStore, error) {
	dbPath := filepath.Join(basePath, "engram.db")
	metaPath := filepath.Join(basePath, "meta.yaml")

	// Load metadata
	meta, err := LoadStoreMeta(metaPath)
	if err != nil {
		return nil, fmt.Errorf("load store metadata: %w", err)
	}

	// Open SQLite store with store ID for logging context
	sqliteStore, err := store.NewSQLiteStore(dbPath, store.WithStoreID(id))
	if err != nil {
		return nil, fmt.Errorf("open store database: %w", err)
	}

	return &ManagedStore{
		ID:       id,
		Store:    sqliteStore,
		Meta:     meta,
		BasePath: basePath,
	}, nil
}

// TouchAccessed updates the last_accessed timestamp.
// Saves metadata to disk periodically (not on every access).
func (m *ManagedStore) TouchAccessed() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Meta.LastAccessed = time.Now().UTC()
	m.metaDirty = true
}

// FlushMeta saves metadata to disk if dirty.
func (m *ManagedStore) FlushMeta() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.metaDirty {
		return nil
	}

	metaPath := filepath.Join(m.BasePath, "meta.yaml")
	if err := SaveStoreMeta(metaPath, m.Meta); err != nil {
		return err
	}

	m.metaDirty = false
	return nil
}

// Close closes the underlying store and flushes metadata.
func (m *ManagedStore) Close() error {
	if err := m.FlushMeta(); err != nil {
		// Log but don't fail close
		slog.Warn("failed to flush store metadata", "store_id", m.ID, "error", err)
	}
	return m.Store.Close()
}

// Type returns the store type from metadata.
func (m *ManagedStore) Type() string {
	return m.Meta.Type
}

// SchemaVersion returns the schema version from the database.
// Returns 0 if unable to read (e.g., old database without sync_meta).
func (m *ManagedStore) SchemaVersion(ctx context.Context) int {
	version, err := m.Store.GetSyncMeta(ctx, "schema_version")
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(version)
	return v
}
