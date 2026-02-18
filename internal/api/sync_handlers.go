package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/store"
	engramsync "github.com/hyperengineering/engram/internal/sync"
)

const (
	// IdempotencyTTL is the duration to cache push responses.
	IdempotencyTTL = 24 * time.Hour

	// MaxPushEntries is the maximum entries per push request.
	MaxPushEntries = 1000
)

// SyncPush handles POST /api/v1/stores/{store_id}/sync/push
func (h *Handler) SyncPush(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()
	storeID := StoreIDFromContext(ctx)

	// 1. Get managed store
	if h.storeManager == nil {
		WriteProblem(w, r, http.StatusNotFound, "Store not found")
		return
	}
	managed, err := h.storeManager.GetStore(ctx, storeID)
	if err != nil {
		WriteProblem(w, r, http.StatusNotFound, "Store not found")
		return
	}

	// 2. Parse request
	var req engramsync.PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %s", err))
		return
	}

	// 3. Validate request structure
	if err := validatePushRequest(req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// 4. Check idempotency
	cachedResp, found, err := managed.Store.CheckPushIdempotency(ctx, req.PushID)
	if err != nil {
		slog.Error("idempotency check failed", "store_id", storeID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error")
		return
	}
	if found {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Idempotent-Replay", "true")
		w.Write(cachedResp)
		slog.Info("push idempotent replay",
			"component", "api",
			"action", "sync_push_replay",
			"store_id", storeID,
			"push_id", req.PushID,
		)
		return
	}

	// 5. Check schema version
	serverVersion := managed.SchemaVersion(ctx)
	if req.SchemaVersion > serverVersion {
		writeSchemaMismatch(w, r, req.SchemaVersion, serverVersion)
		return
	}

	// 6. Get domain plugin
	p, _ := plugin.Get(managed.Type())

	// 7. Validate entries via plugin
	orderedEntries, err := p.ValidatePush(ctx, req.Entries)
	if err != nil {
		var validationErrs plugin.ValidationErrors
		if errors.As(err, &validationErrs) {
			writePushValidationErrors(w, validationErrs)
			return
		}
		slog.Error("plugin validation failed", "store_id", storeID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Validation error")
		return
	}

	// 8. Execute replay in transaction
	remoteSeq, err := executePushTransaction(ctx, managed.Store, p, req.SourceID, orderedEntries)
	if err != nil {
		slog.Error("push transaction failed",
			"component", "api",
			"action", "sync_push_failed",
			"store_id", storeID,
			"push_id", req.PushID,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError, "Push failed")
		return
	}

	// 9. Build success response
	resp := engramsync.PushResponse{
		Accepted:       len(orderedEntries),
		RemoteSequence: remoteSeq,
	}

	respBytes, _ := json.Marshal(resp)

	// 10. Cache response for idempotency
	if err := managed.Store.RecordPushIdempotency(ctx, req.PushID, storeID, respBytes, IdempotencyTTL); err != nil {
		slog.Warn("failed to cache idempotency", "store_id", storeID, "push_id", req.PushID, "error", err)
	}

	// 11. Return response
	w.Header().Set("Content-Type", "application/json")
	w.Write(respBytes)

	slog.Info("push completed",
		"component", "api",
		"action", "sync_push",
		"store_id", storeID,
		"push_id", req.PushID,
		"source_id", req.SourceID,
		"entries", len(orderedEntries),
		"remote_sequence", remoteSeq,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// txCapableStore is the interface for stores that support transactions.
// Used via type assertion to avoid leaking *sql.Tx into the Store interface.
type txCapableStore interface {
	BeginTx(ctx context.Context) (*sql.Tx, error)
	AppendChangeLogBatchTx(ctx context.Context, tx *sql.Tx, entries []engramsync.ChangeLogEntry) (int64, error)
}

// executePushTransaction replays entries and records to change log atomically.
func executePushTransaction(
	ctx context.Context,
	s store.Store,
	p plugin.DomainPlugin,
	sourceID string,
	entries []engramsync.ChangeLogEntry,
) (int64, error) {
	sqlStore, ok := s.(txCapableStore)
	if !ok {
		return 0, fmt.Errorf("store does not support transactions")
	}

	tx, err := sqlStore.BeginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create transaction-scoped replay store
	replayStore := &txReplayStore{tx: tx}

	// Replay entries via plugin
	if err := p.OnReplay(ctx, replayStore, entries); err != nil {
		return 0, fmt.Errorf("replay entries: %w", err)
	}

	// Stamp entries with source_id and current time
	now := time.Now().UTC()
	for i := range entries {
		entries[i].SourceID = sourceID
		entries[i].ReceivedAt = now
	}

	// Append to change log
	maxSeq, err := sqlStore.AppendChangeLogBatchTx(ctx, tx, entries)
	if err != nil {
		return 0, fmt.Errorf("append change log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return maxSeq, nil
}

// txReplayStore wraps a transaction for plugin replay.
type txReplayStore struct {
	tx *sql.Tx
}

func (s *txReplayStore) UpsertRow(ctx context.Context, tableName, entityID string, payload []byte) error {
	return store.UpsertRowTx(ctx, s.tx, tableName, entityID, payload)
}

func (s *txReplayStore) DeleteRow(ctx context.Context, tableName, entityID string) error {
	return store.DeleteRowTx(ctx, s.tx, tableName, entityID)
}

func (s *txReplayStore) QueueEmbedding(ctx context.Context, entryID string) error {
	return store.QueueEmbeddingTx(ctx, s.tx, entryID)
}

// validatePushRequest validates the push request structure.
func validatePushRequest(req engramsync.PushRequest) error {
	if req.PushID == "" {
		return fmt.Errorf("push_id is required")
	}
	if req.SourceID == "" {
		return fmt.Errorf("source_id is required")
	}
	if req.SchemaVersion < 1 {
		return fmt.Errorf("schema_version must be >= 1")
	}
	if len(req.Entries) == 0 {
		return fmt.Errorf("entries array is required")
	}
	if len(req.Entries) > MaxPushEntries {
		return fmt.Errorf("entries exceeds maximum of %d", MaxPushEntries)
	}
	return nil
}

// writeSchemaMismatch writes a 409 response for schema version mismatch.
func writeSchemaMismatch(w http.ResponseWriter, r *http.Request, clientVersion, serverVersion int) {
	resp := struct {
		Type          string `json:"type"`
		Title         string `json:"title"`
		Status        int    `json:"status"`
		Detail        string `json:"detail"`
		Instance      string `json:"instance"`
		ClientVersion int    `json:"client_version"`
		ServerVersion int    `json:"server_version"`
	}{
		Type:          "https://engram.dev/errors/schema-mismatch",
		Title:         "Schema Version Mismatch",
		Status:        http.StatusConflict,
		Detail:        fmt.Sprintf("Client schema version %d is ahead of server version %d. Engram upgrade required.", clientVersion, serverVersion),
		Instance:      r.URL.Path,
		ClientVersion: clientVersion,
		ServerVersion: serverVersion,
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(resp)
}

// writePushValidationErrors writes the push error response.
func writePushValidationErrors(w http.ResponseWriter, errs plugin.ValidationErrors) {
	pushErrors := make([]engramsync.PushError, len(errs.Errors))
	for i, e := range errs.Errors {
		pushErrors[i] = engramsync.PushError{
			Sequence:  e.Sequence,
			TableName: e.TableName,
			EntityID:  e.EntityID,
			Code:      engramsync.PushErrorValidation,
			Message:   e.Message,
		}
	}

	resp := engramsync.PushErrorResponse{
		Accepted: 0,
		Errors:   pushErrors,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(resp)
}

// SyncDelta handles GET /api/v1/stores/{store_id}/sync/delta
func (h *Handler) SyncDelta(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx := r.Context()
	storeID := StoreIDFromContext(ctx)

	// 1. Get store
	s := h.getStoreForRequest(r)
	if s == nil {
		WriteProblem(w, r, http.StatusNotFound, "Store not found")
		return
	}

	// 2. Parse query parameters
	req, err := parseDeltaRequest(r)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// 3. Query change log
	entries, err := s.GetChangeLogAfter(ctx, req.After, req.Limit)
	if err != nil {
		slog.Error("delta query failed",
			"component", "api",
			"action", "sync_delta_failed",
			"store_id", storeID,
			"after", req.After,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError, "Failed to retrieve delta")
		return
	}

	// 4. Get latest sequence for pagination info
	latestSeq, err := s.GetLatestSequence(ctx)
	if err != nil {
		slog.Error("get latest sequence failed",
			"component", "api",
			"action", "sync_delta_failed",
			"store_id", storeID,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError, "Failed to retrieve delta")
		return
	}

	// 5. Calculate pagination info
	var lastSeq int64
	if len(entries) > 0 {
		lastSeq = entries[len(entries)-1].Sequence
	} else {
		lastSeq = req.After
	}

	hasMore := len(entries) == req.Limit && lastSeq < latestSeq

	// 6. Build response
	resp := engramsync.DeltaResponse{
		Entries:        entries,
		LastSequence:   lastSeq,
		LatestSequence: latestSeq,
		HasMore:        hasMore,
	}

	// Ensure entries is [] not null in JSON
	if resp.Entries == nil {
		resp.Entries = []engramsync.ChangeLogEntry{}
	}

	// 7. Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	slog.Info("sync delta served",
		"component", "api",
		"action", "sync_delta",
		"store_id", storeID,
		"after", req.After,
		"limit", req.Limit,
		"entries_returned", len(entries),
		"last_sequence", lastSeq,
		"latest_sequence", latestSeq,
		"has_more", hasMore,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// parseDeltaRequest extracts and validates query parameters for GET /sync/delta.
func parseDeltaRequest(r *http.Request) (engramsync.DeltaRequest, error) {
	var req engramsync.DeltaRequest

	// Parse after (required)
	afterStr := r.URL.Query().Get("after")
	if afterStr == "" {
		return req, fmt.Errorf("missing required query parameter: after")
	}

	after, err := strconv.ParseInt(afterStr, 10, 64)
	if err != nil {
		return req, fmt.Errorf("invalid after parameter: must be an integer")
	}
	if after < 0 {
		return req, fmt.Errorf("invalid after parameter: must be >= 0")
	}
	req.After = after

	// Parse limit (optional)
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		req.Limit = engramsync.DefaultDeltaLimit
	} else {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return req, fmt.Errorf("invalid limit parameter: must be an integer")
		}
		if limit < 1 {
			return req, fmt.Errorf("invalid limit parameter: must be >= 1")
		}
		if limit > engramsync.MaxDeltaLimit {
			limit = engramsync.MaxDeltaLimit
		}
		req.Limit = limit
	}

	return req, nil
}
