package worker

import (
	"context"
	"log/slog"
	"time"
)

// DecayStore defines the store operations needed by the decay worker.
type DecayStore interface {
	DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
}

// ConfidenceDecayWorker periodically decays confidence on stale lore entries.
type ConfidenceDecayWorker struct {
	store       DecayStore
	interval    time.Duration
	decayAmount float64
}

// NewConfidenceDecayWorker creates a worker with the given store, interval, and decay amount.
func NewConfidenceDecayWorker(store DecayStore, interval time.Duration, decayAmount float64) *ConfidenceDecayWorker {
	return &ConfidenceDecayWorker{
		store:       store,
		interval:    interval,
		decayAmount: decayAmount,
	}
}

// Run starts the worker loop. Blocks until ctx is cancelled.
// Does NOT run immediately on start (decay is a slow operation best run on schedule).
func (w *ConfidenceDecayWorker) Run(ctx context.Context) {
	slog.Info("worker started",
		"component", "worker",
		"worker", "confidence-decay",
		"interval", w.interval.String(),
		"decay_amount", w.decayAmount,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped",
				"component", "worker",
				"worker", "confidence-decay",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			w.runDecay(ctx)
		}
	}
}

// runDecay executes a single decay cycle.
func (w *ConfidenceDecayWorker) runDecay(ctx context.Context) {
	start := time.Now()

	// Threshold: entries not validated within one decay interval are stale
	threshold := start.Add(-w.interval)

	slog.Debug("decay cycle started",
		"component", "worker",
		"action", "decay_start",
		"threshold", threshold.Format(time.RFC3339),
	)

	affected, err := w.store.DecayConfidence(ctx, threshold, w.decayAmount)
	if err != nil {
		// Check for graceful shutdown
		if ctx.Err() != nil {
			return
		}
		slog.Error("decay failed",
			"component", "worker",
			"action", "decay_failed",
			"error", err,
		)
		return
	}

	duration := time.Since(start)
	slog.Info("decay cycle completed",
		"component", "worker",
		"action", "decay_complete",
		"affected", affected,
		"duration_ms", duration.Milliseconds(),
	)
}
