package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperengineering/engram/internal/types"
)

// --- Mock Implementations ---

type mockStore struct {
	mu                   sync.Mutex
	pendingEntries       []types.LoreEntry
	getPendingErr        error
	updateEmbeddingErr   error
	markFailedErr        error
	updateEmbeddingCalls []string // IDs that had UpdateEmbedding called
	markFailedCalls      []string // IDs that had MarkEmbeddingFailed called
}

func (m *mockStore) GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getPendingErr != nil {
		return nil, m.getPendingErr
	}
	if limit > len(m.pendingEntries) {
		limit = len(m.pendingEntries)
	}
	return m.pendingEntries[:limit], nil
}

func (m *mockStore) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateEmbeddingErr != nil {
		return m.updateEmbeddingErr
	}
	m.updateEmbeddingCalls = append(m.updateEmbeddingCalls, id)
	// Remove from pending
	for i, e := range m.pendingEntries {
		if e.ID == id {
			m.pendingEntries = append(m.pendingEntries[:i], m.pendingEntries[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markFailedErr != nil {
		return m.markFailedErr
	}
	m.markFailedCalls = append(m.markFailedCalls, id)
	// Remove from pending
	for i, e := range m.pendingEntries {
		if e.ID == id {
			m.pendingEntries = append(m.pendingEntries[:i], m.pendingEntries[i+1:]...)
			break
		}
	}
	return nil
}

type mockEmbedder struct {
	mu        sync.Mutex
	embedErr  error
	callCount int
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, contents []string) ([][]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	// Return dummy embeddings
	result := make([][]float32, len(contents))
	for i := range contents {
		result[i] = make([]float32, 1536)
	}
	return result, nil
}

// --- Tests ---

func TestEmbeddingRetryWorker_ProcessesPending(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
			{ID: "entry-2", Content: "content 2"},
		},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Process once
	worker.processPendingEmbeddings(ctx)

	if embedder.callCount != 1 {
		t.Errorf("Expected 1 embed call, got %d", embedder.callCount)
	}

	store.mu.Lock()
	if len(store.updateEmbeddingCalls) != 2 {
		t.Errorf("Expected 2 UpdateEmbedding calls, got %d", len(store.updateEmbeddingCalls))
	}
	store.mu.Unlock()
}

func TestEmbeddingRetryWorker_UpdatesStatusOnSuccess(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
		},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx := context.Background()
	worker.processPendingEmbeddings(ctx)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.updateEmbeddingCalls) != 1 {
		t.Errorf("Expected 1 UpdateEmbedding call, got %d", len(store.updateEmbeddingCalls))
	}
	if len(store.updateEmbeddingCalls) > 0 && store.updateEmbeddingCalls[0] != "entry-1" {
		t.Errorf("Expected UpdateEmbedding for entry-1, got %s", store.updateEmbeddingCalls[0])
	}
}

func TestEmbeddingRetryWorker_IncrementsRetryOnFailure(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
		},
	}
	embedder := &mockEmbedder{embedErr: errors.New("API unavailable")}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx := context.Background()

	// First attempt - should fail and increment retry count
	worker.processPendingEmbeddings(ctx)

	if worker.retryCount["entry-1"] != 1 {
		t.Errorf("Expected retry count 1, got %d", worker.retryCount["entry-1"])
	}

	// Second attempt - should fail again and increment
	worker.processPendingEmbeddings(ctx)

	if worker.retryCount["entry-1"] != 2 {
		t.Errorf("Expected retry count 2, got %d", worker.retryCount["entry-1"])
	}
}

func TestEmbeddingRetryWorker_MarksFailedAfterMaxRetries(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
		},
	}
	embedder := &mockEmbedder{embedErr: errors.New("API unavailable")}

	// maxAttempts = 3
	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 3, 50)

	ctx := context.Background()

	// Simulate 3 failed attempts
	worker.retryCount["entry-1"] = 3

	// Next process should mark as failed
	worker.processPendingEmbeddings(ctx)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.markFailedCalls) != 1 {
		t.Errorf("Expected 1 MarkEmbeddingFailed call, got %d", len(store.markFailedCalls))
	}
	if len(store.markFailedCalls) > 0 && store.markFailedCalls[0] != "entry-1" {
		t.Errorf("Expected MarkEmbeddingFailed for entry-1, got %s", store.markFailedCalls[0])
	}

	// Retry count should be cleared
	if _, exists := worker.retryCount["entry-1"]; exists {
		t.Error("Expected retry count to be cleared after marking failed")
	}
}

func TestEmbeddingRetryWorker_GracefulShutdown(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, 50*time.Millisecond, 10, 50)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Cancel and verify it stops
	cancel()

	select {
	case <-done:
		// Success - worker stopped
	case <-time.After(time.Second):
		t.Error("Worker did not stop within timeout after context cancellation")
	}
}

func TestEmbeddingRetryWorker_EmptyPending(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx := context.Background()
	worker.processPendingEmbeddings(ctx)

	// Should not call embedder if no pending entries
	if embedder.callCount != 0 {
		t.Errorf("Expected 0 embed calls for empty pending, got %d", embedder.callCount)
	}
}

func TestEmbeddingRetryWorker_ProcessesImmediatelyOnStart(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
		},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx, cancel := context.WithCancel(context.Background())

	// Start worker in goroutine
	go func() {
		worker.Run(ctx)
	}()

	// Give it time to process immediately
	time.Sleep(50 * time.Millisecond)
	cancel()

	embedder.mu.Lock()
	defer embedder.mu.Unlock()

	// Should have processed immediately on start, before first tick
	if embedder.callCount < 1 {
		t.Error("Expected worker to process immediately on start")
	}
}

func TestEmbeddingRetryWorker_ClearsRetryCountOnSuccess(t *testing.T) {
	store := &mockStore{
		pendingEntries: []types.LoreEntry{
			{ID: "entry-1", Content: "content 1"},
		},
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	// Pre-set some retry count
	worker.retryCount["entry-1"] = 5

	ctx := context.Background()
	worker.processPendingEmbeddings(ctx)

	// After successful embedding, retry count should be cleared
	if _, exists := worker.retryCount["entry-1"]; exists {
		t.Error("Expected retry count to be cleared after successful embedding")
	}
}

func TestEmbeddingRetryWorker_HandlesStoreError(t *testing.T) {
	store := &mockStore{
		getPendingErr: errors.New("database connection failed"),
	}
	embedder := &mockEmbedder{}

	worker := NewEmbeddingRetryWorker(store, embedder, time.Hour, 10, 50)

	ctx := context.Background()

	// Should not panic on store error
	worker.processPendingEmbeddings(ctx)

	// Should not have called embedder
	if embedder.callCount != 0 {
		t.Errorf("Expected 0 embed calls on store error, got %d", embedder.callCount)
	}
}
