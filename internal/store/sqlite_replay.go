package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hyperengineering/engram/internal/plugin"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

// execContext is satisfied by both *sql.DB and *sql.Tx.
type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UpsertRow inserts or updates a row in the specified table.
// Used by domain plugins during sync replay.
func (s *SQLiteStore) UpsertRow(ctx context.Context, tableName string, entityID string, payload []byte) error {
	// Check for registered table schema first (generic path)
	if schema, ok := plugin.GetTableSchema(tableName); ok {
		return genericUpsertRow(ctx, s.db, schema, entityID, payload)
	}

	// Legacy hardcoded path for lore_entries (Recall backward compat)
	if tableName == "lore_entries" {
		return upsertLoreEntry(ctx, s.db, entityID, payload)
	}

	return fmt.Errorf("unsupported table: %s", tableName)
}

// DeleteRow soft-deletes or hard-deletes a row from the specified table.
// Used by domain plugins during sync replay.
func (s *SQLiteStore) DeleteRow(ctx context.Context, tableName string, entityID string) error {
	// Check for registered table schema first (generic path)
	if schema, ok := plugin.GetTableSchema(tableName); ok {
		return genericDeleteRow(ctx, s.db, schema, entityID)
	}

	// Legacy hardcoded path for lore_entries (Recall backward compat)
	if tableName == "lore_entries" {
		return deleteLoreEntry(ctx, s.db, entityID)
	}

	return fmt.Errorf("unsupported table: %s", tableName)
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

// --- Legacy lore_entries-specific functions ---

// upsertLoreEntry performs the lore_entries-specific upsert with specialized
// struct deserialization, embedding handling, and sources JSON marshaling.
func upsertLoreEntry(ctx context.Context, execer execContext, entityID string, payload []byte) error {
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
	_, err = execer.ExecContext(ctx, `
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

// deleteLoreEntry performs lore_entries-specific soft delete.
func deleteLoreEntry(ctx context.Context, execer execContext, entityID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := execer.ExecContext(ctx, `
		UPDATE lore_entries
		SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, now, entityID)
	if err != nil {
		return fmt.Errorf("soft delete lore entry: %w", err)
	}

	return nil
}

// --- Generic table replay functions ---

// genericUpsertRow inserts or updates a row using the registered table schema.
// Uses INSERT ... ON CONFLICT(id) DO UPDATE SET to avoid cascade-deleting FK children.
func genericUpsertRow(ctx context.Context, execer execContext, schema plugin.TableSchema, entityID string, payload []byte) error {
	// 1. Unmarshal payload into map
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	// 2. Verify entity ID matches payload "id"
	if payloadID, ok := data["id"].(string); ok && payloadID != entityID {
		return fmt.Errorf("payload ID %q does not match entity ID %q", payloadID, entityID)
	}

	// 3. Set updated_at to now (if column exists in schema)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hasUpdatedAt := false
	for _, col := range schema.Columns {
		if col == "updated_at" {
			hasUpdatedAt = true
			break
		}
	}
	if hasUpdatedAt {
		data["updated_at"] = now
	}

	// 4. Build INSERT ... ON CONFLICT(id) DO UPDATE SET ... SQL
	cols := schema.Columns
	placeholders := make([]string, len(cols))
	updateClauses := make([]string, 0, len(cols)-1)
	args := make([]interface{}, len(cols))

	for i, col := range cols {
		placeholders[i] = "?"
		args[i] = mapValueToSQL(data[col])
		if col != "id" {
			updateClauses = append(updateClauses, fmt.Sprintf("%s = excluded.%s", col, col))
		}
	}

	sqlStr := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(id) DO UPDATE SET %s",
		schema.Name,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(updateClauses, ", "),
	)

	_, err := execer.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("upsert %s row %s: %w", schema.Name, entityID, err)
	}
	return nil
}

// genericDeleteRow performs soft or hard delete based on the schema's SoftDelete flag.
func genericDeleteRow(ctx context.Context, execer execContext, schema plugin.TableSchema, entityID string) error {
	if schema.SoftDelete {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		sqlStr := fmt.Sprintf(
			"UPDATE %s SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL",
			schema.Name,
		)
		_, err := execer.ExecContext(ctx, sqlStr, now, now, entityID)
		if err != nil {
			return fmt.Errorf("soft delete %s row %s: %w", schema.Name, entityID, err)
		}
	} else {
		sqlStr := fmt.Sprintf("DELETE FROM %s WHERE id = ?", schema.Name)
		_, err := execer.ExecContext(ctx, sqlStr, entityID)
		if err != nil {
			return fmt.Errorf("delete %s row %s: %w", schema.Name, entityID, err)
		}
	}
	return nil
}

// mapValueToSQL converts Go interface{} values to SQL-safe parameters.
func mapValueToSQL(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}, []interface{}:
		// JSON objects/arrays -> store as TEXT (JSON string)
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return v
	}
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
	// Check for registered table schema first (generic path)
	if schema, ok := plugin.GetTableSchema(tableName); ok {
		return genericUpsertRow(ctx, tx, schema, entityID, payload)
	}

	// Legacy hardcoded path for lore_entries
	if tableName == "lore_entries" {
		return upsertLoreEntry(ctx, tx, entityID, payload)
	}

	return fmt.Errorf("unsupported table: %s", tableName)
}

// DeleteRowTx performs a delete within a transaction.
func DeleteRowTx(ctx context.Context, tx *sql.Tx, tableName, entityID string) error {
	// Check for registered table schema first (generic path)
	if schema, ok := plugin.GetTableSchema(tableName); ok {
		return genericDeleteRow(ctx, tx, schema, entityID)
	}

	// Legacy hardcoded path for lore_entries
	if tableName == "lore_entries" {
		return deleteLoreEntry(ctx, tx, entityID)
	}

	return fmt.Errorf("unsupported table: %s", tableName)
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
