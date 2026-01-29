package worker

import (
	"context"
	"log/slog"
	"time"
)

// SnapshotStore defines the store operations needed by the snapshot worker.
type SnapshotStore interface {
	GenerateSnapshot(ctx context.Context) error
}

// SnapshotGenerationWorker generates periodic database snapshots.
type SnapshotGenerationWorker struct {
	store    SnapshotStore
	interval time.Duration
}

// NewSnapshotGenerationWorker creates a worker with the given store and interval.
func NewSnapshotGenerationWorker(store SnapshotStore, interval time.Duration) *SnapshotGenerationWorker {
	return &SnapshotGenerationWorker{
		store:    store,
		interval: interval,
	}
}

// Run starts the worker loop. Generates snapshot immediately on start,
// then on each interval. Respects context cancellation for graceful shutdown.
func (w *SnapshotGenerationWorker) Run(ctx context.Context) {
	slog.Info("worker started",
		"component", "worker",
		"worker", "snapshot-generation",
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Generate snapshot immediately on start
	w.generateSnapshot(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped",
				"component", "worker",
				"worker", "snapshot-generation",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			w.generateSnapshot(ctx)
		}
	}
}

// generateSnapshot generates a snapshot and logs any errors.
func (w *SnapshotGenerationWorker) generateSnapshot(ctx context.Context) {
	slog.Info("snapshot generation started",
		"component", "worker",
		"action", "snapshot_start",
	)

	if err := w.store.GenerateSnapshot(ctx); err != nil {
		// Check if it's a context cancellation (graceful shutdown)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("snapshot generation failed",
			"component", "worker",
			"action", "snapshot_failed",
			"error", err,
		)
	}
}
