package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// nullablePayload converts a json.RawMessage to a sql-friendly value.
// Returns nil for empty/null payloads, string otherwise.
func nullablePayload(p json.RawMessage) any {
	if len(p) == 0 {
		return nil
	}
	return string(p)
}
