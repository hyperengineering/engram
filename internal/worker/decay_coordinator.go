package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
)

// DecayCapableStore defines the operations required for confidence decay.
// Implemented by SQLiteStore.
type DecayCapableStore interface {
	DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
	SetLastDecay(t time.Time)
}

// DecayStoreEnumerator provides access to all managed stores for decay operations.
// This abstraction allows testing with mock stores while production uses StoreManager.
type DecayStoreEnumerator interface {
	ListStores(ctx context.Context) ([]multistore.StoreInfo, error)
	GetDecayStore(ctx context.Context, storeID string) (DecayCapableStore, error)
}

// DecayCoordinator runs confidence decay across all managed stores.
type DecayCoordinator struct {
	manager     DecayStoreEnumerator
	interval    time.Duration
	decayAmount float64
}

// DecayStoreManagerAdapter adapts multistore.StoreManager to DecayStoreEnumerator.
type DecayStoreManagerAdapter struct {
	manager *multistore.StoreManager
}

// NewDecayStoreManagerAdapter creates an adapter for the given StoreManager.
func NewDecayStoreManagerAdapter(manager *multistore.StoreManager) *DecayStoreManagerAdapter {
	return &DecayStoreManagerAdapter{manager: manager}
}

// ListStores returns all stores from the underlying StoreManager.
func (a *DecayStoreManagerAdapter) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	return a.manager.ListStores(ctx)
}

// GetDecayStore returns the store which implements DecayCapableStore.
func (a *DecayStoreManagerAdapter) GetDecayStore(ctx context.Context, storeID string) (DecayCapableStore, error) {
	managed, err := a.manager.GetStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	return managed.Store, nil
}

// NewDecayCoordinator creates a coordinator for multi-store decay.
func NewDecayCoordinator(
	manager DecayStoreEnumerator,
	interval time.Duration,
	decayAmount float64,
) *DecayCoordinator {
	return &DecayCoordinator{
		manager:     manager,
		interval:    interval,
		decayAmount: decayAmount,
	}
}

// Run starts the decay coordinator loop. It blocks until ctx is cancelled.
//
// Unlike EmbeddingRetryCoordinator which runs immediately on start, this coordinator
// waits for the first ticker interval before processing. This is intentional because
// confidence decay is IO-intensive (scans all lore entries) and we avoid spiking
// resources during server startup. With a typical 24-hour interval, this delay is
// negligible.
func (c *DecayCoordinator) Run(ctx context.Context) {
	slog.Info("decay coordinator started",
		"component", "worker",
		"worker", "decay-coordinator",
		"interval", c.interval.String(),
		"decay_amount", c.decayAmount,
	)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("decay coordinator stopped",
				"component", "worker",
				"worker", "decay-coordinator",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			c.decayAllStores(ctx)
		}
	}
}

// decayAllStores runs decay on each store, continuing on individual failures.
func (c *DecayCoordinator) decayAllStores(ctx context.Context) {
	stores, err := c.manager.ListStores(ctx)
	if err != nil {
		slog.Error("failed to list stores for decay",
			"component", "worker",
			"worker", "decay-coordinator",
			"error", err,
		)
		return
	}

	var succeeded, failed int
	for _, info := range stores {
		if ctx.Err() != nil {
			return // Graceful shutdown
		}
		if c.decayStore(ctx, info.ID) {
			succeeded++
		} else {
			failed++
		}
	}

	// Log summary only if we processed stores (skip during mid-cycle shutdown)
	if succeeded > 0 || failed > 0 {
		slog.Info("decay cycle completed",
			"component", "worker",
			"worker", "decay-coordinator",
			"stores_total", len(stores),
			"stores_succeeded", succeeded,
			"stores_failed", failed,
		)
	}
}

// decayStore runs confidence decay for a single store.
// Returns true on success, false on failure.
func (c *DecayCoordinator) decayStore(ctx context.Context, storeID string) bool {
	start := time.Now()
	threshold := start.Add(-c.interval)

	slog.Debug("starting decay for store",
		"component", "worker",
		"worker", "decay-coordinator",
		"store_id", storeID,
		"threshold", threshold.Format(time.RFC3339),
	)

	store, err := c.manager.GetDecayStore(ctx, storeID)
	if err != nil {
		slog.Warn("failed to get store for decay",
			"component", "worker",
			"worker", "decay-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return false
	}

	affected, err := store.DecayConfidence(ctx, threshold, c.decayAmount)
	if err != nil {
		if ctx.Err() != nil {
			return false // Graceful shutdown, don't log as error
		}
		slog.Error("decay failed for store",
			"component", "worker",
			"worker", "decay-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return false
	}

	// Update per-store decay timestamp for observability
	store.SetLastDecay(time.Now().UTC())

	slog.Info("decay completed for store",
		"component", "worker",
		"worker", "decay-coordinator",
		"store_id", storeID,
		"entries_affected", affected,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return true
}
