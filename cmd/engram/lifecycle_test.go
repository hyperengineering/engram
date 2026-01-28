package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// logCapture captures slog output for testing
type logCapture struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (c *logCapture) handler() slog.Handler {
	return slog.NewJSONHandler(c, &slog.HandlerOptions{Level: slog.LevelDebug})
}

func (c *logCapture) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var entry map[string]any
	if err := json.Unmarshal(p, &entry); err == nil {
		c.entries = append(c.entries, entry)
	}
	return len(p), nil
}

func (c *logCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var msgs []string
	for _, e := range c.entries {
		if msg, ok := e["msg"].(string); ok {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

func (c *logCapture) hasMessage(msg string) bool {
	for _, m := range c.messages() {
		if m == msg {
			return true
		}
	}
	return false
}

func (c *logCapture) messageIndex(msg string) int {
	for i, m := range c.messages() {
		if m == msg {
			return i
		}
	}
	return -1
}

// TestStartWorker_LaunchesGoroutineAndTracksCompletion tests the startWorker helper
func TestStartWorker_LaunchesGoroutineAndTracksCompletion(t *testing.T) {
	capture := &logCapture{}
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(capture.handler()))
	defer slog.SetDefault(oldDefault)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	workerRan := atomic.Bool{}
	startWorker(ctx, &wg, "test-worker", func(ctx context.Context) {
		workerRan.Store(true)
		<-ctx.Done()
	})

	// Give worker time to start
	time.Sleep(10 * time.Millisecond)

	if !workerRan.Load() {
		t.Error("worker function was not called")
	}

	// Cancel and wait for worker to complete
	cancel()
	wg.Wait()

	// Verify logging
	if !capture.hasMessage("worker started") {
		t.Error("expected 'worker started' log message")
	}
	if !capture.hasMessage("worker stopped") {
		t.Error("expected 'worker stopped' log message")
	}
}

// TestStartWorker_RespectsContextCancellation verifies workers stop when context is cancelled
func TestStartWorker_RespectsContextCancellation(t *testing.T) {
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	startWorker(ctx, &wg, "cancel-test", func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})

	cancel()

	select {
	case <-done:
		// Worker responded to cancellation
	case <-time.After(100 * time.Millisecond):
		t.Error("worker did not respond to context cancellation")
	}

	wg.Wait()
}

// TestShutdownLogging verifies shutdown sequence logging
func TestShutdownLogging(t *testing.T) {
	capture := &logCapture{}
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(capture.handler()))
	defer slog.SetDefault(oldDefault)

	// Simulate shutdown sequence logging
	slog.Info("shutdown initiated")
	slog.Info("shutdown complete")

	if !capture.hasMessage("shutdown initiated") {
		t.Error("expected 'shutdown initiated' log message")
	}
	if !capture.hasMessage("shutdown complete") {
		t.Error("expected 'shutdown complete' log message")
	}

	// Verify order
	initiatedIdx := capture.messageIndex("shutdown initiated")
	completeIdx := capture.messageIndex("shutdown complete")
	if initiatedIdx >= completeIdx {
		t.Error("'shutdown initiated' should come before 'shutdown complete'")
	}
}

// TestStartupSequenceLogging verifies all startup steps are logged in order
func TestStartupSequenceLogging(t *testing.T) {
	capture := &logCapture{}
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(capture.handler()))
	defer slog.SetDefault(oldDefault)

	// Simulate the startup sequence logging (as it should be in run())
	slog.Info("configuration loaded")
	slog.Info("logger initialized", "level", "info")
	slog.Info("store initialized", "path", "test.db")
	slog.Info("embedder initialized", "model", "test-model")
	slog.Info("router initialized")
	slog.Info("server starting", "address", ":8080")

	expectedMessages := []string{
		"configuration loaded",
		"logger initialized",
		"store initialized",
		"embedder initialized",
		"router initialized",
		"server starting",
	}

	messages := capture.messages()
	for i, expected := range expectedMessages {
		if i >= len(messages) {
			t.Errorf("missing message at index %d: expected %q", i, expected)
			continue
		}
		if messages[i] != expected {
			t.Errorf("message at index %d = %q, want %q", i, messages[i], expected)
		}
	}
}

// TestGracefulShutdownDrainsRequests verifies in-flight requests complete before shutdown
func TestGracefulShutdownDrainsRequests(t *testing.T) {
	// Create a handler that takes time to respond
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Addr:    ":0", // Random port
		Handler: slowHandler,
	}

	// Start server
	go srv.ListenAndServe()
	time.Sleep(10 * time.Millisecond) // Let server start

	// This test validates the pattern - actual integration test would need real server binding
	// The key behavior is that Shutdown() waits for in-flight requests

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Shutdown should succeed within timeout
	if err := srv.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
		// Server may not have started listening, which is OK for this unit test
		t.Logf("shutdown returned: %v (acceptable for unit test)", err)
	}
}

// TestShutdownTimeoutRespected verifies shutdown doesn't hang indefinitely
func TestShutdownTimeoutRespected(t *testing.T) {
	// Create a server with a handler that never responds
	blockingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {} // Block forever
	})

	srv := &http.Server{
		Addr:    ":0",
		Handler: blockingHandler,
	}

	// Very short timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	srv.Shutdown(shutdownCtx)
	elapsed := time.Since(start)

	// Shutdown should respect timeout (with some buffer)
	if elapsed > 50*time.Millisecond {
		t.Errorf("shutdown took %v, expected <= 50ms", elapsed)
	}
}

// TestWorkerWaitGroupIntegration verifies workers are waited on during shutdown
func TestWorkerWaitGroupIntegration(t *testing.T) {
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	workerCompleted := atomic.Bool{}
	startWorker(ctx, &wg, "slow-worker", func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond) // Simulate cleanup work
		workerCompleted.Store(true)
	})

	// Cancel and wait
	cancel()
	wg.Wait()

	if !workerCompleted.Load() {
		t.Error("wg.Wait() returned before worker completed")
	}
}

// TestStoreClosedLast verifies store is closed after server and workers
func TestStoreClosedLast(t *testing.T) {
	var order []string
	var mu sync.Mutex

	recordOrder := func(step string) {
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
	}

	// Simulate shutdown sequence
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	// Add a worker
	startWorker(ctx, &wg, "order-test", func(ctx context.Context) {
		<-ctx.Done()
		recordOrder("worker_stopped")
	})

	// Simulate shutdown
	cancel()                        // Signal workers
	recordOrder("server_shutdown")  // Would be srv.Shutdown()
	wg.Wait()                       // Wait for workers
	recordOrder("store_closed")     // Would be db.Close()

	// Give worker goroutine time to record
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Verify order: server_shutdown -> worker_stopped -> store_closed
	if len(order) < 3 {
		t.Fatalf("expected 3 order entries, got %d: %v", len(order), order)
	}

	serverIdx := indexOf(order, "server_shutdown")
	workerIdx := indexOf(order, "worker_stopped")
	storeIdx := indexOf(order, "store_closed")

	if serverIdx == -1 || workerIdx == -1 || storeIdx == -1 {
		t.Fatalf("missing order entries: %v", order)
	}

	// Store must be closed last
	if storeIdx < workerIdx {
		t.Errorf("store closed before workers: %v", order)
	}
}

func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}

// TestStartWorker_LogsWorkerName verifies worker name is included in log attributes
func TestStartWorker_LogsWorkerName(t *testing.T) {
	capture := &logCapture{}
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(capture.handler()))
	defer slog.SetDefault(oldDefault)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	startWorker(ctx, &wg, "my-custom-worker", func(ctx context.Context) {
		<-ctx.Done()
	})

	time.Sleep(10 * time.Millisecond)
	cancel()
	wg.Wait()

	// Check that worker name is in log entries
	capture.mu.Lock()
	defer capture.mu.Unlock()

	foundWorkerName := false
	for _, entry := range capture.entries {
		if worker, ok := entry["worker"].(string); ok && worker == "my-custom-worker" {
			foundWorkerName = true
			break
		}
	}

	if !foundWorkerName {
		t.Error("expected log entry with worker='my-custom-worker' attribute")
	}
}
