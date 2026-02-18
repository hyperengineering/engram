package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// UpsertRow inserts or updates a row in the specified table.
// Used by domain plugins during sync replay.
func (s *SQLiteStore) UpsertRow(ctx context.Context, tableName string, entityID string, payload []byte) error {
	if tableName != "lore_entries" {
		return fmt.Errorf("unsupported table: %s", tableName)
	}

	var row loreRow
	if err := json.Unmarshal(payload, &row); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	if row.ID != entityID {
		return fmt.Errorf("payload ID %q does not match entity ID %q", row.ID, entityID)
	}

	sourcesJSON, err := json.Marshal(row.Sources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	var embeddingBlob []byte
	if len(row.Embedding) > 0 {
		embeddingBlob = packEmbedding(row.Embedding)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	embeddingStatus := row.EmbeddingStatus
	if embeddingStatus == "" {
		embeddingStatus = "pending"
	}

	createdAt := row.CreatedAt
	if createdAt == "" {
		createdAt = now
	}

	// INSERT OR REPLACE in SQLite deletes the existing row then inserts
	// a new one (rather than updating in-place). This is safe for
	// lore_entries because it has no child FK relationships. Multi-table
	// plugins should use INSERT ... ON CONFLICT ... DO UPDATE instead.
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO lore_entries (
			id, content, context, category, confidence, embedding, embedding_status,
			source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.ID,
		row.Content,
		row.Context,
		row.Category,
		row.Confidence,
		embeddingBlob,
		embeddingStatus,
		row.SourceID,
		string(sourcesJSON),
		row.ValidationCount,
		createdAt,
		now,
		formatNullableTime(row.DeletedAt),
		formatNullableTime(row.LastValidatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert lore entry: %w", err)
	}

	return nil
}

// DeleteRow soft-deletes a row from the specified table.
// Used by domain plugins during sync replay.
func (s *SQLiteStore) DeleteRow(ctx context.Context, tableName string, entityID string) error {
	if tableName != "lore_entries" {
		return fmt.Errorf("unsupported table: %s", tableName)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, now, entityID)
	if err != nil {
		return fmt.Errorf("soft delete lore entry: %w", err)
	}

	return nil
}

// QueueEmbedding marks an entry for embedding generation.
// Only updates entries that don't already have an embedding and aren't already pending.
func (s *SQLiteStore) QueueEmbedding(ctx context.Context, entryID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE lore_entries
		SET embedding_status = 'pending'
		WHERE id = ? AND embedding IS NULL AND embedding_status != 'pending'
	`, entryID)
	if err != nil {
		return fmt.Errorf("queue embedding: %w", err)
	}
	return nil
}

// loreRow mirrors LorePayload for JSON unmarshaling in UpsertRow.
type loreRow struct {
	ID              string    `json:"id"`
	Content         string    `json:"content"`
	Context         string    `json:"context"`
	Category        string    `json:"category"`
	Confidence      float64   `json:"confidence"`
	Embedding       []float32 `json:"embedding"`
	EmbeddingStatus string    `json:"embedding_status"`
	SourceID        string    `json:"source_id"`
	Sources         []string  `json:"sources"`
	ValidationCount int       `json:"validation_count"`
	CreatedAt       string    `json:"created_at"`
	UpdatedAt       string    `json:"updated_at"`
	DeletedAt       *string   `json:"deleted_at"`
	LastValidatedAt *string   `json:"last_validated_at"`
}

// formatNullableTime converts a string pointer to a sql-friendly format.
func formatNullableTime(t *string) any {
	if t == nil || *t == "" {
		return nil
	}
	return *t
}

// --- Transaction-scoped replay functions ---

// BeginTx starts a database transaction.
func (s *SQLiteStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// AppendChangeLogBatchTx appends entries within an existing transaction.
// Returns the highest assigned sequence number.
func (s *SQLiteStore) AppendChangeLogBatchTx(ctx context.Context, tx *sql.Tx, entries []engramsync.ChangeLogEntry) (int64, error) {
	var maxSeq int64

	for i := range entries {
		result, err := tx.ExecContext(ctx, insertChangeLogSQL,
			changeLogArgs(&entries[i])...)
		if err != nil {
			return 0, fmt.Errorf("insert change log entry: %w", err)
		}

		seq, err := result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("get last insert id: %w", err)
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}

	return maxSeq, nil
}

// UpsertRowTx performs an upsert within a transaction.
func UpsertRowTx(ctx context.Context, tx *sql.Tx, tableName, entityID string, payload []byte) error {
	if tableName != "lore_entries" {
		return fmt.Errorf("unsupported table: %s", tableName)
	}

	var row loreRow
	if err := json.Unmarshal(payload, &row); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	if row.ID != entityID {
		return fmt.Errorf("payload ID %q does not match entity ID %q", row.ID, entityID)
	}

	sourcesJSON, err := json.Marshal(row.Sources)
	if err != nil {
		return fmt.Errorf("marshal sources: %w", err)
	}

	var embeddingBlob []byte
	if len(row.Embedding) > 0 {
		embeddingBlob = packEmbedding(row.Embedding)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	embeddingStatus := row.EmbeddingStatus
	if embeddingStatus == "" {
		embeddingStatus = "pending"
	}

	createdAt := row.CreatedAt
	if createdAt == "" {
		createdAt = now
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO lore_entries (
			id, content, context, category, confidence, embedding, embedding_status,
			source_id, sources, validation_count, created_at, updated_at, deleted_at, last_validated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.ID,
		row.Content,
		row.Context,
		row.Category,
		row.Confidence,
		embeddingBlob,
		embeddingStatus,
		row.SourceID,
		string(sourcesJSON),
		row.ValidationCount,
		createdAt,
		now,
		formatNullableTime(row.DeletedAt),
		formatNullableTime(row.LastValidatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert lore entry: %w", err)
	}

	return nil
}

// DeleteRowTx performs a soft delete within a transaction.
func DeleteRowTx(ctx context.Context, tx *sql.Tx, tableName, entityID string) error {
	if tableName != "lore_entries" {
		return fmt.Errorf("unsupported table: %s", tableName)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `
		UPDATE lore_entries SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, now, entityID)
	if err != nil {
		return fmt.Errorf("soft delete lore entry: %w", err)
	}
	return nil
}

// QueueEmbeddingTx queues embedding within a transaction.
func QueueEmbeddingTx(ctx context.Context, tx *sql.Tx, entryID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE lore_entries SET embedding_status = 'pending'
		WHERE id = ? AND embedding IS NULL AND embedding_status != 'pending'
	`, entryID)
	if err != nil {
		return fmt.Errorf("queue embedding: %w", err)
	}
	return nil
}
