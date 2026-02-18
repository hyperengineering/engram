package worker

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
)

// CompactionCapableStore defines operations required for change_log compaction.
// Implemented by SQLiteStore.
type CompactionCapableStore interface {
	// CompactChangeLog removes entries older than cutoff, keeping only latest per entity.
	// Returns: entries exported, entries deleted, error.
	CompactChangeLog(ctx context.Context, cutoff time.Time, auditDir string) (exported int64, deleted int64, err error)

	// SetLastCompaction records compaction metadata.
	SetLastCompaction(ctx context.Context, sequence int64, timestamp time.Time) error
}

// CompactionStoreEnumerator provides access to stores for compaction.
type CompactionStoreEnumerator interface {
	ListStores(ctx context.Context) ([]multistore.StoreInfo, error)
	// GetCompactionStore returns: store, basePath, error.
	GetCompactionStore(ctx context.Context, storeID string) (CompactionCapableStore, string, error)
}

// CompactionCoordinator runs change_log compaction across all stores.
type CompactionCoordinator struct {
	manager   CompactionStoreEnumerator
	interval  time.Duration
	retention time.Duration
}

// CompactionStoreManagerAdapter adapts multistore.StoreManager to CompactionStoreEnumerator.
type CompactionStoreManagerAdapter struct {
	manager *multistore.StoreManager
}

// NewCompactionStoreManagerAdapter creates an adapter for the given StoreManager.
func NewCompactionStoreManagerAdapter(manager *multistore.StoreManager) *CompactionStoreManagerAdapter {
	return &CompactionStoreManagerAdapter{manager: manager}
}

// ListStores returns all stores from the underlying StoreManager.
func (a *CompactionStoreManagerAdapter) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	return a.manager.ListStores(ctx)
}

// GetCompactionStore returns the store and its base path.
func (a *CompactionStoreManagerAdapter) GetCompactionStore(ctx context.Context, storeID string) (CompactionCapableStore, string, error) {
	managed, err := a.manager.GetStore(ctx, storeID)
	if err != nil {
		return nil, "", err
	}
	return managed.Store, managed.BasePath, nil
}

// NewCompactionCoordinator creates a compaction coordinator.
func NewCompactionCoordinator(
	manager CompactionStoreEnumerator,
	interval time.Duration,
	retention time.Duration,
) *CompactionCoordinator {
	return &CompactionCoordinator{
		manager:   manager,
		interval:  interval,
		retention: retention,
	}
}

// Run starts the coordinator loop. Blocks until ctx is cancelled.
//
// Like DecayCoordinator, this waits for the first ticker interval before processing.
// Compaction is IO-intensive; we avoid spiking resources during server startup.
func (c *CompactionCoordinator) Run(ctx context.Context) {
	slog.Info("compaction coordinator started",
		"component", "worker",
		"worker", "compaction-coordinator",
		"interval", c.interval.String(),
		"retention", c.retention.String(),
	)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("compaction coordinator stopped",
				"component", "worker",
				"worker", "compaction-coordinator",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			c.compactAllStores(ctx)
		}
	}
}

// compactAllStores runs compaction on each store, continuing on individual failures.
func (c *CompactionCoordinator) compactAllStores(ctx context.Context) {
	stores, err := c.manager.ListStores(ctx)
	if err != nil {
		slog.Error("failed to list stores for compaction",
			"component", "worker",
			"worker", "compaction-coordinator",
			"error", err,
		)
		return
	}

	var succeeded, failed, skipped int
	var totalExported, totalDeleted int64

	for _, info := range stores {
		if ctx.Err() != nil {
			return // Graceful shutdown
		}

		exported, deleted, ok := c.compactStore(ctx, info.ID)
		if ok {
			if exported == 0 && deleted == 0 {
				skipped++
			} else {
				succeeded++
				totalExported += exported
				totalDeleted += deleted
			}
		} else {
			failed++
		}
	}

	if succeeded > 0 || failed > 0 {
		slog.Info("compaction cycle completed",
			"component", "worker",
			"worker", "compaction-coordinator",
			"stores_total", len(stores),
			"stores_succeeded", succeeded,
			"stores_failed", failed,
			"stores_skipped", skipped,
			"entries_exported", totalExported,
			"entries_deleted", totalDeleted,
		)
	}
}

// compactStore runs compaction for a single store.
// Returns: exported, deleted, success.
func (c *CompactionCoordinator) compactStore(ctx context.Context, storeID string) (int64, int64, bool) {
	start := time.Now()
	cutoff := start.Add(-c.retention)

	store, basePath, err := c.manager.GetCompactionStore(ctx, storeID)
	if err != nil {
		slog.Warn("failed to get store for compaction",
			"component", "worker",
			"worker", "compaction-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return 0, 0, false
	}

	auditDir := filepath.Join(basePath, "audit")

	exported, deleted, err := store.CompactChangeLog(ctx, cutoff, auditDir)
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, false // Graceful shutdown
		}
		slog.Error("compaction failed for store",
			"component", "worker",
			"worker", "compaction-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return 0, 0, false
	}

	if exported == 0 && deleted == 0 {
		slog.Debug("no entries to compact",
			"component", "worker",
			"worker", "compaction-coordinator",
			"store_id", storeID,
		)
		return 0, 0, true
	}

	slog.Info("compaction completed for store",
		"component", "worker",
		"worker", "compaction-coordinator",
		"store_id", storeID,
		"entries_exported", exported,
		"entries_deleted", deleted,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return exported, deleted, true
}
