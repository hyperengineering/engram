package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockDecayStore implements DecayStore for testing
type mockDecayStore struct {
	mu             sync.Mutex
	calls          []decayCall
	decayErr       error
	affectedCount  int64
}

type decayCall struct {
	threshold time.Time
	amount    float64
}

func (m *mockDecayStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, decayCall{threshold: threshold, amount: amount})
	if m.decayErr != nil {
		return 0, m.decayErr
	}
	return m.affectedCount, nil
}

func (m *mockDecayStore) getCalls() []decayCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]decayCall{}, m.calls...)
}

func (m *mockDecayStore) SetLastDecay(t time.Time) {
	// No-op for testing - we don't need to track this in tests
}

func TestConfidenceDecayWorker_RunsOnSchedule(t *testing.T) {
	store := &mockDecayStore{affectedCount: 5}
	worker := NewConfidenceDecayWorker(store, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	go worker.Run(ctx)

	// Wait for at least 2 ticks
	time.Sleep(120 * time.Millisecond)
	cancel()

	calls := store.getCalls()
	if len(calls) < 2 {
		t.Errorf("Expected at least 2 decay calls, got %d", len(calls))
	}

	// Verify decay amount
	for _, call := range calls {
		if call.amount != 0.01 {
			t.Errorf("Expected decay amount 0.01, got %v", call.amount)
		}
	}
}

func TestConfidenceDecayWorker_DoesNotRunImmediately(t *testing.T) {
	store := &mockDecayStore{affectedCount: 5}
	worker := NewConfidenceDecayWorker(store, 1*time.Hour, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	go worker.Run(ctx)

	// Wait a short time - should NOT have decayed yet
	time.Sleep(50 * time.Millisecond)
	cancel()

	calls := store.getCalls()
	if len(calls) != 0 {
		t.Errorf("Expected 0 decay calls (does not run immediately), got %d", len(calls))
	}
}

func TestConfidenceDecayWorker_GracefulShutdown(t *testing.T) {
	store := &mockDecayStore{affectedCount: 5}
	worker := NewConfidenceDecayWorker(store, 1*time.Hour, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	// Cancel immediately
	cancel()

	// Should stop within reasonable time
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Worker did not stop within 1 second")
	}
}

func TestConfidenceDecayWorker_HandlesStoreError(t *testing.T) {
	store := &mockDecayStore{
		decayErr:      errors.New("database error"),
		affectedCount: 0,
	}
	worker := NewConfidenceDecayWorker(store, 50*time.Millisecond, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	go worker.Run(ctx)

	// Wait for at least 2 ticks (should continue despite errors)
	time.Sleep(120 * time.Millisecond)
	cancel()

	calls := store.getCalls()
	if len(calls) < 2 {
		t.Errorf("Expected at least 2 decay calls (continues on error), got %d", len(calls))
	}
}

func TestConfidenceDecayWorker_CalculatesThreshold(t *testing.T) {
	store := &mockDecayStore{affectedCount: 5}
	interval := 100 * time.Millisecond
	worker := NewConfidenceDecayWorker(store, interval, 0.01)

	ctx, cancel := context.WithCancel(context.Background())

	startTime := time.Now()
	go worker.Run(ctx)

	// Wait for first tick
	time.Sleep(150 * time.Millisecond)
	cancel()

	calls := store.getCalls()
	if len(calls) == 0 {
		t.Fatal("Expected at least 1 decay call")
	}

	// Threshold should be approximately (call time - interval)
	// The call happened around startTime + 100ms, so threshold should be around startTime
	call := calls[0]
	expectedThreshold := startTime
	diff := call.threshold.Sub(expectedThreshold)
	if diff < -50*time.Millisecond || diff > 50*time.Millisecond {
		t.Errorf("Threshold %v not close to expected %v (diff: %v)", call.threshold, expectedThreshold, diff)
	}
}

func TestConfidenceDecayWorker_UsesConfiguredAmount(t *testing.T) {
	store := &mockDecayStore{affectedCount: 5}
	customAmount := 0.05
	worker := NewConfidenceDecayWorker(store, 50*time.Millisecond, customAmount)

	ctx, cancel := context.WithCancel(context.Background())

	go worker.Run(ctx)

	// Wait for first tick
	time.Sleep(70 * time.Millisecond)
	cancel()

	calls := store.getCalls()
	if len(calls) == 0 {
		t.Fatal("Expected at least 1 decay call")
	}

	if calls[0].amount != customAmount {
		t.Errorf("Expected decay amount %v, got %v", customAmount, calls[0].amount)
	}
}
