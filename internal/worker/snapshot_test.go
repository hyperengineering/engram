package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockSnapshotStore implements the SnapshotStore interface for testing.
type mockSnapshotStore struct {
	mu               sync.Mutex
	generateCalls    int
	generateErr      error
	generateDuration time.Duration
}

func (m *mockSnapshotStore) GenerateSnapshot(ctx context.Context) error {
	m.mu.Lock()
	m.generateCalls++
	duration := m.generateDuration
	err := m.generateErr
	m.mu.Unlock()

	// Simulate atomic operation that completes once started
	// (like VACUUM INTO which runs to completion)
	if duration > 0 {
		time.Sleep(duration)
	}
	return err
}

func (m *mockSnapshotStore) GetGenerateCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generateCalls
}

func TestSnapshotWorker_GeneratesOnStart(t *testing.T) {
	store := &mockSnapshotStore{}
	worker := NewSnapshotGenerationWorker(store, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	// Run worker in goroutine
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Wait for initial snapshot generation
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if store.GetGenerateCalls() < 1 {
		t.Errorf("Expected at least 1 GenerateSnapshot call on start, got %d", store.GetGenerateCalls())
	}
}

func TestSnapshotWorker_GeneratesOnInterval(t *testing.T) {
	store := &mockSnapshotStore{}
	worker := NewSnapshotGenerationWorker(store, 50*time.Millisecond) // Short interval for testing

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Wait for initial + at least 2 interval ticks
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	calls := store.GetGenerateCalls()
	// Should have initial + at least 2 interval calls
	if calls < 3 {
		t.Errorf("Expected at least 3 GenerateSnapshot calls (initial + 2 intervals), got %d", calls)
	}
}

func TestSnapshotWorker_StopsOnContextCancel(t *testing.T) {
	store := &mockSnapshotStore{}
	worker := NewSnapshotGenerationWorker(store, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Cancel immediately after start
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Worker should stop quickly
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not stop on context cancellation")
	}
}

func TestSnapshotWorker_LogsErrors(t *testing.T) {
	store := &mockSnapshotStore{generateErr: errors.New("test error")}
	worker := NewSnapshotGenerationWorker(store, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Let it run a couple cycles
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	// Worker should continue despite errors (not panic)
	calls := store.GetGenerateCalls()
	if calls < 2 {
		t.Errorf("Expected multiple GenerateSnapshot calls even with errors, got %d", calls)
	}
}

func TestSnapshotWorker_CompletesInProgressOnShutdown(t *testing.T) {
	store := &mockSnapshotStore{generateDuration: 100 * time.Millisecond}
	worker := NewSnapshotGenerationWorker(store, 1*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Cancel while generation is in progress
	time.Sleep(30 * time.Millisecond)
	cancel()

	<-done
	duration := time.Since(startTime)

	// Should have waited for the in-progress generation to complete
	// Generation takes 100ms, we cancelled at 30ms, so total should be ~100ms
	if duration < 80*time.Millisecond {
		t.Errorf("Worker did not complete in-progress snapshot, duration: %v", duration)
	}
}
