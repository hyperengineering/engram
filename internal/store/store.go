package store

import (
	"context"
	"io"
	"time"

	engramsync "github.com/hyperengineering/engram/internal/sync"
	"github.com/hyperengineering/engram/internal/types"
)

// Store defines the interface contract for all lore storage operations.
type Store interface {
	IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error)
	FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error)
	MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error
	GetLore(ctx context.Context, id string) (*types.LoreEntry, error)
	DeleteLore(ctx context.Context, id, sourceID string) error
	GetMetadata(ctx context.Context) (*types.StoreMetadata, error)
	GetSnapshot(ctx context.Context) (io.ReadCloser, error)
	GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error)
	GenerateSnapshot(ctx context.Context) error
	GetSnapshotPath(ctx context.Context) (string, error)
	RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error)
	DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
	SetLastDecay(t time.Time)
	GetLastDecay() *time.Time
	GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error)
	UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
	MarkEmbeddingFailed(ctx context.Context, id string) error
	GetStats(ctx context.Context) (*types.StoreStats, error)
	GetExtendedStats(ctx context.Context) (*types.ExtendedStats, error)

	// Change log operations (sync protocol)
	AppendChangeLog(ctx context.Context, entry *engramsync.ChangeLogEntry) (int64, error)
	AppendChangeLogBatch(ctx context.Context, entries []engramsync.ChangeLogEntry) (int64, error)
	GetChangeLogAfter(ctx context.Context, afterSeq int64, limit int) ([]engramsync.ChangeLogEntry, error)
	GetLatestSequence(ctx context.Context) (int64, error)

	// Push idempotency operations (sync protocol)
	CheckPushIdempotency(ctx context.Context, pushID string) ([]byte, bool, error)
	RecordPushIdempotency(ctx context.Context, pushID, storeID string, response []byte, ttl time.Duration) error
	CleanExpiredIdempotency(ctx context.Context) (int64, error)

	// Sync metadata operations
	GetSyncMeta(ctx context.Context, key string) (string, error)
	SetSyncMeta(ctx context.Context, key, value string) error

	// Replay operations (used by domain plugins during sync replay)
	UpsertRow(ctx context.Context, tableName string, entityID string, payload []byte) error
	DeleteRow(ctx context.Context, tableName string, entityID string) error
	QueueEmbedding(ctx context.Context, entryID string) error

	Close() error
}
