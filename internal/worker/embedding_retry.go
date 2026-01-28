package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperengineering/engram/internal/types"
)

// EmbeddingStore defines the store operations needed by the embedding retry worker.
type EmbeddingStore interface {
	GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error)
	UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
	MarkEmbeddingFailed(ctx context.Context, id string) error
}

// Embedder defines the embedding operations needed by the worker.
type Embedder interface {
	EmbedBatch(ctx context.Context, contents []string) ([][]float32, error)
}

// EmbeddingRetryWorker processes lore entries with pending embeddings.
type EmbeddingRetryWorker struct {
	store       EmbeddingStore
	embedder    Embedder
	interval    time.Duration
	maxAttempts int
	batchSize   int
	retryCount  map[string]int // tracks retry attempts per entry ID
}

// NewEmbeddingRetryWorker creates a new embedding retry worker.
func NewEmbeddingRetryWorker(
	s EmbeddingStore,
	e Embedder,
	interval time.Duration,
	maxAttempts int,
	batchSize int,
) *EmbeddingRetryWorker {
	return &EmbeddingRetryWorker{
		store:       s,
		embedder:    e,
		interval:    interval,
		maxAttempts: maxAttempts,
		batchSize:   batchSize,
		retryCount:  make(map[string]int),
	}
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *EmbeddingRetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Process immediately on start, then on each tick
	w.processPendingEmbeddings(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processPendingEmbeddings(ctx)
		}
	}
}

func (w *EmbeddingRetryWorker) processPendingEmbeddings(ctx context.Context) {
	entries, err := w.store.GetPendingEmbeddings(ctx, w.batchSize)
	if err != nil {
		slog.Error("failed to get pending embeddings",
			"error", err,
			"component", "worker",
		)
		return
	}

	if len(entries) == 0 {
		return
	}

	// Filter out entries that have exceeded max attempts
	var toProcess []types.LoreEntry
	for _, e := range entries {
		if w.retryCount[e.ID] >= w.maxAttempts {
			w.markAsFailed(ctx, e.ID)
			continue
		}
		toProcess = append(toProcess, e)
	}

	if len(toProcess) == 0 {
		return
	}

	// Extract content for batch embedding
	contents := make([]string, len(toProcess))
	for i, e := range toProcess {
		contents[i] = e.Content
	}

	embeddings, err := w.embedder.EmbedBatch(ctx, contents)
	if err != nil {
		slog.Warn("embedding batch failed, will retry",
			"error", err,
			"count", len(toProcess),
			"component", "worker",
		)
		// Increment retry count for all entries in batch
		for _, e := range toProcess {
			w.retryCount[e.ID]++
		}
		return
	}

	// Update each entry with its embedding
	var successCount int
	for i, entry := range toProcess {
		if err := w.store.UpdateEmbedding(ctx, entry.ID, embeddings[i]); err != nil {
			slog.Error("failed to update embedding",
				"lore_id", entry.ID,
				"error", err,
				"component", "worker",
			)
			w.retryCount[entry.ID]++
			continue
		}
		// Success — remove from retry tracking
		delete(w.retryCount, entry.ID)
		successCount++
	}

	if successCount > 0 {
		slog.Info("processed pending embeddings",
			"action", "embed_retry",
			"count", successCount,
			"component", "worker",
		)
	}
}

func (w *EmbeddingRetryWorker) markAsFailed(ctx context.Context, id string) {
	attempts := w.retryCount[id]

	if err := w.store.MarkEmbeddingFailed(ctx, id); err != nil {
		slog.Error("failed to mark embedding as failed",
			"lore_id", id,
			"error", err,
			"component", "worker",
		)
		return
	}

	slog.Error("embedding permanently failed",
		"action", "embed_retry",
		"lore_id", id,
		"attempts", attempts,
		"component", "worker",
	)

	// Remove from retry tracking
	delete(w.retryCount, id)
}
