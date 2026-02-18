package worker

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/types"
)

// mockEmbeddingCapableStore implements EmbeddingCapableStore for testing.
type mockEmbeddingCapableStore struct {
	mu                 sync.Mutex
	pendingEntries     []types.LoreEntry
	pendingErr         error
	updateCalls        int
	updateErr          error
	markFailedCalls    int
	markFailedErr      error
	updatedIDs         []string
	failedIDs          []string
}

func (m *mockEmbeddingCapableStore) GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendingErr != nil {
		return nil, m.pendingErr
	}
	if limit > len(m.pendingEntries) {
		limit = len(m.pendingEntries)
	}
	return m.pendingEntries[:limit], nil
}

func (m *mockEmbeddingCapableStore) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls++
	m.updatedIDs = append(m.updatedIDs, id)
	if m.updateErr != nil {
		return m.updateErr
	}
	// Remove from pending after successful update
	var remaining []types.LoreEntry
	for _, e := range m.pendingEntries {
		if e.ID != id {
			remaining = append(remaining, e)
		}
	}
	m.pendingEntries = remaining
	return nil
}

func (m *mockEmbeddingCapableStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markFailedCalls++
	m.failedIDs = append(m.failedIDs, id)
	if m.markFailedErr != nil {
		return m.markFailedErr
	}
	// Remove from pending
	var remaining []types.LoreEntry
	for _, e := range m.pendingEntries {
		if e.ID != id {
			remaining = append(remaining, e)
		}
	}
	m.pendingEntries = remaining
	return nil
}

func (m *mockEmbeddingCapableStore) getUpdateCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateCalls
}

func (m *mockEmbeddingCapableStore) getMarkFailedCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.markFailedCalls
}

func (m *mockEmbeddingCapableStore) getUpdatedIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.updatedIDs...)
}

// mockEmbeddingStoreEnumerator implements EmbeddingStoreEnumerator for testing.
type mockEmbeddingStoreEnumerator struct {
	mu        sync.Mutex
	stores    []multistore.StoreInfo
	listErr   error
	getStores map[string]*mockEmbeddingCapableStore
	getErr    map[string]error
}

func newMockEmbeddingStoreEnumerator(storeIDs ...string) *mockEmbeddingStoreEnumerator {
	m := &mockEmbeddingStoreEnumerator{
		stores:    make([]multistore.StoreInfo, 0, len(storeIDs)),
		getStores: make(map[string]*mockEmbeddingCapableStore),
		getErr:    make(map[string]error),
	}
	for _, id := range storeIDs {
		m.stores = append(m.stores, multistore.StoreInfo{ID: id})
		m.getStores[id] = &mockEmbeddingCapableStore{}
	}
	return m
}

func (m *mockEmbeddingStoreEnumerator) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.stores, nil
}

func (m *mockEmbeddingStoreEnumerator) GetEmbeddingStore(ctx context.Context, storeID string) (EmbeddingCapableStore, error) {
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

func (m *mockEmbeddingStoreEnumerator) addPendingEntries(storeID string, entries ...types.LoreEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if store, ok := m.getStores[storeID]; ok {
		store.mu.Lock()
		store.pendingEntries = append(store.pendingEntries, entries...)
		store.mu.Unlock()
	}
}

func (m *mockEmbeddingStoreEnumerator) getUpdateCalls(storeID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if store, ok := m.getStores[storeID]; ok {
		return store.getUpdateCalls()
	}
	return 0
}

func (m *mockEmbeddingStoreEnumerator) waitForUpdates(totalUpdates int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		current := 0
		m.mu.Lock()
		for _, store := range m.getStores {
			current += store.getUpdateCalls()
		}
		m.mu.Unlock()

		if current >= totalUpdates {
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

// mockCoordinatorEmbedder implements Embedder for coordinator tests.
type mockCoordinatorEmbedder struct {
	mu       sync.Mutex
	calls    int
	err      error
	embedDim int
}

func newMockCoordinatorEmbedder() *mockCoordinatorEmbedder {
	return &mockCoordinatorEmbedder{embedDim: 128}
}

func (m *mockCoordinatorEmbedder) EmbedBatch(ctx context.Context, contents []string) ([][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	result := make([][]float32, len(contents))
	for i := range result {
		result[i] = make([]float32, m.embedDim)
		result[i][0] = float32(i + 1) // Just set first element for identification
	}
	return result, nil
}

func (m *mockCoordinatorEmbedder) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// --- Tests ---

func TestEmbeddingRetryCoordinator_IteratesAllStores(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("default", "project-a", "org/project-b")
	embedder := newMockCoordinatorEmbedder()

	// Add pending entries to each store
	enum.addPendingEntries("default", types.LoreEntry{ID: "1", Content: "test1"})
	enum.addPendingEntries("project-a", types.LoreEntry{ID: "2", Content: "test2"})
	enum.addPendingEntries("org/project-b", types.LoreEntry{ID: "3", Content: "test3"})

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for all 3 stores to have updates (runs immediately)
	if !enum.waitForUpdates(3, 2*time.Second) {
		t.Fatal("Timed out waiting for embedding updates on all stores")
	}
	cancel()
	<-done

	// Verify all stores had embeddings updated
	for _, storeID := range []string{"default", "project-a", "org/project-b"} {
		calls := enum.getUpdateCalls(storeID)
		if calls < 1 {
			t.Errorf("Expected at least 1 UpdateEmbedding call for store %q, got %d", storeID, calls)
		}
	}
}

func TestEmbeddingRetryCoordinator_RunsImmediately(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("default")
	embedder := newMockCoordinatorEmbedder()

	// Add pending entry
	enum.addPendingEntries("default", types.LoreEntry{ID: "1", Content: "test1"})

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 1*time.Hour, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait briefly - should have processed immediately
	if !enum.waitForUpdates(1, 500*time.Millisecond) {
		t.Fatal("Embedding retry should run immediately on start")
	}
	cancel()
	<-done

	// Verify embedding was updated
	calls := enum.getUpdateCalls("default")
	if calls != 1 {
		t.Errorf("Expected 1 UpdateEmbedding call (runs immediately), got %d", calls)
	}
}

func TestEmbeddingRetryCoordinator_HandlesStoreErrorsGracefully(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("store-a", "store-b", "store-c")
	embedder := newMockCoordinatorEmbedder()

	// Add entries to all stores
	enum.addPendingEntries("store-a", types.LoreEntry{ID: "1", Content: "test1"})
	enum.addPendingEntries("store-b", types.LoreEntry{ID: "2", Content: "test2"})
	enum.addPendingEntries("store-c", types.LoreEntry{ID: "3", Content: "test3"})

	// Make store-b fail on GetPendingEmbeddings
	enum.mu.Lock()
	enum.getStores["store-b"].pendingErr = errors.New("disk full")
	enum.mu.Unlock()

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-a and store-c to be processed
	if !enum.waitForUpdates(2, 2*time.Second) {
		t.Fatal("Timed out waiting for updates")
	}
	cancel()
	<-done

	// store-a and store-c should still be processed despite store-b error
	if calls := enum.getUpdateCalls("store-a"); calls < 1 {
		t.Errorf("Expected store-a to be processed, got %d calls", calls)
	}
	if calls := enum.getUpdateCalls("store-c"); calls < 1 {
		t.Errorf("Expected store-c to be processed despite store-b error, got %d calls", calls)
	}
}

func TestEmbeddingRetryCoordinator_RespectsContextCancellation(t *testing.T) {
	// Create many stores
	storeIDs := make([]string, 10)
	for i := range storeIDs {
		storeIDs[i] = "store-" + string(rune('0'+i))
	}
	enum := newMockEmbeddingStoreEnumerator(storeIDs...)
	embedder := newMockCoordinatorEmbedder()

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 20*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Cancel quickly
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	duration := time.Since(startTime)

	// Should stop quickly
	if duration > 500*time.Millisecond {
		t.Errorf("Coordinator did not respect context cancellation, took %v", duration)
	}
}

func TestEmbeddingRetryCoordinator_HandleListStoresError(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("default")
	enum.listErr = errors.New("failed to read directory")
	embedder := newMockCoordinatorEmbedder()

	// Add entry
	enum.addPendingEntries("default", types.LoreEntry{ID: "1", Content: "test1"})

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 20*time.Millisecond, 3, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	<-done

	// No stores should be processed due to list error
	calls := enum.getUpdateCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 UpdateEmbedding calls due to list error, got %d", calls)
	}
}

func TestEmbeddingRetryCoordinator_HandleGetStoreError(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("store-a", "store-b")
	embedder := newMockCoordinatorEmbedder()

	// Add entries
	enum.addPendingEntries("store-a", types.LoreEntry{ID: "1", Content: "test1"})
	enum.addPendingEntries("store-b", types.LoreEntry{ID: "2", Content: "test2"})

	// Make getting store-a fail
	enum.getErr["store-a"] = errors.New("store deleted")

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-b to be processed
	if !enum.waitForUpdates(1, 2*time.Second) {
		t.Fatal("Timed out waiting for store-b to be processed")
	}
	cancel()
	<-done

	// store-a should fail to get, but store-b should still be processed
	if calls := enum.getUpdateCalls("store-a"); calls != 0 {
		t.Errorf("Expected store-a to have 0 calls (get failed), got %d", calls)
	}
	if calls := enum.getUpdateCalls("store-b"); calls < 1 {
		t.Errorf("Expected store-b to be processed despite store-a get error, got %d calls", calls)
	}
}

func TestEmbeddingRetryCoordinator_MarksFailedAfterMaxAttempts(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("default")
	embedder := newMockCoordinatorEmbedder()
	embedder.err = errors.New("embedding service unavailable")

	// Add entry
	enum.addPendingEntries("default", types.LoreEntry{ID: "1", Content: "test1"})

	// Use maxAttempts=2 so it fails after 2 attempts
	coord := NewEmbeddingRetryCoordinator(enum, embedder, 30*time.Millisecond, 2, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for multiple cycles to accumulate failures
	time.Sleep(150 * time.Millisecond)

	// Clear error so next cycle can mark as failed
	embedder.mu.Lock()
	embedder.err = nil
	embedder.mu.Unlock()

	// Wait for the mark failed call
	deadline := time.After(2 * time.Second)
	for {
		enum.mu.Lock()
		store := enum.getStores["default"]
		failedCalls := store.getMarkFailedCalls()
		enum.mu.Unlock()

		if failedCalls >= 1 {
			break
		}

		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("Timed out waiting for MarkEmbeddingFailed to be called")
		case <-time.After(10 * time.Millisecond):
			// Poll again
		}
	}

	cancel()
	<-done

	// Verify entry was marked as failed
	enum.mu.Lock()
	store := enum.getStores["default"]
	enum.mu.Unlock()

	if calls := store.getMarkFailedCalls(); calls < 1 {
		t.Errorf("Expected at least 1 MarkEmbeddingFailed call, got %d", calls)
	}
}

func TestEmbeddingRetryCoordinator_IsolatesRetryCountsPerStore(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("store-a", "store-b")
	embedder := newMockCoordinatorEmbedder()

	// Add entries with same ID to different stores
	enum.addPendingEntries("store-a", types.LoreEntry{ID: "shared-id", Content: "test1"})
	enum.addPendingEntries("store-b", types.LoreEntry{ID: "shared-id", Content: "test2"})

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for both stores to be processed
	if !enum.waitForUpdates(2, 2*time.Second) {
		t.Fatal("Timed out waiting for updates")
	}
	cancel()
	<-done

	// Both stores should have had their entries processed independently
	if calls := enum.getUpdateCalls("store-a"); calls < 1 {
		t.Errorf("Expected store-a to be processed, got %d calls", calls)
	}
	if calls := enum.getUpdateCalls("store-b"); calls < 1 {
		t.Errorf("Expected store-b to be processed, got %d calls", calls)
	}
}

func TestEmbeddingRetryCoordinator_NoWorkIsSuccess(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("default")
	embedder := newMockCoordinatorEmbedder()

	// Don't add any pending entries - store is empty

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 3, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait briefly then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	// Should have no updates or errors (no work to do is fine)
	calls := enum.getUpdateCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 UpdateEmbedding calls (no pending entries), got %d", calls)
	}
}

func TestEmbeddingRetryCoordinator_CleansUpOrphanedRetryCounts(t *testing.T) {
	enum := newMockEmbeddingStoreEnumerator("store-a", "store-b")
	embedder := newMockCoordinatorEmbedder()
	embedder.err = errors.New("embedding service unavailable")

	// Add entries to both stores
	enum.addPendingEntries("store-a", types.LoreEntry{ID: "1", Content: "test1"})
	enum.addPendingEntries("store-b", types.LoreEntry{ID: "2", Content: "test2"})

	coord := NewEmbeddingRetryCoordinator(enum, embedder, 50*time.Millisecond, 5, 10)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for retries to accumulate
	time.Sleep(100 * time.Millisecond)

	// Verify retry counts exist for both stores
	coord.mu.Lock()
	hasStoreA := coord.retryCount["store-a"] != nil
	hasStoreB := coord.retryCount["store-b"] != nil
	coord.mu.Unlock()

	if !hasStoreA || !hasStoreB {
		t.Log("Retry counts not yet accumulated, waiting longer...")
		time.Sleep(100 * time.Millisecond)
	}

	// Simulate store-b being deleted by removing it from the enumerator
	enum.mu.Lock()
	enum.stores = []multistore.StoreInfo{{ID: "store-a"}}
	delete(enum.getStores, "store-b")
	enum.mu.Unlock()

	// Wait for another cycle to trigger cleanup
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	// Verify store-b retry counts were cleaned up
	coord.mu.Lock()
	_, storeAExists := coord.retryCount["store-a"]
	_, storeBExists := coord.retryCount["store-b"]
	coord.mu.Unlock()

	// store-a should still have retry counts (it's still active)
	// store-b should be cleaned up (it was removed from active stores)
	if storeBExists {
		t.Error("Expected store-b retry counts to be cleaned up after store deletion")
	}
	// Note: store-a may or may not have counts depending on timing, so we don't assert on it
	_ = storeAExists
}

// --- Integration Tests ---
// These tests use real StoreManager and SQLiteStores to verify
// the adapter correctly wires through to the underlying stores.

func TestEmbeddingStoreManagerAdapter_Integration_ListStores(t *testing.T) {
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
	adapter := NewEmbeddingStoreManagerAdapter(manager)

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

func TestEmbeddingStoreManagerAdapter_Integration_GetEmbeddingStore(t *testing.T) {
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
	adapter := NewEmbeddingStoreManagerAdapter(manager)

	// Get store through adapter
	store, err := adapter.GetEmbeddingStore(ctx, "default")
	if err != nil {
		t.Fatalf("adapter.GetEmbeddingStore('default') error = %v", err)
	}

	if store == nil {
		t.Fatal("adapter.GetEmbeddingStore() should return a non-nil store")
	}

	// Verify the store implements EmbeddingCapableStore
	_, ok := store.(EmbeddingCapableStore)
	if !ok {
		t.Error("returned store should implement EmbeddingCapableStore")
	}
}

func TestEmbeddingStoreManagerAdapter_Integration_GetEmbeddingStore_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	adapter := NewEmbeddingStoreManagerAdapter(manager)

	_, err = adapter.GetEmbeddingStore(ctx, "nonexistent")
	if !errors.Is(err, multistore.ErrStoreNotFound) {
		t.Errorf("adapter.GetEmbeddingStore('nonexistent') expected ErrStoreNotFound, got %v", err)
	}
}

func TestEmbeddingStoreManagerAdapter_Integration_EmbeddingOperations(t *testing.T) {
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
	adapter := NewEmbeddingStoreManagerAdapter(manager)

	// Verify operations work for each store through the adapter
	for _, id := range storeIDs {
		store, err := adapter.GetEmbeddingStore(ctx, id)
		if err != nil {
			t.Fatalf("adapter.GetEmbeddingStore(%q) error = %v", id, err)
		}

		// GetPendingEmbeddings should succeed (even with no entries)
		entries, err := store.GetPendingEmbeddings(ctx, 10)
		if err != nil {
			t.Fatalf("GetPendingEmbeddings for %q error = %v", id, err)
		}

		// With no entries, should return empty slice
		if len(entries) != 0 {
			t.Errorf("Expected 0 pending entries for empty store %q, got %d", id, len(entries))
		}
	}
}
