package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/types"
)

// EmbeddingCapableStore defines the operations required for embedding retry.
// Implemented by SQLiteStore.
type EmbeddingCapableStore interface {
	GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error)
	UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
	MarkEmbeddingFailed(ctx context.Context, id string) error
}

// EmbeddingStoreEnumerator provides access to all managed stores for embedding retry.
// This abstraction allows testing with mock stores while production uses StoreManager.
type EmbeddingStoreEnumerator interface {
	ListStores(ctx context.Context) ([]multistore.StoreInfo, error)
	GetEmbeddingStore(ctx context.Context, storeID string) (EmbeddingCapableStore, error)
}

// EmbeddingRetryCoordinator runs embedding retry across all managed stores.
type EmbeddingRetryCoordinator struct {
	manager     EmbeddingStoreEnumerator
	embedder    Embedder
	interval    time.Duration
	maxAttempts int
	batchSize   int

	mu         sync.Mutex
	retryCount map[string]map[string]int // storeID -> entryID -> count
}

// EmbeddingStoreManagerAdapter adapts multistore.StoreManager to EmbeddingStoreEnumerator.
type EmbeddingStoreManagerAdapter struct {
	manager *multistore.StoreManager
}

// NewEmbeddingStoreManagerAdapter creates an adapter for the given StoreManager.
func NewEmbeddingStoreManagerAdapter(manager *multistore.StoreManager) *EmbeddingStoreManagerAdapter {
	return &EmbeddingStoreManagerAdapter{manager: manager}
}

// ListStores returns all stores from the underlying StoreManager.
func (a *EmbeddingStoreManagerAdapter) ListStores(ctx context.Context) ([]multistore.StoreInfo, error) {
	return a.manager.ListStores(ctx)
}

// GetEmbeddingStore returns the store which implements EmbeddingCapableStore.
func (a *EmbeddingStoreManagerAdapter) GetEmbeddingStore(ctx context.Context, storeID string) (EmbeddingCapableStore, error) {
	managed, err := a.manager.GetStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	return managed.Store, nil
}

// NewEmbeddingRetryCoordinator creates a coordinator for multi-store embedding retry.
func NewEmbeddingRetryCoordinator(
	manager EmbeddingStoreEnumerator,
	embedder Embedder,
	interval time.Duration,
	maxAttempts int,
	batchSize int,
) *EmbeddingRetryCoordinator {
	return &EmbeddingRetryCoordinator{
		manager:     manager,
		embedder:    embedder,
		interval:    interval,
		maxAttempts: maxAttempts,
		batchSize:   batchSize,
		retryCount:  make(map[string]map[string]int),
	}
}

// Run starts the embedding retry coordinator loop. It blocks until ctx is cancelled.
//
// Unlike DecayCoordinator which waits for the first tick, this coordinator processes
// immediately on start. This ensures lore entries that failed embedding during
// ingestion are retried promptly rather than waiting for the full interval.
func (c *EmbeddingRetryCoordinator) Run(ctx context.Context) {
	slog.Info("embedding coordinator started",
		"component", "worker",
		"worker", "embedding-coordinator",
		"interval", c.interval.String(),
		"max_attempts", c.maxAttempts,
		"batch_size", c.batchSize,
	)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Process immediately on start to handle pending entries from previous runs
	c.processAllStores(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("embedding coordinator stopped",
				"component", "worker",
				"worker", "embedding-coordinator",
				"reason", "context_cancelled",
			)
			return
		case <-ticker.C:
			c.processAllStores(ctx)
		}
	}
}

// processAllStores runs embedding retry on each store, continuing on individual failures.
func (c *EmbeddingRetryCoordinator) processAllStores(ctx context.Context) {
	stores, err := c.manager.ListStores(ctx)
	if err != nil {
		slog.Error("failed to list stores for embedding retry",
			"component", "worker",
			"worker", "embedding-coordinator",
			"error", err,
		)
		return
	}

	var succeeded, failed int
	for _, info := range stores {
		if ctx.Err() != nil {
			return // Graceful shutdown
		}
		if c.processStore(ctx, info.ID) {
			succeeded++
		} else {
			failed++
		}
	}

	// Log summary only if we processed stores (skip during mid-cycle shutdown)
	if succeeded > 0 || failed > 0 {
		slog.Debug("embedding retry cycle completed",
			"component", "worker",
			"worker", "embedding-coordinator",
			"stores_total", len(stores),
			"stores_succeeded", succeeded,
			"stores_failed", failed,
		)
	}

	// Clean up retry tracking for deleted stores to prevent memory leaks
	c.cleanupOrphanedRetryCounts(stores)
}

// cleanupOrphanedRetryCounts removes retry tracking for stores that no longer exist.
func (c *EmbeddingRetryCoordinator) cleanupOrphanedRetryCounts(activeStores []multistore.StoreInfo) {
	activeSet := make(map[string]struct{}, len(activeStores))
	for _, s := range activeStores {
		activeSet[s.ID] = struct{}{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var removedCount int
	for storeID := range c.retryCount {
		if _, exists := activeSet[storeID]; !exists {
			delete(c.retryCount, storeID)
			removedCount++
		}
	}

	if removedCount > 0 {
		slog.Debug("cleaned up retry counts for deleted stores",
			"component", "worker",
			"worker", "embedding-coordinator",
			"stores_removed", removedCount,
		)
	}
}

// processStore runs embedding retry for a single store.
// Returns true on success (including no pending work), false on failure.
func (c *EmbeddingRetryCoordinator) processStore(ctx context.Context, storeID string) bool {
	store, err := c.manager.GetEmbeddingStore(ctx, storeID)
	if err != nil {
		slog.Warn("failed to get store for embedding retry",
			"component", "worker",
			"worker", "embedding-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return false
	}

	entries, err := store.GetPendingEmbeddings(ctx, c.batchSize)
	if err != nil {
		if ctx.Err() != nil {
			return false // Graceful shutdown
		}
		slog.Error("failed to get pending embeddings",
			"component", "worker",
			"worker", "embedding-coordinator",
			"store_id", storeID,
			"error", err,
		)
		return false
	}

	if len(entries) == 0 {
		return true // No pending work
	}

	// Initialize retry tracking for this store if needed
	c.mu.Lock()
	if c.retryCount[storeID] == nil {
		c.retryCount[storeID] = make(map[string]int)
	}
	storeRetries := c.retryCount[storeID]
	c.mu.Unlock()

	// Filter out entries that have exceeded max attempts
	var toProcess []types.LoreEntry
	for _, entry := range entries {
		c.mu.Lock()
		attempts := storeRetries[entry.ID]
		c.mu.Unlock()

		if attempts >= c.maxAttempts {
			c.markAsFailed(ctx, store, storeID, entry.ID, attempts)
			continue
		}
		toProcess = append(toProcess, entry)
	}

	if len(toProcess) == 0 {
		return true // All entries exceeded max attempts
	}

	// Extract content for batch embedding
	contents := make([]string, len(toProcess))
	for i, entry := range toProcess {
		contents[i] = entry.Content
	}

	embeddings, err := c.embedder.EmbedBatch(ctx, contents)
	if err != nil {
		slog.Warn("embedding batch failed, will retry",
			"component", "worker",
			"worker", "embedding-coordinator",
			"store_id", storeID,
			"error", err,
			"entries_count", len(toProcess),
		)
		// Increment retry count for all entries in the failed batch
		c.mu.Lock()
		for _, entry := range toProcess {
			storeRetries[entry.ID]++
		}
		c.mu.Unlock()
		return false
	}

	// Update each entry with its embedding
	var successCount int
	for i, entry := range toProcess {
		if err := store.UpdateEmbedding(ctx, entry.ID, embeddings[i]); err != nil {
			slog.Error("failed to update embedding",
				"component", "worker",
				"worker", "embedding-coordinator",
				"store_id", storeID,
				"lore_id", entry.ID,
				"error", err,
			)
			c.mu.Lock()
			storeRetries[entry.ID]++
			c.mu.Unlock()
			continue
		}
		// Success: remove from retry tracking
		c.mu.Lock()
		delete(storeRetries, entry.ID)
		c.mu.Unlock()
		successCount++
	}

	if successCount > 0 {
		slog.Info("processed pending embeddings",
			"component", "worker",
			"worker", "embedding-coordinator",
			"store_id", storeID,
			"entries_processed", successCount,
		)
	}

	return true
}

// markAsFailed marks an entry as permanently failed after exhausting retry attempts.
func (c *EmbeddingRetryCoordinator) markAsFailed(ctx context.Context, store EmbeddingCapableStore, storeID, entryID string, attempts int) {
	if err := store.MarkEmbeddingFailed(ctx, entryID); err != nil {
		slog.Error("failed to mark embedding as failed",
			"component", "worker",
			"worker", "embedding-coordinator",
			"store_id", storeID,
			"lore_id", entryID,
			"error", err,
		)
		return
	}

	slog.Warn("embedding permanently failed after max attempts",
		"component", "worker",
		"worker", "embedding-coordinator",
		"store_id", storeID,
		"lore_id", entryID,
		"attempts", attempts,
	)

	// Remove from retry tracking
	c.mu.Lock()
	if storeRetries, ok := c.retryCount[storeID]; ok {
		delete(storeRetries, entryID)
	}
	c.mu.Unlock()
}
