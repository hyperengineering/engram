package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hyperengineering/engram/internal/types"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// ErrNotImplemented is returned for Store interface methods not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// Constants for merge operations
const (
	ContextSeparator = "\n\n---\n\n" // 7 characters
	MaxContextLength = 1000
	ConfidenceBoost  = 0.10
	MaxConfidence    = 1.0
)

// Feedback confidence adjustment constants (FR19, FR21)
const (
	FeedbackHelpfulBoost     = 0.08 // FR19: helpful → +0.08
	FeedbackIncorrectPenalty = 0.15 // FR19: incorrect → -0.15
	FeedbackNotRelevantDelta = 0.0  // FR19: not_relevant → 0
	MinConfidence            = 0.0  // FR21: floor
)

// Decay constants (FR22)
const (
	DefaultDecayAmount = 0.01 // FR22: -0.01 per decay cycle
)

// appendContext appends new context to existing, respecting the MaxContextLength limit.
// Truncation applies to the new context only, preserving existing content.
func appendContext(existing, new string) string {
	if new == "" {
		return existing
	}

	if existing == "" {
		if len(new) > MaxContextLength {
			return new[:MaxContextLength-3] + "..."
		}
		return new
	}

	available := MaxContextLength - len(existing) - len(ContextSeparator)

	if available <= 0 {
		return existing // No room to append
	}

	if len(new) > available {
		if available <= 3 {
			return existing // Not enough room even for "..."
		}
		return existing + ContextSeparator + new[:available-3] + "..."
	}

	return existing + ContextSeparator + new
}

// addSourceID adds a source ID to the sources slice if not already present.
// Returns the updated slice and whether the ID was added.
func addSourceID(sources []string, newSourceID string) ([]string, bool) {
	for _, s := range sources {
		if s == newSourceID {
			return sources, false // Already present
		}
	}
	return append(sources, newSourceID), true
}

// SQLiteStore represents the SQLite-backed lore database.
type SQLiteStore struct {
	db           *sql.DB
	dbPath       string
	embedder     Embedder
	cfg          Config
	snapshotMu   sync.Mutex
	lastSnapshot *time.Time
}

// Embedder is the interface for embedding generation (matches embedding.Embedder).
type Embedder interface {
	Embed(ctx context.Context, content string) ([]float32, error)
	EmbedBatch(ctx context.Context, contents []string) ([][]float32, error)
	ModelName() string
}

// Config is the interface for configuration access.
type Config interface {
	GetDeduplicationEnabled() bool
	GetSimilarityThreshold() float64
}

// NewSQLiteStore creates a new SQLiteStore instance.
// It initializes the database with WAL mode, applies pragmas, and runs migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure parent directory exists
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable pragmas for performance and safety
	if err := enablePragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable pragmas: %w", err)
	}

	// Run goose migrations
	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &SQLiteStore{db: db, dbPath: dbPath}, nil
}

// SetDependencies configures the embedder and config for deduplication.
// This is called after construction to inject dependencies without changing the constructor signature.
func (s *SQLiteStore) SetDependencies(embedder Embedder, cfg Config) {
	s.embedder = embedder
	s.cfg = cfg
}

// enablePragmas sets SQLite pragmas for optimal performance and safety.
func enablePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("execute %s: %w", pragma, err)
		}
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// snapshotDir returns the directory for snapshot files.
func (s *SQLiteStore) snapshotDir() string {
	return filepath.Join(filepath.Dir(s.dbPath), "snapshots")
}

// snapshotPath returns the path to the current snapshot file.
func (s *SQLiteStore) snapshotPath() string {
	return filepath.Join(s.snapshotDir(), "current.db")
}


// Record stores a new lore entry
func (s *SQLiteStore) Record(lore types.Lore, embedding []float32) (*types.Lore, error) {
	now := time.Now().UTC()
	lore.ID = ulid.Make().String()
	lore.CreatedAt = now
	lore.UpdatedAt = now
	lore.Embedding = packEmbedding(embedding)

	_, err := s.db.Exec(`
		INSERT INTO lore_entries (id, content, context, category, confidence, embedding, source_id, validation_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lore.ID, lore.Content, lore.Context, lore.Category, lore.Confidence, lore.Embedding, lore.SourceID, lore.ValidationCount, lore.CreatedAt.Format(time.RFC3339), lore.UpdatedAt.Format(time.RFC3339))

	if err != nil {
		return nil, err
	}

	return &lore, nil
}

// Count returns the number of lore entries (excluding soft-deleted)
func (s *SQLiteStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	return count, err
}

// GetStats returns aggregate store statistics
func (s *SQLiteStore) GetStats(ctx context.Context) (*types.StoreStats, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	if err != nil {
		return nil, err
	}

	return &types.StoreStats{
		LoreCount:    count,
		LastSnapshot: s.lastSnapshot,
	}, nil
}


func packEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func unpackEmbedding(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// scanLoreEntry scans a row into a LoreEntry, handling BLOB unpacking and JSON parsing.
func scanLoreEntry(scanner interface{ Scan(...any) error }) (*types.LoreEntry, error) {
	var entry types.LoreEntry
	var embeddingBlob []byte
	var sourcesJSON string
	var createdAt, updatedAt string
	var deletedAt, lastValidatedAt sql.NullString

	err := scanner.Scan(
		&entry.ID,
		&entry.Content,
		&entry.Context,
		&entry.Category,
		&entry.Confidence,
		&embeddingBlob,
		&entry.EmbeddingStatus,
		&entry.SourceID,
		&sourcesJSON,
		&entry.ValidationCount,
		&createdAt,
		&updatedAt,
		&deletedAt,
		&lastValidatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Unpack embedding if present
	if len(embeddingBlob) > 0 {
		entry.Embedding = unpackEmbedding(embeddingBlob)
	}

	// Parse sources JSON
	if sourcesJSON != "" {
		if err := json.Unmarshal([]byte(sourcesJSON), &entry.Sources); err != nil {
			return nil, fmt.Errorf("parse sources JSON: %w", err)
		}
	}

	// Parse timestamps
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		entry.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		entry.UpdatedAt = t
	}
	if deletedAt.Valid {
		if t, err := time.Parse(time.RFC3339, deletedAt.String); err == nil {
			entry.DeletedAt = &t
		}
	}
	if lastValidatedAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastValidatedAt.String); err == nil {
			entry.LastValidatedAt = &t
		}
	}

	return &entry, nil
}

// IngestLore stores new lore entries with optional embedding generation and deduplication.
// If an embedder is configured, embeddings are generated synchronously.
// If deduplication is enabled and embeddings are available, similar entries are merged.
func (s *SQLiteStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	if len(entries) == 0 {
		return &types.IngestResult{Accepted: 0, Merged: 0, Rejected: 0, Errors: []string{}}, nil
	}

	start := time.Now()
	result := &types.IngestResult{Errors: []string{}}

	// 1. Generate embeddings if embedder is available
	var embeddings [][]float32
	var embeddingErr error
	if s.embedder != nil {
		contents := make([]string, len(entries))
		for i, e := range entries {
			contents[i] = e.Content
		}
		embeddings, embeddingErr = s.embedder.EmbedBatch(ctx, contents)
		if embeddingErr != nil {
			slog.Warn("embedding generation failed, entries will be stored pending",
				"error", embeddingErr, "count", len(entries))
		}
	}

	// 2. Determine deduplication settings
	dedupEnabled := s.cfg != nil && s.cfg.GetDeduplicationEnabled()
	threshold := 0.92
	if s.cfg != nil {
		threshold = s.cfg.GetSimilarityThreshold()
	}

	// 3. Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 4. Process each entry
	for i, entry := range entries {
		var embedding []float32
		hasEmbedding := embeddingErr == nil && embeddings != nil && i < len(embeddings) && len(embeddings[i]) > 0
		if hasEmbedding {
			embedding = embeddings[i]
		}

		// 5. Deduplication check (if enabled and embedding available)
		if dedupEnabled && hasEmbedding {
			similar, err := s.findSimilarInTx(ctx, tx, embedding, entry.Category, threshold)
			if err != nil {
				return nil, fmt.Errorf("find similar: %w", err)
			}

			if len(similar) > 0 {
				// Merge with best match (highest similarity)
				bestMatch := similar[0]
				if err := s.mergeLoreInTx(ctx, tx, bestMatch.ID, entry); err != nil {
					return nil, fmt.Errorf("merge lore: %w", err)
				}
				result.Merged++
				continue
			}
		}

		// 6. Store as new entry
		if err := s.insertEntryInTx(ctx, tx, entry, embedding, hasEmbedding); err != nil {
			return nil, fmt.Errorf("insert entry: %w", err)
		}
		result.Accepted++
	}

	// 7. Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// 8. Performance logging
	duration := time.Since(start)
	if duration > 5*time.Second {
		slog.Warn("ingest batch exceeded performance target",
			"duration_ms", duration.Milliseconds(),
			"count", len(entries),
			"accepted", result.Accepted,
			"merged", result.Merged,
		)
	}

	return result, nil
}

// GetLore retrieves a lore entry by ID.
func (s *SQLiteStore) GetLore(ctx context.Context, id string) (*types.LoreEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries
		WHERE id = ? AND deleted_at IS NULL
	`, id)

	entry, err := scanLoreEntry(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan row: %w", err)
	}

	return entry, nil
}

// GetPendingEmbeddings retrieves entries that need embedding generation.
func (s *SQLiteStore) GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries
		WHERE embedding_status = 'pending' AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending embeddings: %w", err)
	}
	defer rows.Close()

	var entries []types.LoreEntry
	for rows.Next() {
		entry, err := scanLoreEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return entries, nil
}

// UpdateEmbedding stores the embedding for a lore entry and marks it complete.
func (s *SQLiteStore) UpdateEmbedding(ctx context.Context, id string, embedding []float32) error {
	embeddingBlob := packEmbedding(embedding)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET embedding = ?, embedding_status = 'complete', updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, embeddingBlob, now, id)
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkEmbeddingFailed marks an entry's embedding as permanently failed.
func (s *SQLiteStore) MarkEmbeddingFailed(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET embedding_status = 'failed', updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, id)
	if err != nil {
		return fmt.Errorf("mark embedding failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// FindSimilar finds lore entries similar to the given embedding within the same category.
// Returns entries with cosine similarity >= threshold, ordered by similarity descending.
func (s *SQLiteStore) FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error) {
	// Delegate to findSimilarInTx; *sql.DB satisfies queryContext interface
	return s.findSimilarInTx(ctx, s.db, embedding, category, threshold)
}

// --- Transaction-aware helper methods for deduplication ---

// queryContext is the interface satisfied by both *sql.DB and *sql.Tx for query operations.
// This abstraction allows the same query logic to execute both within transactions (for atomic
// deduplication operations) and outside transactions (for standalone queries), avoiding code
// duplication while maintaining transactional integrity where needed.
type queryContext interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// findSimilarInTx finds similar entries within a transaction.
func (s *SQLiteStore) findSimilarInTx(ctx context.Context, qc queryContext, embedding []float32, category string, threshold float64) ([]types.SimilarEntry, error) {
	rows, err := qc.QueryContext(ctx, `
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries
		WHERE category = ? AND embedding IS NOT NULL AND deleted_at IS NULL
	`, category)
	if err != nil {
		return nil, fmt.Errorf("query similar entries: %w", err)
	}
	defer rows.Close()

	var results []types.SimilarEntry
	for rows.Next() {
		entry, err := scanLoreEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		similarity := cosineSimilarity(embedding, entry.Embedding)
		if similarity >= threshold {
			results = append(results, types.SimilarEntry{
				LoreEntry:  *entry,
				Similarity: similarity,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if results == nil {
		results = []types.SimilarEntry{}
	}

	return results, nil
}

// getLoreInTx retrieves a lore entry by ID within a transaction.
func (s *SQLiteStore) getLoreInTx(ctx context.Context, qc queryContext, id string) (*types.LoreEntry, error) {
	row := qc.QueryRowContext(ctx, `
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries
		WHERE id = ? AND deleted_at IS NULL
	`, id)

	entry, err := scanLoreEntry(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan row: %w", err)
	}

	return entry, nil
}

// mergeLoreInTx merges a source entry into target within a transaction.
func (s *SQLiteStore) mergeLoreInTx(ctx context.Context, qc queryContext, targetID string, source types.NewLoreEntry) error {
	target, err := s.getLoreInTx(ctx, qc, targetID)
	if err != nil {
		return err
	}

	newConfidence := math.Min(target.Confidence+ConfidenceBoost, MaxConfidence)
	newContext := appendContext(target.Context, source.Context)
	newSources, _ := addSourceID(target.Sources, source.SourceID)
	sourcesJSON, err := json.Marshal(newSources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = qc.ExecContext(ctx, `
		UPDATE lore_entries
		SET confidence = ?, context = ?, sources = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, newConfidence, newContext, string(sourcesJSON), now, targetID)
	if err != nil {
		return fmt.Errorf("update lore entry: %w", err)
	}

	return nil
}

// insertEntryInTx inserts a new entry within a transaction.
func (s *SQLiteStore) insertEntryInTx(ctx context.Context, qc queryContext, entry types.NewLoreEntry, embedding []float32, hasEmbedding bool) error {
	id := ulid.Make().String()
	sources := []string{entry.SourceID}
	sourcesBytes, err := json.Marshal(sources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	embeddingStatus := "pending"
	var embeddingBlob []byte
	if hasEmbedding {
		embeddingStatus = "complete"
		embeddingBlob = packEmbedding(embedding)
	}

	_, err = qc.ExecContext(ctx, `
		INSERT INTO lore_entries (
			id, content, context, category, confidence,
			embedding, embedding_status, source_id, sources,
			validation_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
	`,
		id,
		entry.Content,
		entry.Context,
		entry.Category,
		entry.Confidence,
		embeddingBlob,
		embeddingStatus,
		entry.SourceID,
		string(sourcesBytes),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}

	return nil
}

// --- Stub implementations for remaining Store interface methods ---
// These will be implemented in future stories.

// MergeLore merges a source entry into an existing target entry.
// It boosts confidence, appends context, and adds source_id to sources array.
func (s *SQLiteStore) MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error {
	// 1. Load target entry
	target, err := s.GetLore(ctx, targetID)
	if err != nil {
		return err // Propagates ErrNotFound
	}

	// 2. Calculate new confidence (cap at 1.0)
	newConfidence := math.Min(target.Confidence+ConfidenceBoost, MaxConfidence)

	// 3. Append context (with truncation if needed)
	newContext := appendContext(target.Context, source.Context)

	// 4. Add source_id to sources array
	newSources, _ := addSourceID(target.Sources, source.SourceID)
	sourcesJSON, err := json.Marshal(newSources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	// 5. Execute UPDATE
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET confidence = ?, context = ?, sources = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, newConfidence, newContext, string(sourcesJSON), now, targetID)
	if err != nil {
		return fmt.Errorf("update lore entry: %w", err)
	}

	return nil
}

// GetMetadata returns store-level metadata.
// TODO: Implement in a future story
func (s *SQLiteStore) GetMetadata(ctx context.Context) (*types.StoreMetadata, error) {
	return nil, ErrNotImplemented
}

// GetSnapshot returns an io.ReadCloser for the current snapshot file.
// The caller is responsible for closing the reader.
// Returns ErrSnapshotNotAvailable if no snapshot has been generated.
func (s *SQLiteStore) GetSnapshot(ctx context.Context) (io.ReadCloser, error) {
	path := s.snapshotPath()
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSnapshotNotAvailable
		}
		return nil, fmt.Errorf("open snapshot: %w", err)
	}
	return file, nil
}

// GetDelta returns lore entries modified since the given time.
// Returns entries updated after `since` (created or modified) and IDs of entries
// deleted after `since`. The AsOf field contains the server time of the query.
// Returns empty arrays (not nil) if no changes exist.
func (s *SQLiteStore) GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error) {
	asOf := time.Now().UTC()
	sinceStr := since.UTC().Format(time.RFC3339)

	// Query 1: Updated/created entries (not deleted)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, context, category, confidence, embedding, embedding_status,
		       source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		FROM lore_entries
		WHERE updated_at > ?
		  AND deleted_at IS NULL
		ORDER BY updated_at ASC
	`, sinceStr)
	if err != nil {
		return nil, fmt.Errorf("query updated entries: %w", err)
	}
	defer rows.Close()

	var lore []types.LoreEntry
	for rows.Next() {
		entry, err := scanLoreEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		lore = append(lore, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	// Query 2: Deleted entry IDs
	deletedRows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM lore_entries
		WHERE deleted_at IS NOT NULL
		  AND deleted_at > ?
		ORDER BY deleted_at ASC
	`, sinceStr)
	if err != nil {
		return nil, fmt.Errorf("query deleted entries: %w", err)
	}
	defer deletedRows.Close()

	var deletedIDs []string
	for deletedRows.Next() {
		var id string
		if err := deletedRows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deleted ID: %w", err)
		}
		deletedIDs = append(deletedIDs, id)
	}
	if err := deletedRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deleted rows: %w", err)
	}

	// Ensure non-nil slices
	if lore == nil {
		lore = []types.LoreEntry{}
	}
	if deletedIDs == nil {
		deletedIDs = []string{}
	}

	return &types.DeltaResult{
		Lore:       lore,
		DeletedIDs: deletedIDs,
		AsOf:       asOf,
	}, nil
}

// GenerateSnapshot generates a point-in-time snapshot of the lore database.
// The snapshot is stored as a SQLite file containing all lore entries with embeddings.
// Returns ErrSnapshotInProgress if generation is already running.
func (s *SQLiteStore) GenerateSnapshot(ctx context.Context) error {
	// Try to acquire snapshot lock (non-blocking)
	if !s.snapshotMu.TryLock() {
		return ErrSnapshotInProgress
	}
	defer s.snapshotMu.Unlock()

	start := time.Now()

	// Ensure snapshot directory exists
	snapshotDir := s.snapshotDir()
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}

	// Generate temp filename for atomic replacement
	tempPath := filepath.Join(snapshotDir, fmt.Sprintf("snapshot_%d.db.tmp", time.Now().UnixNano()))
	finalPath := s.snapshotPath()

	// Use VACUUM INTO for point-in-time backup (non-blocking to writers)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", tempPath))
	if err != nil {
		// Clean up temp file on error
		os.Remove(tempPath)
		return fmt.Errorf("vacuum into snapshot: %w", err)
	}

	// Get snapshot file size for logging
	info, err := os.Stat(tempPath)
	var sizeBytes int64
	if err == nil {
		sizeBytes = info.Size()
	}

	// Atomic rename to final location
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename snapshot: %w", err)
	}

	// Update last snapshot timestamp
	now := time.Now().UTC()
	s.lastSnapshot = &now

	duration := time.Since(start)
	slog.Info("snapshot generated",
		"component", "store",
		"action", "snapshot_complete",
		"duration_ms", duration.Milliseconds(),
		"size_bytes", sizeBytes,
	)

	return nil
}

// GetSnapshotPath returns the filesystem path to the current snapshot.
// Returns ErrSnapshotNotAvailable if no snapshot has been generated.
func (s *SQLiteStore) GetSnapshotPath(ctx context.Context) (string, error) {
	path := s.snapshotPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", ErrSnapshotNotAvailable
	}
	return path, nil
}

// RecordFeedback records feedback entries and adjusts confidence.
// Uses a transaction for atomic batch processing (fail-fast on missing lore).
func (s *SQLiteStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	if len(feedback) == 0 {
		return &types.FeedbackResult{Updates: []types.FeedbackResultUpdate{}}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	updates := make([]types.FeedbackResultUpdate, 0, len(feedback))

	for _, entry := range feedback {
		// Fetch current lore entry
		var id string
		var currentConfidence float64
		var validationCount int

		err := tx.QueryRowContext(ctx, `
			SELECT id, confidence, validation_count
			FROM lore_entries
			WHERE id = ? AND deleted_at IS NULL
		`, entry.LoreID).Scan(&id, &currentConfidence, &validationCount)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("fetch lore entry: %w", err)
		}

		// Calculate new confidence based on feedback type
		previousConfidence := currentConfidence
		var delta float64
		switch entry.Type {
		case "helpful":
			delta = FeedbackHelpfulBoost
		case "incorrect":
			delta = -FeedbackIncorrectPenalty
		case "not_relevant":
			delta = FeedbackNotRelevantDelta
		}

		newConfidence := currentConfidence + delta
		// Apply cap/floor
		if newConfidence > MaxConfidence {
			newConfidence = MaxConfidence
		}
		if newConfidence < MinConfidence {
			newConfidence = MinConfidence
		}

		// Build result update
		update := types.FeedbackResultUpdate{
			LoreID:             entry.LoreID,
			PreviousConfidence: previousConfidence,
			CurrentConfidence:  newConfidence,
		}

		// Update database based on feedback type
		if entry.Type == "helpful" {
			newValidationCount := validationCount + 1
			update.ValidationCount = &newValidationCount

			_, err = tx.ExecContext(ctx, `
				UPDATE lore_entries
				SET confidence = ?,
				    validation_count = validation_count + 1,
				    last_validated_at = ?,
				    updated_at = ?
				WHERE id = ? AND deleted_at IS NULL
			`, newConfidence, nowStr, nowStr, entry.LoreID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE lore_entries
				SET confidence = ?,
				    updated_at = ?
				WHERE id = ? AND deleted_at IS NULL
			`, newConfidence, nowStr, entry.LoreID)
		}

		if err != nil {
			return nil, fmt.Errorf("update lore entry: %w", err)
		}

		updates = append(updates, update)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &types.FeedbackResult{Updates: updates}, nil
}

// DecayConfidence reduces confidence for entries not validated since threshold.
// Entries with last_validated_at < threshold OR last_validated_at IS NULL are decayed.
// Uses a single bulk UPDATE with floor enforcement via max(0.0, confidence - amount).
func (s *SQLiteStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	thresholdStr := threshold.UTC().Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET confidence = max(0.0, confidence - ?),
		    updated_at = ?
		WHERE deleted_at IS NULL
		  AND (last_validated_at < ? OR last_validated_at IS NULL)
	`, amount, now, thresholdStr)

	if err != nil {
		return 0, fmt.Errorf("decay confidence: %w", err)
	}

	return result.RowsAffected()
}
