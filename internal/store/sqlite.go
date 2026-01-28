package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/hyperengineering/engram/internal/types"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// ErrNotImplemented is returned for Store interface methods not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// SQLiteStore represents the SQLite-backed lore database.
type SQLiteStore struct {
	db *sql.DB
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

	return &SQLiteStore{db: db}, nil
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
		LastSnapshot: nil, // Snapshot tracking not yet implemented
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

func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
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

// IngestLore stores new lore entries with pending embedding status.
func (s *SQLiteStore) IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error) {
	if len(entries) == 0 {
		return &types.IngestResult{Accepted: 0, Merged: 0, Rejected: 0, Errors: []string{}}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO lore_entries (
			id, content, context, category, confidence,
			embedding, embedding_status, source_id, sources,
			validation_count, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, NULL, 'pending', ?, ?, 0, ?, ?)
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	for _, entry := range entries {
		id := ulid.Make().String()
		sources := []string{entry.SourceID}
		sourcesBytes, err := json.Marshal(sources)
		if err != nil {
			return nil, fmt.Errorf("marshal sources: %w", err)
		}
		sourcesJSON := string(sourcesBytes)

		_, err = stmt.ExecContext(ctx,
			id,
			entry.Content,
			entry.Context,
			entry.Category,
			entry.Confidence,
			entry.SourceID,
			sourcesJSON,
			nowStr,
			nowStr,
		)
		if err != nil {
			return nil, fmt.Errorf("insert entry: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &types.IngestResult{
		Accepted: len(entries),
		Merged:   0,
		Rejected: 0,
		Errors:   []string{},
	}, nil
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

// --- Stub implementations for remaining Store interface methods ---
// These will be implemented in future stories.

// FindSimilar finds lore entries similar to the given embedding (new interface signature).
// TODO: Implement in Story 3.1 (Cosine Similarity Detection)
func (s *SQLiteStore) FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.LoreEntry, error) {
	return nil, ErrNotImplemented
}

// MergeLore merges a source entry into an existing target entry.
// TODO: Implement in Story 3.2 (Lore Merge Strategy)
func (s *SQLiteStore) MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error {
	return ErrNotImplemented
}

// GetMetadata returns store-level metadata.
// TODO: Implement in a future story
func (s *SQLiteStore) GetMetadata(ctx context.Context) (*types.StoreMetadata, error) {
	return nil, ErrNotImplemented
}

// GetSnapshot returns a reader for the current snapshot.
// TODO: Implement in Story 4.2 (Snapshot Serving Endpoint)
func (s *SQLiteStore) GetSnapshot(ctx context.Context) (io.ReadCloser, error) {
	return nil, ErrNotImplemented
}

// GetDelta returns lore entries modified since the given time.
// TODO: Implement in Story 4.3 (Delta Sync Endpoint)
func (s *SQLiteStore) GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error) {
	return nil, ErrNotImplemented
}

// GenerateSnapshot generates a new snapshot file.
// TODO: Implement in Story 4.1 (Snapshot Generation Worker)
func (s *SQLiteStore) GenerateSnapshot(ctx context.Context) error {
	return ErrNotImplemented
}

// GetSnapshotPath returns the path to the current snapshot file.
// TODO: Implement in Story 4.1 (Snapshot Generation Worker)
func (s *SQLiteStore) GetSnapshotPath(ctx context.Context) (string, error) {
	return "", ErrNotImplemented
}

// RecordFeedback records feedback entries and adjusts confidence.
// TODO: Implement in Story 5.1 (Feedback Processing Endpoint)
func (s *SQLiteStore) RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error) {
	return nil, ErrNotImplemented
}

// DecayConfidence reduces confidence for entries not validated since threshold.
// TODO: Implement in Story 5.2 (Confidence Decay Worker)
func (s *SQLiteStore) DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error) {
	return 0, ErrNotImplemented
}
