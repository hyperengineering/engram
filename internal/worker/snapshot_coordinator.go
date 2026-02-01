package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
)

// StoreEnumerator provides access to all managed stores.
// This interface allows testing with mock implementations.
type StoreEnumerator interface {
	ListStores(ctx context.Context) ([]multistore.StoreInfo, error)
	GetStore(ctx context.Context, storeID string) (SnapshotCapableStore, error)
}

// SnapshotCapableStore represents a store that can generate snapshots.
type SnapshotCapableStore interface {
	GenerateSnapshot(ctx context.Context) error
}

// StoreManagerAdapter adapts multistore.StoreManager to StoreEnumerator.
type StoreManagerAdapter struct {
	manager *multistore.StoreManager
}

// NewStoreManagerAdapter creates an adapter for the given StoreManager.
func NewStoreManagerAdapter(manager *multistore.StoreManager) *StoreManagerAdapter {
	return &StoreManagerAdapter{manager: manager}
}

// ListStores returns all stores from the underlying StoreManager.
func (a *StoreManagerAdapter) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	return a.manager.ListStores(ctx)
}

// GetStore returns the store's underlying Store which implements SnapshotCapableStore.
func (a *StoreManagerAdapter) GetStore(ctx context.Context, storeID string) (SnapshotCapableStore, error) {
	managed, err := a.manager.GetStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	return managed.Store, nil
}

// SnapshotCoordinator generates snapshots for all managed stores.
type SnapshotCoordinator struct {
	manager  StoreEnumerator
	interval time.Duration
}

// NewSnapshotCoordinator creates a coordinator that generates snapshots
// for all stores managed by the given StoreEnumerator.
func NewSnapshotCoordinator(
	manager StoreEnumerator,
	interval time.Duration,
) *SnapshotCoordinator {
	return &SnapshotCoordinator{
		manager:  manager,
		interval: interval,
	}
}

// Run starts the coordinator loop.
func (c *SnapshotCoordinator) Run(ctx context.Context) {
	slog.Info("worker started",
		"component", "worker",
		"worker", "snapshot-coordinator",
		"action", "worker_started",
	)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Generate snapshots immediately on start
	c.generateAllSnapshots(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped",
				"component", "worker",
				"worker", "snapshot-coordinator",
				"action", "worker_stopped",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			c.generateAllSnapshots(ctx)
		}
	}
}

// generateAllSnapshots iterates through all stores and generates snapshots.
func (c *SnapshotCoordinator) generateAllSnapshots(ctx context.Context) {
	stores, err := c.manager.ListStores(ctx)
	if err != nil {
		slog.Error("failed to list stores for snapshot generation",
			"component", "worker",
			"worker", "snapshot-coordinator",
			"action", "list_stores_failed",
			"error", err,
		)
		return
	}

	var succeeded, failed int
	for _, storeInfo := range stores {
		if ctx.Err() != nil {
			return // Graceful shutdown, don't log summary
		}
		if c.generateStoreSnapshot(ctx, storeInfo.ID) {
			succeeded++
		} else {
			failed++
		}
	}

	// Log summary only if we processed stores (not during shutdown)
	if succeeded > 0 || failed > 0 {
		slog.Info("snapshot generation cycle completed",
			"component", "worker",
			"worker", "snapshot-coordinator",
			"action", "cycle_complete",
			"total", len(stores),
			"succeeded", succeeded,
			"failed", failed,
		)
	}
}

// generateStoreSnapshot generates a snapshot for a single store.
// Returns true if successful, false if failed.
func (c *SnapshotCoordinator) generateStoreSnapshot(ctx context.Context, storeID string) bool {
	slog.Info("snapshot generation started",
		"component", "worker",
		"worker", "snapshot-coordinator",
		"action", "snapshot_start",
		"store_id", storeID,
	)

	store, err := c.manager.GetStore(ctx, storeID)
	if err != nil {
		slog.Warn("failed to get store for snapshot",
			"component", "worker",
			"worker", "snapshot-coordinator",
			"action", "snapshot_failed",
			"store_id", storeID,
			"error", err,
		)
		return false
	}

	if err := store.GenerateSnapshot(ctx); err != nil {
		if ctx.Err() != nil {
			return false // Graceful shutdown, don't log as error
		}
		slog.Warn("snapshot generation failed",
			"component", "worker",
			"worker", "snapshot-coordinator",
			"action", "snapshot_failed",
			"store_id", storeID,
			"error", err,
		)
		return false
	}
	return true
}
