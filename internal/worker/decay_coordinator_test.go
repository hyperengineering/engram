package worker

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
)

// mockDecayCapableStore implements store operations for decay coordinator tests.
type mockDecayCapableStore struct {
	mu          sync.Mutex
	decayCalls  int
	decayErr    error
	affected    int64
	lastDecay   *time.Time
}

func (m *mockDecayCapableStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decayCalls++
	if m.decayErr != nil {
		return 0, m.decayErr
	}
	return m.affected, nil
}

func (m *mockDecayCapableStore) SetLastDecay(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastDecay = &t
}

func (m *mockDecayCapableStore) getDecayCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decayCalls
}

func (m *mockDecayCapableStore) getLastDecay() *time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastDecay
}

// mockDecayStoreEnumerator implements DecayStoreEnumerator for testing.
type mockDecayStoreEnumerator struct {
	mu        sync.Mutex
	stores    []multistore.StoreInfo
	listErr   error
	getStores map[string]*mockDecayCapableStore
	getErr    map[string]error
}

func newMockDecayStoreEnumerator(storeIDs ...string) *mockDecayStoreEnumerator {
	m := &mockDecayStoreEnumerator{
		stores:    make([]multistore.StoreInfo, 0, len(storeIDs)),
		getStores: make(map[string]*mockDecayCapableStore),
		getErr:    make(map[string]error),
	}
	for _, id := range storeIDs {
		m.stores = append(m.stores, multistore.StoreInfo{ID: id})
		m.getStores[id] = &mockDecayCapableStore{affected: 5}
	}
	return m
}

func (m *mockDecayStoreEnumerator) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.stores, nil
}

func (m *mockDecayStoreEnumerator) GetDecayStore(ctx context.Context, storeID string) (DecayCapableStore, error) {
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

func (m *mockDecayStoreEnumerator) getDecayCalls(storeID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		return ms.getDecayCalls()
	}
	return 0
}

func (m *mockDecayStoreEnumerator) setStoreError(storeID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		ms.decayErr = err
	}
}

// waitForDecayCalls waits until totalCalls decay operations have occurred across all stores.
func (m *mockDecayStoreEnumerator) waitForDecayCalls(totalCalls int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		current := 0
		m.mu.Lock()
		for _, store := range m.getStores {
			current += store.getDecayCalls()
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

// --- Tests ---

func TestDecayCoordinator_IteratesAllStores(t *testing.T) {
	enum := newMockDecayStoreEnumerator("default", "project-a", "org/project-b")

	coord := NewDecayCoordinator(enum, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for all 3 stores to be processed (decay does NOT run immediately)
	if !enum.waitForDecayCalls(3, 2*time.Second) {
		t.Fatal("Timed out waiting for decay to run on all stores")
	}
	cancel()
	<-done

	// Verify all stores had decay run
	for _, storeID := range []string{"default", "project-a", "org/project-b"} {
		calls := enum.getDecayCalls(storeID)
		if calls < 1 {
			t.Errorf("Expected at least 1 DecayConfidence call for store %q, got %d", storeID, calls)
		}
	}
}

func TestDecayCoordinator_DoesNotRunImmediately(t *testing.T) {
	enum := newMockDecayStoreEnumerator("default")

	coord := NewDecayCoordinator(enum, 1*time.Hour, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait briefly then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// No stores should be processed (decay waits for first tick)
	calls := enum.getDecayCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 DecayConfidence calls (should not run immediately), got %d", calls)
	}
}

func TestDecayCoordinator_HandlesStoreErrorsGracefully(t *testing.T) {
	enum := newMockDecayStoreEnumerator("store-a", "store-b", "store-c")
	// Make store-b fail
	enum.setStoreError("store-b", errors.New("disk full"))

	coord := NewDecayCoordinator(enum, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-a and store-c to be processed
	if !enum.waitForDecayCalls(2, 2*time.Second) {
		t.Fatal("Timed out waiting for decay")
	}
	cancel()
	<-done

	// store-a and store-c should still be processed despite store-b error
	if calls := enum.getDecayCalls("store-a"); calls < 1 {
		t.Errorf("Expected store-a to be processed, got %d calls", calls)
	}
	if calls := enum.getDecayCalls("store-c"); calls < 1 {
		t.Errorf("Expected store-c to be processed despite store-b error, got %d calls", calls)
	}
}

func TestDecayCoordinator_RespectsContextCancellation(t *testing.T) {
	// Create many stores with slow decay
	storeIDs := make([]string, 10)
	for i := range storeIDs {
		storeIDs[i] = "store-" + string(rune('0'+i))
	}
	enum := newMockDecayStoreEnumerator(storeIDs...)

	coord := NewDecayCoordinator(enum, 20*time.Millisecond, 0.01) // Short interval

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for first tick to start, then cancel quickly
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	duration := time.Since(startTime)

	// Should stop quickly
	if duration > 500*time.Millisecond {
		t.Errorf("Coordinator did not respect context cancellation, took %v", duration)
	}
}

func TestDecayCoordinator_UpdatesLastDecayTimestamp(t *testing.T) {
	enum := newMockDecayStoreEnumerator("default")

	coord := NewDecayCoordinator(enum, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for decay to run
	if !enum.waitForDecayCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for decay")
	}
	cancel()
	<-done

	// Verify lastDecay was set
	enum.mu.Lock()
	store := enum.getStores["default"]
	enum.mu.Unlock()

	lastDecay := store.getLastDecay()
	if lastDecay == nil {
		t.Error("Expected lastDecay to be set after decay runs")
	}
}

func TestDecayCoordinator_HandleListStoresError(t *testing.T) {
	enum := newMockDecayStoreEnumerator("default")
	enum.listErr = errors.New("failed to read directory")

	coord := NewDecayCoordinator(enum, 20*time.Millisecond, 0.01)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	<-done

	// No stores should be processed due to list error
	calls := enum.getDecayCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 DecayConfidence calls due to list error, got %d", calls)
	}
}

func TestDecayCoordinator_HandleGetStoreError(t *testing.T) {
	enum := newMockDecayStoreEnumerator("store-a", "store-b")
	enum.getErr["store-a"] = errors.New("store deleted")

	coord := NewDecayCoordinator(enum, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-b to be processed
	if !enum.waitForDecayCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for store-b to be processed")
	}
	cancel()
	<-done

	// store-a should fail to get, but store-b should still be processed
	if calls := enum.getDecayCalls("store-a"); calls != 0 {
		t.Errorf("Expected store-a to have 0 calls (get failed), got %d", calls)
	}
	if calls := enum.getDecayCalls("store-b"); calls < 1 {
		t.Errorf("Expected store-b to be processed despite store-a get error, got %d calls", calls)
	}
}

// --- Integration Tests ---
// These tests use real StoreManager and SQLiteStores to verify
// the adapter correctly wires through to the underlying stores.

func TestDecayStoreManagerAdapter_Integration_ListStores(t *testing.T) {
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
	_, err = manager.CreateStore(ctx, "project-a", "", "Project A")
	if err != nil {
		t.Fatalf("CreateStore('project-a') error = %v", err)
	}

	// Create adapter
	adapter := NewDecayStoreManagerAdapter(manager)

	// List stores through adapter
	stores, err := adapter.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	if len(stores) != 2 {
		t.Errorf("ListStores() returned %d stores, want 2", len(stores))
	}

	// Verify all store IDs are present
	found := make(map[string]bool)
	for _, s := range stores {
		found[s.ID] = true
	}

	for _, id := range []string{"default", "project-a"} {
		if !found[id] {
			t.Errorf("ListStores should include %q", id)
		}
	}
}

func TestDecayStoreManagerAdapter_Integration_GetDecayStore(t *testing.T) {
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
	adapter := NewDecayStoreManagerAdapter(manager)

	// Get store through adapter
	store, err := adapter.GetDecayStore(ctx, "default")
	if err != nil {
		t.Fatalf("adapter.GetDecayStore('default') error = %v", err)
	}

	if store == nil {
		t.Fatal("adapter.GetDecayStore() should return a non-nil store")
	}

	// Verify the store implements DecayCapableStore
	_, ok := store.(DecayCapableStore)
	if !ok {
		t.Error("returned store should implement DecayCapableStore")
	}
}

func TestDecayStoreManagerAdapter_Integration_GetDecayStore_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	adapter := NewDecayStoreManagerAdapter(manager)

	_, err = adapter.GetDecayStore(ctx, "nonexistent")
	if !errors.Is(err, multistore.ErrStoreNotFound) {
		t.Errorf("adapter.GetDecayStore('nonexistent') expected ErrStoreNotFound, got %v", err)
	}
}

func TestDecayStoreManagerAdapter_Integration_DecayExecution(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Create multiple stores
	storeIDs := []string{"default", "project-a"}
	for _, id := range storeIDs {
		if id == "default" {
			_, err = manager.GetStore(ctx, id)
		} else {
			_, err = manager.CreateStore(ctx, id, "", "Test store: "+id)
		}
		if err != nil {
			t.Fatalf("create store %q error = %v", id, err)
		}
	}

	// Create adapter
	adapter := NewDecayStoreManagerAdapter(manager)

	// Run decay for each store through the adapter
	for _, id := range storeIDs {
		store, err := adapter.GetDecayStore(ctx, id)
		if err != nil {
			t.Fatalf("adapter.GetDecayStore(%q) error = %v", id, err)
		}

		// DecayConfidence should succeed (even with no entries to decay)
		threshold := time.Now().Add(-24 * time.Hour)
		affected, err := store.DecayConfidence(ctx, threshold, 0.01)
		if err != nil {
			t.Fatalf("DecayConfidence for %q error = %v", id, err)
		}

		// With no entries, affected should be 0
		if affected != 0 {
			t.Errorf("Expected 0 affected entries for empty store %q, got %d", id, affected)
		}
	}
}

func TestDecayStoreManagerAdapter_Integration_CoordinatorDecaysAllStores(t *testing.T) {
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
			_, err = manager.CreateStore(ctx, id, "", "Test store: "+id)
		}
		if err != nil {
			t.Fatalf("create store %q error = %v", id, err)
		}
	}

	// Create coordinator with adapter
	adapter := NewDecayStoreManagerAdapter(manager)
	coord := NewDecayCoordinator(adapter, 50*time.Millisecond, 0.01)

	// Run coordinator briefly to run decay cycle
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		coord.Run(runCtx)
		close(done)
	}()

	// Wait for decay to complete (needs one tick + processing)
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	// Verify all stores have lastDecay set
	for _, id := range storeIDs {
		managed, err := manager.GetStore(ctx, id)
		if err != nil {
			t.Errorf("GetStore(%q) error = %v", id, err)
			continue
		}
		lastDecay := managed.Store.GetLastDecay()
		if lastDecay == nil {
			t.Errorf("store %q should have lastDecay set after coordinator run", id)
		}
	}
}
