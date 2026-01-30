package store

import (
	"context"
	"io"
	"time"

	"github.com/hyperengineering/engram/internal/types"
)

// Store defines the interface contract for all lore storage operations.
type Store interface {
	IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error)
	FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error)
	MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error
	GetLore(ctx context.Context, id string) (*types.LoreEntry, error)
	DeleteLore(ctx context.Context, id string) error
	GetMetadata(ctx context.Context) (*types.StoreMetadata, error)
	GetSnapshot(ctx context.Context) (io.ReadCloser, error)
	GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error)
	GenerateSnapshot(ctx context.Context) error
	GetSnapshotPath(ctx context.Context) (string, error)
	RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error)
	DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
	GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error)
	UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
	MarkEmbeddingFailed(ctx context.Context, id string) error
	GetStats(ctx context.Context) (*types.StoreStats, error)
	GetExtendedStats(ctx context.Context) (*types.ExtendedStats, error)
	Close() error
}
