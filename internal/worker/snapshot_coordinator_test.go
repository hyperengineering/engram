package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
)

// mockStoreEnumerator implements StoreEnumerator for testing.
type mockStoreEnumerator struct {
	mu        sync.Mutex
	stores    []multistore.StoreInfo
	listErr   error
	getStores map[string]*mockCoordinatorStore
	getErr    map[string]error
}

func newMockStoreEnumerator(storeIDs ...string) *mockStoreEnumerator {
	m := &mockStoreEnumerator{
		stores:    make([]multistore.StoreInfo, 0, len(storeIDs)),
		getStores: make(map[string]*mockCoordinatorStore),
		getErr:    make(map[string]error),
	}
	for _, id := range storeIDs {
		m.stores = append(m.stores, multistore.StoreInfo{ID: id})
		m.getStores[id] = &mockCoordinatorStore{
			// Buffer size 10 allows multiple snapshot calls per store without blocking.
			// This accommodates tests that run multiple interval cycles (e.g., initial + 2-3 ticks).
			called: make(chan struct{}, 10),
		}
	}
	return m
}

// waitForCalls waits until totalCalls snapshot generations have occurred across all stores.
// Returns true if completed within timeout, false otherwise.
func (m *mockStoreEnumerator) waitForCalls(totalCalls int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		current := 0
		m.mu.Lock()
		for _, store := range m.getStores {
			current += store.getCalls()
		}
		m.mu.Unlock()

		if current >= totalCalls {
			return true
		}

		select {
		case <-deadline:
			return false
		case <-time.After(5 * time.Millisecond):
			// Poll again
		}
	}
}

func (m *mockStoreEnumerator) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.stores, nil
}

func (m *mockStoreEnumerator) GetStore(ctx context.Context, storeID string) (SnapshotCapableStore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.getErr[storeID]; ok && err != nil {
		return nil, err
	}
	if ms, ok := m.getStores[storeID]; ok {
		return ms, nil
	}
	return nil, errors.New("store not found")
}

func (m *mockStoreEnumerator) getSnapshotCalls(storeID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		return ms.getCalls()
	}
	return 0
}

func (m *mockStoreEnumerator) setStoreError(storeID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		ms.setError(err)
	}
}

// mockCoordinatorStore implements snapshot generation for coordinator tests.
type mockCoordinatorStore struct {
	mu       sync.Mutex
	calls    int
	err      error
	duration time.Duration
	called   chan struct{} // Signals when GenerateSnapshot is called
}

func (m *mockCoordinatorStore) GenerateSnapshot(ctx context.Context) error {
	m.mu.Lock()
	m.calls++
	err := m.err
	duration := m.duration
	called := m.called
	m.mu.Unlock()

	// Signal that we were called (non-blocking)
	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}

	if duration > 0 {
		select {
		case <-time.After(duration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (m *mockCoordinatorStore) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockCoordinatorStore) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// --- Tests ---

func TestSnapshotCoordinator_IteratesAllStores(t *testing.T) {
	enum := newMockStoreEnumerator("default", "project-a", "org/project-b")

	coord := NewSnapshotCoordinator(enum, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for all 3 stores to be processed
	if !enum.waitForCalls(3, 2*time.Second) {
		t.Fatal("Timed out waiting for initial snapshot generation")
	}
	cancel()
	<-done

	// Verify all stores had snapshots generated
	for _, storeID := range []string{"default", "project-a", "org/project-b"} {
		calls := enum.getSnapshotCalls(storeID)
		if calls < 1 {
			t.Errorf("Expected at least 1 GenerateSnapshot call for store %q, got %d", storeID, calls)
		}
	}
}

func TestSnapshotCoordinator_HandlesStoreErrorsGracefully(t *testing.T) {
	enum := newMockStoreEnumerator("store-a", "store-b", "store-c")
	// Make store-b fail
	enum.setStoreError("store-b", errors.New("disk full"))

	coord := NewSnapshotCoordinator(enum, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-a and store-c to be processed (store-b errors but is still "attempted")
	// We expect 2 successful calls (store-a and store-c)
	if !enum.waitForCalls(2, 2*time.Second) {
		t.Fatal("Timed out waiting for snapshot generation")
	}
	cancel()
	<-done

	// store-a and store-c should still be processed despite store-b error
	if calls := enum.getSnapshotCalls("store-a"); calls < 1 {
		t.Errorf("Expected store-a to be processed, got %d calls", calls)
	}
	if calls := enum.getSnapshotCalls("store-c"); calls < 1 {
		t.Errorf("Expected store-c to be processed despite store-b error, got %d calls", calls)
	}
}

func TestSnapshotCoordinator_RespectsContextCancellation(t *testing.T) {
	// Create many stores with slow snapshot generation
	storeIDs := make([]string, 10)
	for i := range storeIDs {
		storeIDs[i] = "store-" + string(rune('0'+i))
	}
	enum := newMockStoreEnumerator(storeIDs...)

	// Make each snapshot take 100ms
	for _, ms := range enum.getStores {
		ms.duration = 100 * time.Millisecond
	}

	coord := NewSnapshotCoordinator(enum, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for at least 1 store to start processing, then cancel
	if !enum.waitForCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for first store to be processed")
	}
	cancel()
	<-done

	duration := time.Since(startTime)

	// Should stop before processing all 10 stores (which would take ~1000ms)
	if duration > 500*time.Millisecond {
		t.Errorf("Coordinator did not respect context cancellation, took %v", duration)
	}

	// At least some stores should have been processed
	totalCalls := 0
	for _, storeID := range storeIDs {
		totalCalls += enum.getSnapshotCalls(storeID)
	}
	if totalCalls < 1 {
		t.Error("Expected at least one store to be processed before cancellation")
	}
	if totalCalls >= 10 {
		t.Errorf("Expected fewer than 10 stores processed due to cancellation, got %d", totalCalls)
	}
}

func TestSnapshotCoordinator_GeneratesOnInterval(t *testing.T) {
	enum := newMockStoreEnumerator("default")

	coord := NewSnapshotCoordinator(enum, 50*time.Millisecond) // Short interval

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for initial + 2 interval ticks (3 total calls)
	if !enum.waitForCalls(3, 2*time.Second) {
		t.Fatal("Timed out waiting for interval-based snapshot generation")
	}
	cancel()
	<-done

	// Should have initial + at least 2 interval calls
	calls := enum.getSnapshotCalls("default")
	if calls < 3 {
		t.Errorf("Expected at least 3 GenerateSnapshot calls (initial + 2 intervals), got %d", calls)
	}
}

func TestSnapshotCoordinator_HandleListStoresError(t *testing.T) {
	enum := newMockStoreEnumerator("default")
	enum.listErr = errors.New("failed to read directory")

	coord := NewSnapshotCoordinator(enum, 20*time.Millisecond) // Short interval

	// Use a timeout context - if list fails, no stores get processed
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	<-done

	// No stores should be processed due to list error
	calls := enum.getSnapshotCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 GenerateSnapshot calls due to list error, got %d", calls)
	}
}

func TestSnapshotCoordinator_HandleGetStoreError(t *testing.T) {
	enum := newMockStoreEnumerator("store-a", "store-b")
	enum.getErr["store-a"] = errors.New("store deleted")

	coord := NewSnapshotCoordinator(enum, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-b to be processed (store-a will fail to get)
	if !enum.waitForCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for store-b to be processed")
	}
	cancel()
	<-done

	// store-a should fail to get, but store-b should still be processed
	if calls := enum.getSnapshotCalls("store-a"); calls != 0 {
		t.Errorf("Expected store-a to have 0 calls (get failed), got %d", calls)
	}
	if calls := enum.getSnapshotCalls("store-b"); calls < 1 {
		t.Errorf("Expected store-b to be processed despite store-a get error, got %d calls", calls)
	}
}

// --- Integration Tests ---
// These tests use real StoreManager and SQLiteStores to verify
// the adapter correctly wires through to the underlying stores.

func TestStoreManagerAdapter_Integration_ListStores(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create multiple stores
	_, err = manager.GetStore(ctx, "default") // Auto-creates default
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "project-a", "Project A")
	if err != nil {
		t.Fatalf("CreateStore('project-a') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "org/project-b", "Nested project")
	if err != nil {
		t.Fatalf("CreateStore('org/project-b') error = %v", err)
	}

	// Create adapter
	adapter := NewStoreManagerAdapter(manager)

	// List stores through adapter
	stores, err := adapter.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	if len(stores) != 3 {
		t.Errorf("ListStores() returned %d stores, want 3", len(stores))
	}

	// Verify all store IDs are present
	found := make(map[string]bool)
	for _, s := range stores {
		found[s.ID] = true
	}

	for _, id := range []string{"default", "project-a", "org/project-b"} {
		if !found[id] {
			t.Errorf("ListStores should include %q", id)
		}
	}
}

func TestStoreManagerAdapter_Integration_GetStore(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create a store
	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	// Create adapter
	adapter := NewStoreManagerAdapter(manager)

	// Get store through adapter
	store, err := adapter.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("adapter.GetStore('default') error = %v", err)
	}

	if store == nil {
		t.Fatal("adapter.GetStore() should return a non-nil store")
	}

	// Verify the store implements SnapshotCapableStore (has GenerateSnapshot)
	// by checking we can call it (we'll test actual snapshot creation separately)
	_, ok := store.(SnapshotCapableStore)
	if !ok {
		t.Error("returned store should implement SnapshotCapableStore")
	}
}

func TestStoreManagerAdapter_Integration_GetStore_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	adapter := NewStoreManagerAdapter(manager)

	_, err = adapter.GetStore(ctx, "nonexistent")
	if !errors.Is(err, multistore.ErrStoreNotFound) {
		t.Errorf("adapter.GetStore('nonexistent') expected ErrStoreNotFound, got %v", err)
	}
}

func TestStoreManagerAdapter_Integration_SnapshotGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create multiple stores
	storeIDs := []string{"default", "project-a", "org/project-b"}
	for _, id := range storeIDs {
		if id == "default" {
			_, err = manager.GetStore(ctx, id)
		} else {
			_, err = manager.CreateStore(ctx, id, "Test store: "+id)
		}
		if err != nil {
			t.Fatalf("create store %q error = %v", id, err)
		}
	}

	// Create adapter
	adapter := NewStoreManagerAdapter(manager)

	// Generate snapshots for each store through the adapter
	for _, id := range storeIDs {
		store, err := adapter.GetStore(ctx, id)
		if err != nil {
			t.Fatalf("adapter.GetStore(%q) error = %v", id, err)
		}

		err = store.GenerateSnapshot(ctx)
		if err != nil {
			t.Fatalf("GenerateSnapshot for %q error = %v", id, err)
		}
	}

	// Verify snapshot files exist at the correct paths
	snapshotPaths := []string{
		filepath.Join(rootPath, "default", "snapshots", "current.db"),
		filepath.Join(rootPath, "project-a", "snapshots", "current.db"),
		filepath.Join(rootPath, "org", "project-b", "snapshots", "current.db"),
	}

	for i, path := range snapshotPaths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("snapshot for %q should exist at %s", storeIDs[i], path)
			continue
		}
		if err != nil {
			t.Errorf("stat snapshot for %q at %s: %v", storeIDs[i], path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("snapshot for %q should not be empty", storeIDs[i])
		}
	}
}

func TestStoreManagerAdapter_Integration_CoordinatorGeneratesAllSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create multiple stores
	storeIDs := []string{"default", "project-x"}
	for _, id := range storeIDs {
		if id == "default" {
			_, err = manager.GetStore(ctx, id)
		} else {
			_, err = manager.CreateStore(ctx, id, "Test store: "+id)
		}
		if err != nil {
			t.Fatalf("create store %q error = %v", id, err)
		}
	}

	// Build expected snapshot paths (store ID maps to filesystem path)
	snapshotPaths := make([]string, len(storeIDs))
	for i, id := range storeIDs {
		// Store IDs like "org/project" become "org/project" in filesystem
		snapshotPaths[i] = filepath.Join(rootPath, filepath.FromSlash(id), "snapshots", "current.db")
	}

	// Create coordinator with adapter
	adapter := NewStoreManagerAdapter(manager)
	coord := NewSnapshotCoordinator(adapter, 1*time.Hour)

	// Run coordinator briefly to generate initial snapshots
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		coord.Run(runCtx)
		close(done)
	}()

	// Wait for all snapshot files to be created (with timeout)
	if ok, missing := waitForSnapshotFiles(snapshotPaths, 5*time.Second); !ok {
		cancel()
		<-done
		t.Fatalf("Timed out waiting for snapshot files to be created. Missing files: %v", missing)
	}

	cancel()
	<-done

	// Verify all stores have non-empty snapshots
	for i, path := range snapshotPaths {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("snapshot for store %q should exist at %s: %v", storeIDs[i], path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("snapshot for store %q should not be empty", storeIDs[i])
		}
	}
}

// waitForSnapshotFiles polls for all files to exist within the timeout.
// Returns true if all files exist, false if timeout reached.
// On timeout, returns the list of missing file paths for debugging.
func waitForSnapshotFiles(paths []string, timeout time.Duration) (bool, []string) {
	deadline := time.After(timeout)
	for {
		var missing []string
		for _, path := range paths {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				missing = append(missing, path)
			}
		}
		if len(missing) == 0 {
			return true, nil
		}

		select {
		case <-deadline:
			return false, missing
		case <-time.After(10 * time.Millisecond):
			// Poll again
		}
	}
}
