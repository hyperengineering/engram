package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
	"github.com/hyperengineering/engram/internal/validation"
)

// HeaderRecallSourceID is the header name for client identification.
// Recall clients should send this header to enable per-client tracking in logs.
const HeaderRecallSourceID = "X-Recall-Source-ID"

// extractSourceID returns the client source ID from the request header.
// Returns "unknown" if the header is not present or empty.
func extractSourceID(r *http.Request) string {
	sourceID := r.Header.Get(HeaderRecallSourceID)
	if sourceID == "" {
		return "unknown"
	}
	return sourceID
}

// Handler implements the API handlers
type Handler struct {
	store    store.Store
	embedder embedding.Embedder
	apiKey   string
	version  string
}

// NewHandler creates a new Handler with store.Store interface
func NewHandler(s store.Store, e embedding.Embedder, apiKey, version string) *Handler {
	return &Handler{
		store:    s,
		embedder: e,
		apiKey:   apiKey,
		version:  version,
	}
}

// Health returns the health status
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetStats(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	resp := types.HealthResponse{
		Status:         "healthy",
		Version:        h.version,
		EmbeddingModel: h.embedder.ModelName(),
		LoreCount:      stats.LoreCount,
		LastSnapshot:   stats.LastSnapshot,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Stats returns extended system metrics for monitoring
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetExtendedStats(r.Context())
	if err != nil {
		slog.Error("stats retrieval failed",
			"component", "api",
			"action", "stats_failed",
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError,
			"Internal error retrieving stats")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// IngestLore handles POST /api/v1/lore
func (h *Handler) IngestLore(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req types.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %s", err.Error()))
		return
	}

	// Validate request-level fields (rejects entire request if invalid)
	reqErrors := validation.ValidateIngestRequest(req)
	if len(reqErrors) > 0 {
		WriteProblemWithErrors(w, r, "Request contains invalid fields", reqErrors)
		return
	}

	// Validate each entry, separate valid from invalid (partial acceptance)
	var validEntries []types.NewLoreEntry
	var allErrors []string

	for i, lore := range req.Lore {
		errs := validation.ValidateLoreEntry(i, lore)
		if len(errs) > 0 {
			for _, err := range errs {
				allErrors = append(allErrors, fmt.Sprintf("%s: %s", err.Field, err.Message))
			}
			continue
		}
		validEntries = append(validEntries, types.NewLoreEntry{
			Content:    lore.Content,
			Context:    lore.Context,
			Category:   string(lore.Category),
			Confidence: lore.Confidence,
			SourceID:   req.SourceID,
		})
	}

	var accepted, merged int
	if len(validEntries) > 0 {
		result, err := h.store.IngestLore(r.Context(), validEntries)
		if err != nil {
			slog.Error("ingest failed",
				"component", "api",
				"action", "ingest_failed",
				"source_id", req.SourceID,
				"remote_addr", r.RemoteAddr,
				"error", err,
			)
			MapStoreError(w, r, err)
			return
		}
		accepted = result.Accepted
		merged = result.Merged
	}

	rejected := len(req.Lore) - len(validEntries)

	slog.Info("lore ingested",
		"component", "api",
		"action", "ingest",
		"source_id", req.SourceID,
		"remote_addr", r.RemoteAddr,
		"accepted", accepted,
		"merged", merged,
		"rejected", rejected,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	resp := types.IngestResult{
		Accepted: accepted,
		Merged:   merged,
		Rejected: rejected,
		Errors:   allErrors,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Snapshot handles GET /api/v1/lore/snapshot
// Streams the cached database snapshot as application/octet-stream.
// Returns 503 with Retry-After if no snapshot is available.
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sourceID := extractSourceID(r)

	reader, err := h.store.GetSnapshot(r.Context())
	if errors.Is(err, store.ErrSnapshotNotAvailable) {
		slog.Warn("snapshot not available",
			"component", "api",
			"action", "client_bootstrap_failed",
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"reason", "snapshot_not_ready",
		)
		w.Header().Set("Retry-After", "60")
		WriteProblem(w, r, http.StatusServiceUnavailable,
			"Snapshot not yet available. Please retry after the indicated interval.")
		return
	}
	if err != nil {
		slog.Error("snapshot retrieval failed",
			"component", "api",
			"action", "client_bootstrap_failed",
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError,
			"Internal error retrieving snapshot")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	bytesWritten, err := io.Copy(w, reader)
	if err != nil {
		slog.Debug("snapshot stream interrupted",
			"component", "api",
			"source_id", sourceID,
			"error", err,
		)
		return
	}

	slog.Info("recall client bootstrap",
		"component", "api",
		"action", "client_bootstrap",
		"source_id", sourceID,
		"remote_addr", r.RemoteAddr,
		"bytes_served", bytesWritten,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// Delta handles GET /api/v1/lore/delta
// Requires `since` query parameter in RFC3339 format.
// Returns 400 if since is missing or invalid.
// Returns JSON with lore[], deleted_ids[], and as_of.
func (h *Handler) Delta(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sourceID := extractSourceID(r)

	// Parse and validate since parameter
	since := r.URL.Query().Get("since")
	if since == "" {
		slog.Warn("delta request missing since",
			"component", "api",
			"action", "client_sync_failed",
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"reason", "missing_since",
		)
		WriteProblem(w, r, http.StatusBadRequest,
			"Missing required query parameter: since")
		return
	}

	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		slog.Warn("delta request invalid since",
			"component", "api",
			"action", "client_sync_failed",
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"since", since,
			"error", err,
		)
		WriteProblem(w, r, http.StatusBadRequest,
			"Invalid since timestamp: must be RFC3339 format (e.g., 2026-01-29T10:00:00Z)")
		return
	}

	result, err := h.store.GetDelta(r.Context(), sinceTime)
	if err != nil {
		slog.Error("delta retrieval failed",
			"component", "api",
			"action", "client_sync_failed",
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"since", since,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError,
			"Internal error retrieving delta")
		return
	}

	slog.Info("recall client sync",
		"component", "api",
		"action", "client_sync",
		"source_id", sourceID,
		"remote_addr", r.RemoteAddr,
		"since", since,
		"lore_count", len(result.Lore),
		"deleted_count", len(result.DeletedIDs),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// feedbackRequest matches the API contract for feedback submission.
// Uses snake_case JSON tags to match external interface specification.
type feedbackRequest struct {
	SourceID string             `json:"source_id"`
	Feedback []feedbackReqEntry `json:"feedback"`
}

// feedbackReqEntry represents a single feedback entry in the request.
// JSON tags use snake_case per API contract.
type feedbackReqEntry struct {
	LoreID string `json:"lore_id"`
	Type   string `json:"type"`
}

// Feedback handles POST /api/v1/lore/feedback
func (h *Handler) Feedback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Parse JSON body
	var req feedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %s", err.Error()))
		return
	}

	// Validate request-level fields
	reqErrors := validation.ValidateFeedbackRequest(req.SourceID, len(req.Feedback))
	if len(reqErrors) > 0 {
		WriteProblemWithErrors(w, r, "Request contains invalid fields", reqErrors)
		return
	}

	// Validate each feedback entry
	var allErrors []validation.ValidationError
	for i, entry := range req.Feedback {
		errs := validation.ValidateFeedbackEntry(i, entry.LoreID, entry.Type)
		allErrors = append(allErrors, errs...)
	}
	if len(allErrors) > 0 {
		WriteProblemWithErrors(w, r, "Request contains invalid fields", allErrors)
		return
	}

	// Convert to store format
	feedbackEntries := make([]types.FeedbackEntry, len(req.Feedback))
	for i, entry := range req.Feedback {
		feedbackEntries[i] = types.FeedbackEntry{
			LoreID:   entry.LoreID,
			Type:     entry.Type,
			SourceID: req.SourceID,
		}
	}

	// Call store
	result, err := h.store.RecordFeedback(r.Context(), feedbackEntries)
	if err != nil {
		slog.Error("feedback processing failed",
			"component", "api",
			"action", "feedback_failed",
			"source_id", req.SourceID,
			"remote_addr", r.RemoteAddr,
			"error", err,
		)
		MapStoreError(w, r, err)
		return
	}

	// Performance logging
	duration := time.Since(start)
	if duration > 500*time.Millisecond {
		slog.Warn("feedback processing exceeded performance target",
			"component", "api",
			"action", "feedback",
			"source_id", req.SourceID,
			"remote_addr", r.RemoteAddr,
			"duration_ms", duration.Milliseconds(),
			"count", len(req.Feedback),
		)
	}

	slog.Info("feedback processed",
		"component", "api",
		"action", "feedback",
		"source_id", req.SourceID,
		"remote_addr", r.RemoteAddr,
		"count", len(result.Updates),
		"duration_ms", duration.Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// DeleteLore handles DELETE /api/v1/lore/{id}
func (h *Handler) DeleteLore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Validate ULID format using shared validation (consistent with Feedback handler)
	if err := validation.ValidateULID("id", id); err != nil {
		WriteProblem(w, r, http.StatusBadRequest,
			"Invalid lore ID format: must be valid ULID")
		return
	}

	err := h.store.DeleteLore(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Generic message - don't echo user-supplied ID (Issue #3)
			WriteProblem(w, r, http.StatusNotFound,
				"Lore entry not found")
			return
		}
		slog.Error("delete lore failed",
			"error", err,
			"id", id,
			"request_id", GetRequestID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
		WriteProblem(w, r, http.StatusInternalServerError,
			"Internal Server Error")
		return
	}

	// Audit log with client identification (Issue #1)
	slog.Info("lore deleted",
		"component", "api",
		"action", "delete_lore",
		"id", id,
		"request_id", GetRequestID(r.Context()),
		"remote_addr", r.RemoteAddr,
	)

	w.WriteHeader(http.StatusNoContent)
}
