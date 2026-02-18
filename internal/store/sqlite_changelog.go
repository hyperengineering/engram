package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	engramsync "github.com/hyperengineering/engram/internal/sync"
)

const insertChangeLogSQL = `
	INSERT INTO change_log (table_name, entity_id, operation, payload, source_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?)`

// changeLogArgs returns the SQL arguments for inserting a ChangeLogEntry.
func changeLogArgs(e *engramsync.ChangeLogEntry) []any {
	return []any{
		e.TableName, e.EntityID, e.Operation,
		nullablePayload(e.Payload), e.SourceID,
		e.CreatedAt.Format(time.RFC3339Nano),
	}
}

// AppendChangeLog appends a single entry to the change log.
// Returns the assigned sequence number.
func (s *SQLiteStore) AppendChangeLog(ctx context.Context, entry *engramsync.ChangeLogEntry) (int64, error) {
	result, err := s.db.ExecContext(ctx, insertChangeLogSQL, changeLogArgs(entry)...)
	if err != nil {
		return 0, fmt.Errorf("append change log: %w", err)
	}
	return result.LastInsertId()
}

// AppendChangeLogBatch appends multiple entries atomically.
// Returns the highest assigned sequence number.
func (s *SQLiteStore) AppendChangeLogBatch(ctx context.Context, entries []engramsync.ChangeLogEntry) (int64, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var highestSeq int64
	for i := range entries {
		result, err := tx.ExecContext(ctx, insertChangeLogSQL, changeLogArgs(&entries[i])...)
		if err != nil {
			return 0, fmt.Errorf("append change log entry %d: %w", i, err)
		}
		highestSeq, err = result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("get last insert id: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}
	return highestSeq, nil
}

// GetChangeLogAfter returns entries with sequence > afterSeq, up to limit.
func (s *SQLiteStore) GetChangeLogAfter(ctx context.Context, afterSeq int64, limit int) ([]engramsync.ChangeLogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, table_name, entity_id, operation, payload, source_id, created_at, received_at
		FROM change_log
		WHERE sequence > ?
		ORDER BY sequence ASC
		LIMIT ?
	`, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("query change log: %w", err)
	}
	defer rows.Close()

	entries := make([]engramsync.ChangeLogEntry, 0)
	for rows.Next() {
		var e engramsync.ChangeLogEntry
		var payload sql.NullString
		var createdAt, receivedAt string

		if err := rows.Scan(&e.Sequence, &e.TableName, &e.EntityID, &e.Operation,
			&payload, &e.SourceID, &createdAt, &receivedAt); err != nil {
			return nil, fmt.Errorf("scan change log entry: %w", err)
		}

		if payload.Valid {
			e.Payload = json.RawMessage(payload.String)
		}
		var parseErr error
		if e.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt); parseErr != nil {
			slog.Warn("change_log: failed to parse created_at", "value", createdAt, "error", parseErr)
		}
		if e.ReceivedAt, parseErr = time.Parse(time.RFC3339Nano, receivedAt); parseErr != nil {
			slog.Warn("change_log: failed to parse received_at", "value", receivedAt, "error", parseErr)
		}

		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetLatestSequence returns the highest sequence number in the change log.
// Returns 0 if the change log is empty.
func (s *SQLiteStore) GetLatestSequence(ctx context.Context) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(sequence) FROM change_log`).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("get latest sequence: %w", err)
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

// CheckPushIdempotency checks if a push_id has been processed.
// Returns the cached response and true if found, nil and false otherwise.
func (s *SQLiteStore) CheckPushIdempotency(ctx context.Context, pushID string) ([]byte, bool, error) {
	var response string
	var expiresAt string

	err := s.db.QueryRowContext(ctx, `
		SELECT response, expires_at FROM push_idempotency WHERE push_id = ?
	`, pushID).Scan(&response, &expiresAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("check idempotency: %w", err)
	}

	// Check expiration
	expires, parseErr := time.Parse(time.RFC3339Nano, expiresAt)
	if parseErr != nil {
		slog.Warn("push_idempotency: failed to parse expires_at", "value", expiresAt, "error", parseErr)
	}
	if time.Now().After(expires) {
		return nil, false, nil
	}

	return []byte(response), true, nil
}

// RecordPushIdempotency records a processed push for idempotency.
func (s *SQLiteStore) RecordPushIdempotency(ctx context.Context, pushID, storeID string, response []byte, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO push_idempotency (push_id, store_id, response, expires_at)
		VALUES (?, ?, ?, ?)
	`, pushID, storeID, string(response), expiresAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record push idempotency: %w", err)
	}
	return nil
}

// CleanExpiredIdempotency removes expired idempotency entries.
// Returns the number of entries removed.
func (s *SQLiteStore) CleanExpiredIdempotency(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM push_idempotency WHERE expires_at < ?
	`, time.Now().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("clean expired idempotency: %w", err)
	}
	return result.RowsAffected()
}

// GetSyncMeta retrieves a sync metadata value by key.
func (s *SQLiteStore) GetSyncMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `
		SELECT value FROM sync_meta WHERE key = ?
	`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("sync meta key %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("get sync meta: %w", err)
	}
	return value, nil
}

// SetSyncMeta sets a sync metadata value.
func (s *SQLiteStore) SetSyncMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO sync_meta (key, value) VALUES (?, ?)
	`, key, value)
	if err != nil {
		return fmt.Errorf("set sync meta: %w", err)
	}
	return nil
}

// CompactChangeLog removes old change_log entries, keeping only the latest per entity.
// Exports all removed entries to auditDir before deletion.
// Returns (exported, deleted, error).
func (s *SQLiteStore) CompactChangeLog(ctx context.Context, cutoff time.Time, auditDir string) (exported int64, deleted int64, err error) {
	cutoffStr := cutoff.UTC().Format(time.RFC3339)

	// 1. Query entries eligible for compaction (older than cutoff)
	rows, err := s.db.QueryContext(ctx, `
		SELECT sequence, table_name, entity_id, operation, payload, source_id, created_at, received_at
		FROM change_log
		WHERE created_at < ?
		ORDER BY sequence ASC
	`, cutoffStr)
	if err != nil {
		return 0, 0, fmt.Errorf("query change_log: %w", err)
	}
	defer rows.Close()

	// 2. Collect entries and track latest per entity
	type auditEntry struct {
		Sequence   int64   `json:"sequence"`
		TableName  string  `json:"table_name"`
		EntityID   string  `json:"entity_id"`
		Operation  string  `json:"operation"`
		Payload    *string `json:"payload,omitempty"`
		SourceID   string  `json:"source_id"`
		CreatedAt  string  `json:"created_at"`
		ReceivedAt string  `json:"received_at"`
	}

	var entries []auditEntry
	latestSeqPerEntity := make(map[string]int64) // "table:entity" -> max sequence

	for rows.Next() {
		var e auditEntry
		var payload sql.NullString
		if err := rows.Scan(&e.Sequence, &e.TableName, &e.EntityID, &e.Operation, &payload, &e.SourceID, &e.CreatedAt, &e.ReceivedAt); err != nil {
			return 0, 0, fmt.Errorf("scan row: %w", err)
		}
		if payload.Valid {
			e.Payload = &payload.String
		}
		entries = append(entries, e)

		key := e.TableName + ":" + e.EntityID
		if e.Sequence > latestSeqPerEntity[key] {
			latestSeqPerEntity[key] = e.Sequence
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate rows: %w", err)
	}

	if len(entries) == 0 {
		return 0, 0, nil
	}

	// 3. Identify entries to delete (not latest per entity, not delete tombstones)
	var toDelete []int64
	for _, e := range entries {
		key := e.TableName + ":" + e.EntityID

		// Keep delete tombstones
		if e.Operation == engramsync.OperationDelete {
			continue
		}

		// Keep if this is the latest entry for this entity
		if e.Sequence == latestSeqPerEntity[key] {
			continue
		}

		toDelete = append(toDelete, e.Sequence)
	}

	if len(toDelete) == 0 {
		return 0, 0, nil
	}

	// 4. Export entries to audit file
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		return 0, 0, fmt.Errorf("create audit dir: %w", err)
	}

	auditFile := filepath.Join(auditDir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(auditFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, 0, fmt.Errorf("open audit file: %w", err)
	}

	toDeleteSet := make(map[int64]bool, len(toDelete))
	for _, seq := range toDelete {
		toDeleteSet[seq] = true
	}

	encoder := json.NewEncoder(f)
	for _, e := range entries {
		if toDeleteSet[e.Sequence] {
			if err := encoder.Encode(e); err != nil {
				f.Close()
				return 0, 0, fmt.Errorf("write audit entry: %w", err)
			}
		}
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return 0, 0, fmt.Errorf("sync audit file: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, 0, fmt.Errorf("close audit file: %w", err)
	}

	exported = int64(len(toDelete))

	// 5. Delete compacted entries in transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Batch deletes to respect SQLite parameter limit (999)
	const batchSize = 999
	for i := 0; i < len(toDelete); i += batchSize {
		end := i + batchSize
		if end > len(toDelete) {
			end = len(toDelete)
		}
		batch := toDelete[i:end]

		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for j, seq := range batch {
			placeholders[j] = "?"
			args[j] = seq
		}

		query := fmt.Sprintf("DELETE FROM change_log WHERE sequence IN (%s)", strings.Join(placeholders, ","))
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, 0, fmt.Errorf("delete change_log: %w", err)
		}
		affected, _ := result.RowsAffected()
		deleted += affected
	}

	// 6. Update sync_meta
	now := time.Now().UTC().Format(time.RFC3339)
	maxDeletedSeq := toDelete[len(toDelete)-1]

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO sync_meta (key, value) VALUES (?, ?)
	`, engramsync.SyncMetaLastCompactionSeq, fmt.Sprintf("%d", maxDeletedSeq))
	if err != nil {
		return 0, 0, fmt.Errorf("update last_compaction_seq: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO sync_meta (key, value) VALUES (?, ?)
	`, engramsync.SyncMetaLastCompactionAt, now)
	if err != nil {
		return 0, 0, fmt.Errorf("update last_compaction_at: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit transaction: %w", err)
	}

	return exported, deleted, nil
}

// SetLastCompaction records compaction metadata in sync_meta.
func (s *SQLiteStore) SetLastCompaction(ctx context.Context, sequence int64, timestamp time.Time) error {
	if err := s.SetSyncMeta(ctx, engramsync.SyncMetaLastCompactionSeq, fmt.Sprintf("%d", sequence)); err != nil {
		return err
	}
	return s.SetSyncMeta(ctx, engramsync.SyncMetaLastCompactionAt, timestamp.UTC().Format(time.RFC3339))
}

// nullablePayload converts a json.RawMessage to a sql-friendly value.
// Returns nil for empty/null payloads, string otherwise.
func nullablePayload(p json.RawMessage) any {
	if len(p) == 0 {
		return nil
	}
	return string(p)
}
