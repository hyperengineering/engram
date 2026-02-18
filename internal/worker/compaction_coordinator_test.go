package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// mockCompactionCapableStore implements CompactionCapableStore for testing.
type mockCompactionCapableStore struct {
	mu             sync.Mutex
	compactCalls   int
	compactErr     error
	exported       int64
	deleted        int64
	lastCompSeq    int64
	lastCompTime   *time.Time
	setCompErr     error
}

func (m *mockCompactionCapableStore) CompactChangeLog(ctx context.Context, cutoff time.Time, auditDir string) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compactCalls++
	if m.compactErr != nil {
		return 0, 0, m.compactErr
	}
	return m.exported, m.deleted, nil
}

func (m *mockCompactionCapableStore) SetLastCompaction(ctx context.Context, sequence int64, timestamp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setCompErr != nil {
		return m.setCompErr
	}
	m.lastCompSeq = sequence
	m.lastCompTime = &timestamp
	return nil
}

func (m *mockCompactionCapableStore) getCompactCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.compactCalls
}

// mockCompactionStoreEnumerator implements CompactionStoreEnumerator for testing.
type mockCompactionStoreEnumerator struct {
	mu        sync.Mutex
	stores    []multistore.StoreInfo
	listErr   error
	getStores map[string]*mockCompactionCapableStore
	basePaths map[string]string
	getErr    map[string]error
}

func newMockCompactionStoreEnumerator(storeIDs ...string) *mockCompactionStoreEnumerator {
	m := &mockCompactionStoreEnumerator{
		stores:    make([]multistore.StoreInfo, 0, len(storeIDs)),
		getStores: make(map[string]*mockCompactionCapableStore),
		basePaths: make(map[string]string),
		getErr:    make(map[string]error),
	}
	for _, id := range storeIDs {
		m.stores = append(m.stores, multistore.StoreInfo{ID: id})
		m.getStores[id] = &mockCompactionCapableStore{exported: 5, deleted: 3}
		m.basePaths[id] = filepath.Join("/tmp/test-stores", id)
	}
	return m
}

func (m *mockCompactionStoreEnumerator) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.stores, nil
}

func (m *mockCompactionStoreEnumerator) GetCompactionStore(ctx context.Context, storeID string) (CompactionCapableStore, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.getErr[storeID]; ok && err != nil {
		return nil, "", err
	}
	if ms, ok := m.getStores[storeID]; ok {
		return ms, m.basePaths[storeID], nil
	}
	return nil, "", errors.New("store not found")
}

func (m *mockCompactionStoreEnumerator) getCompactCalls(storeID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		return ms.getCompactCalls()
	}
	return 0
}

func (m *mockCompactionStoreEnumerator) setStoreError(storeID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.getStores[storeID]; ok {
		ms.compactErr = err
	}
}

// waitForCompactCalls waits until totalCalls compaction operations have occurred.
func (m *mockCompactionStoreEnumerator) waitForCompactCalls(totalCalls int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		current := 0
		m.mu.Lock()
		for _, store := range m.getStores {
			current += store.getCompactCalls()
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

func TestCompactionCoordinator_IteratesAllStores(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("default", "project-a", "org/project-b")

	coord := NewCompactionCoordinator(enum, 50*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for all 3 stores to be processed
	if !enum.waitForCompactCalls(3, 2*time.Second) {
		t.Fatal("Timed out waiting for compaction to run on all stores")
	}
	cancel()
	<-done

	// Verify all stores had compaction run
	for _, storeID := range []string{"default", "project-a", "org/project-b"} {
		calls := enum.getCompactCalls(storeID)
		if calls < 1 {
			t.Errorf("Expected at least 1 CompactChangeLog call for store %q, got %d", storeID, calls)
		}
	}
}

func TestCompactionCoordinator_DoesNotRunImmediately(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("default")

	coord := NewCompactionCoordinator(enum, 1*time.Hour, 7*24*time.Hour)

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

	// No stores should be processed (compaction waits for first tick)
	calls := enum.getCompactCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 CompactChangeLog calls (should not run immediately), got %d", calls)
	}
}

func TestCompactionCoordinator_StopsOnCancel(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("default")

	coord := NewCompactionCoordinator(enum, 20*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	duration := time.Since(startTime)
	if duration > 500*time.Millisecond {
		t.Errorf("Coordinator did not respect context cancellation, took %v", duration)
	}
}

func TestCompactionCoordinator_ContinuesOnError(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("store-a", "store-b", "store-c")
	// Make store-b fail
	enum.setStoreError("store-b", errors.New("disk full"))

	coord := NewCompactionCoordinator(enum, 50*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-a and store-c to be processed
	if !enum.waitForCompactCalls(2, 2*time.Second) {
		t.Fatal("Timed out waiting for compaction")
	}
	cancel()
	<-done

	// store-a and store-c should still be processed despite store-b error
	if calls := enum.getCompactCalls("store-a"); calls < 1 {
		t.Errorf("Expected store-a to be processed, got %d calls", calls)
	}
	if calls := enum.getCompactCalls("store-c"); calls < 1 {
		t.Errorf("Expected store-c to be processed despite store-b error, got %d calls", calls)
	}
}

func TestCompactionCoordinator_HandleListStoresError(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("default")
	enum.listErr = errors.New("failed to read directory")

	coord := NewCompactionCoordinator(enum, 20*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	<-done

	// No stores should be processed due to list error
	calls := enum.getCompactCalls("default")
	if calls != 0 {
		t.Errorf("Expected 0 CompactChangeLog calls due to list error, got %d", calls)
	}
}

func TestCompactionCoordinator_HandleGetStoreError(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("store-a", "store-b")
	enum.getErr["store-a"] = errors.New("store deleted")

	coord := NewCompactionCoordinator(enum, 50*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	// Wait for store-b to be processed
	if !enum.waitForCompactCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for store-b to be processed")
	}
	cancel()
	<-done

	// store-a should fail to get, but store-b should still be processed
	if calls := enum.getCompactCalls("store-a"); calls != 0 {
		t.Errorf("Expected store-a to have 0 calls (get failed), got %d", calls)
	}
	if calls := enum.getCompactCalls("store-b"); calls < 1 {
		t.Errorf("Expected store-b to be processed despite store-a get error, got %d calls", calls)
	}
}

func TestCompactionCoordinator_SkipsWhenNothingToCompact(t *testing.T) {
	enum := newMockCompactionStoreEnumerator("default")
	// Return 0, 0 (nothing to compact)
	enum.getStores["default"].exported = 0
	enum.getStores["default"].deleted = 0

	coord := NewCompactionCoordinator(enum, 50*time.Millisecond, 7*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		coord.Run(ctx)
		close(done)
	}()

	if !enum.waitForCompactCalls(1, 2*time.Second) {
		t.Fatal("Timed out waiting for compaction")
	}
	cancel()
	<-done

	// Should have been called but nothing to do
	calls := enum.getCompactCalls("default")
	if calls < 1 {
		t.Errorf("Expected at least 1 call, got %d", calls)
	}
}

// --- Integration Tests ---
// These use real StoreManager and SQLiteStores.

func TestCompaction_EndToEnd(t *testing.T) {
	// Given: A store with old entries (3 versions of same entity)
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	// Insert entries with old timestamps
	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		_, err := managed.Store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-1",
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{"v":` + string(rune('0'+i)) + `}`),
			SourceID:  "src-1",
			CreatedAt: oldTime.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("AppendChangeLog failed: %v", err)
		}
	}

	// When: Run compaction via adapter
	adapter := NewCompactionStoreManagerAdapter(manager)
	store, basePath, err := adapter.GetCompactionStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetCompactionStore failed: %v", err)
	}

	cutoff := time.Now().UTC()
	auditDir := filepath.Join(basePath, "audit")
	exported, deleted, err := store.CompactChangeLog(ctx, cutoff, auditDir)
	if err != nil {
		t.Fatalf("CompactChangeLog failed: %v", err)
	}

	// Then: 2 entries exported/deleted, 1 remains (latest)
	if exported != 2 {
		t.Errorf("expected 2 exported, got %d", exported)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// Delta still works — only latest entry visible
	entries, err := managed.Store.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 remaining entry, got %d", len(entries))
	}
}

func TestCompaction_AuditRecovers(t *testing.T) {
	// Given: Entries that get compacted
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 5; i++ {
		_, err := managed.Store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-1",
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{"v":` + string(rune('0'+i)) + `}`),
			SourceID:  "src-1",
			CreatedAt: oldTime.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("AppendChangeLog failed: %v", err)
		}
	}

	// When: Compact
	adapter := NewCompactionStoreManagerAdapter(manager)
	store, basePath, err := adapter.GetCompactionStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetCompactionStore failed: %v", err)
	}

	auditDir := filepath.Join(basePath, "audit")
	exported, _, err := store.CompactChangeLog(ctx, time.Now().UTC(), auditDir)
	if err != nil {
		t.Fatalf("CompactChangeLog failed: %v", err)
	}

	// Then: Audit file contains all deleted entries
	dateStr := time.Now().UTC().Format("2006-01-02")
	auditFile := filepath.Join(auditDir, dateStr+".jsonl")

	data, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if int64(len(lines)) != exported {
		t.Errorf("expected %d audit entries, got %d", exported, len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("audit line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestCompaction_DeltaStillWorks(t *testing.T) {
	// Given: Multiple entities, some compacted
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	managed, err := manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	oldTime := time.Now().UTC().Add(-48 * time.Hour)

	// Entity-1: 3 old versions
	for i := 0; i < 3; i++ {
		_, _ = managed.Store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-1",
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{"v":` + string(rune('0'+i)) + `}`),
			SourceID:  "src-1",
			CreatedAt: oldTime.Add(time.Duration(i) * time.Minute),
		})
	}

	// Entity-2: 1 recent entry (should NOT be compacted)
	_, _ = managed.Store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-2",
		Operation: engramsync.OperationUpsert,
		Payload:   json.RawMessage(`{"recent":true}`),
		SourceID:  "src-1",
		CreatedAt: time.Now().UTC(),
	})

	// When: Compact with cutoff between old and recent
	adapter := NewCompactionStoreManagerAdapter(manager)
	store, basePath, err := adapter.GetCompactionStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetCompactionStore failed: %v", err)
	}

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	auditDir := filepath.Join(basePath, "audit")
	_, _, err = store.CompactChangeLog(ctx, cutoff, auditDir)
	if err != nil {
		t.Fatalf("CompactChangeLog failed: %v", err)
	}

	// Then: Delta from sequence 0 returns remaining entries
	entries, err := managed.Store.GetChangeLogAfter(ctx, 0, 100)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}

	// Should have: latest of entity-1 + entity-2's recent entry
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after compaction, got %d", len(entries))
	}

	// Verify entity IDs
	entityIDs := make(map[string]bool)
	for _, e := range entries {
		entityIDs[e.EntityID] = true
	}
	if !entityIDs["entity-1"] {
		t.Error("entity-1 latest should remain after compaction")
	}
	if !entityIDs["entity-2"] {
		t.Error("entity-2 (recent) should not be compacted")
	}
}

func TestCompactionStoreManagerAdapter_Integration_ListStores(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}
	_, err = manager.CreateStore(ctx, "project-a", "", "Project A")
	if err != nil {
		t.Fatalf("CreateStore('project-a') error = %v", err)
	}

	adapter := NewCompactionStoreManagerAdapter(manager)

	stores, err := adapter.ListStores(ctx)
	if err != nil {
		t.Fatalf("ListStores() error = %v", err)
	}

	if len(stores) != 2 {
		t.Errorf("ListStores() returned %d stores, want 2", len(stores))
	}
}

func TestCompactionStoreManagerAdapter_Integration_GetCompactionStore(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	_, err = manager.GetStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetStore('default') error = %v", err)
	}

	adapter := NewCompactionStoreManagerAdapter(manager)

	store, basePath, err := adapter.GetCompactionStore(ctx, "default")
	if err != nil {
		t.Fatalf("GetCompactionStore('default') error = %v", err)
	}

	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if basePath == "" {
		t.Fatal("expected non-empty basePath")
	}
}

func TestCompactionStoreManagerAdapter_Integration_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	rootPath := filepath.Join(tmpDir, "stores")

	manager, err := multistore.NewStoreManager(rootPath)
	if err != nil {
		t.Fatalf("NewStoreManager() error = %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	adapter := NewCompactionStoreManagerAdapter(manager)

	_, _, err = adapter.GetCompactionStore(ctx, "nonexistent")
	if !errors.Is(err, multistore.ErrStoreNotFound) {
		t.Errorf("expected ErrStoreNotFound, got %v", err)
	}
}
