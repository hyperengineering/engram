package store

import (
	"context"
	"io"
	"time"

	engramsync "github.com/hyperengineering/engram/internal/sync"
	"github.com/hyperengineering/engram/internal/types"
)

// mockStore is a compile-time check that the Store interface can be implemented.
type mockStore struct{}

var _ Store = (*mockStore)(nil)

func (m *mockStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	return nil, nil
}
func (m *mockStore) FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error) {
	return nil, nil
}
func (m *mockStore) MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error {
	return nil
}
func (m *mockStore) GetLore(ctx context.Context, id string) (*types.LoreEntry, error) {
	return nil, nil
}
func (m *mockStore) DeleteLore(ctx context.Context, id string) error {
	return nil
}
func (m *mockStore) GetMetadata(ctx context.Context) (*types.StoreMetadata, error) {
	return nil, nil
}
func (m *mockStore) GetSnapshot(ctx context.Context) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockStore) GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error) {
	return nil, nil
}
func (m *mockStore) GenerateSnapshot(ctx context.Context) error {
	return nil
}
func (m *mockStore) GetSnapshotPath(ctx context.Context) (string, error) {
	return "", nil
}
func (m *mockStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	return nil, nil
}
func (m *mockStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	return 0, nil
}
func (m *mockStore) SetLastDecay(t time.Time) {
	// No-op for testing
}
func (m *mockStore) GetLastDecay() *time.Time {
	return nil
}
func (m *mockStore) GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error) {
	return nil, nil
}
func (m *mockStore) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	return nil
}
func (m *mockStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
	return nil
}
func (m *mockStore) GetStats(ctx context.Context) (*types.StoreStats, error) {
	return nil, nil
}
func (m *mockStore) GetExtendedStats(ctx context.Context) (*types.ExtendedStats, error) {
	return nil, nil
}
func (m *mockStore) AppendChangeLog(ctx context.Context, entry *engramsync.ChangeLogEntry) (int64, error) {
	return 0, nil
}
func (m *mockStore) AppendChangeLogBatch(ctx context.Context, entries []engramsync.ChangeLogEntry) (int64, error) {
	return 0, nil
}
func (m *mockStore) GetChangeLogAfter(ctx context.Context, afterSeq int64, limit int) ([]engramsync.ChangeLogEntry, error) {
	return nil, nil
}
func (m *mockStore) GetLatestSequence(ctx context.Context) (int64, error) {
	return 0, nil
}
func (m *mockStore) CheckPushIdempotency(ctx context.Context, pushID string) ([]byte, bool, error) {
	return nil, false, nil
}
func (m *mockStore) RecordPushIdempotency(ctx context.Context, pushID, storeID string, response []byte, ttl time.Duration) error {
	return nil
}
func (m *mockStore) CleanExpiredIdempotency(ctx context.Context) (int64, error) {
	return 0, nil
}
func (m *mockStore) GetSyncMeta(ctx context.Context, key string) (string, error) {
	return "", nil
}
func (m *mockStore) SetSyncMeta(ctx context.Context, key, value string) error {
	return nil
}
func (m *mockStore) Close() error {
	return nil
}
