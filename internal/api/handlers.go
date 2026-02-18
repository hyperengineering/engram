package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/multistore"
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
	store        store.Store
	storeManager *multistore.StoreManager
	embedder     embedding.Embedder
	apiKey       string
	version      string
}

// NewHandler creates a new Handler with store.Store interface
// The storeManager parameter can be nil for backward compatibility.
func NewHandler(s store.Store, mgr *multistore.StoreManager, e embedding.Embedder, apiKey, version string) *Handler {
	return &Handler{
		store:        s,
		storeManager: mgr,
		embedder:     e,
		apiKey:       apiKey,
		version:      version,
	}
}

// Health returns the health status.
// Accepts optional ?store={store_id} query parameter for store-specific stats.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store")

	var stats *types.StoreStats
	var err error

	if storeID != "" {
		// Store-specific health
		if err := multistore.ValidateStoreID(storeID); err != nil {
			WriteProblem(w, r, http.StatusBadRequest, err.Error())
			return
		}

		if h.storeManager == nil {
			WriteProblem(w, r, http.StatusServiceUnavailable, "Multi-store support not configured")
			return
		}

		managed, err := h.storeManager.GetStore(r.Context(), storeID)
		if err != nil {
			if errors.Is(err, multistore.ErrStoreNotFound) {
				WriteProblem(w, r, http.StatusNotFound, "Store not found")
				return
			}
			slog.Error("health store lookup failed", "store_id", storeID, "error", err)
			WriteProblem(w, r, http.StatusInternalServerError, "Internal error")
			return
		}
		stats, err = managed.Store.GetStats(r.Context())
		if err != nil {
			slog.Error("health stats failed", "store_id", storeID, "error", err)
			WriteProblem(w, r, http.StatusInternalServerError, "Internal error")
			return
		}
	} else {
		// Default/global health (backward compatible)
		stats, err = h.store.GetStats(r.Context())
		if err != nil {
			slog.Error("health stats failed", "error", err)
			WriteProblem(w, r, http.StatusInternalServerError, "Internal error")
			return
		}
	}

	resp := types.HealthResponse{
		Status:         "healthy",
		Version:        h.version,
		EmbeddingModel: h.embedder.ModelName(),
		LoreCount:      stats.LoreCount,
		LastSnapshot:   stats.LastSnapshot,
	}

	// Include store_id in response if specified
	if storeID != "" {
		resp.StoreID = storeID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Stats returns extended system metrics for monitoring
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	storeID := StoreIDFromContext(r.Context())
	s := h.getStoreForRequest(r)
	stats, err := s.GetExtendedStats(r.Context())
	if err != nil {
		slog.Error("stats retrieval failed",
			"component", "api",
			"action", "stats_failed",
			"store_id", storeID,
			"error", err,
		)
		WriteProblem(w, r, http.StatusInternalServerError,
			"Internal error retrieving stats")
		return
	}

	// Include store_id in response if accessed via store-scoped route
	if IsStoreScoped(r.Context()) {
		stats.StoreID = storeID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// getStoreForRequest extracts the store from context or falls back to h.store.
// This supports both store-scoped routes (store in context) and backward-compatible
// routes (direct h.store usage when mgr is nil).
func (h *Handler) getStoreForRequest(r *http.Request) store.Store {
	s, err := StoreFromContext(r.Context())
	if err == nil {
		return s
	}
	// Fall back to h.store for backward compatibility
	return h.store
}

// IngestLore handles POST /api/v1/lore and POST /api/v1/stores/{store_id}/lore
func (h *Handler) IngestLore(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	storeID := StoreIDFromContext(r.Context())

	// Get store (from context if available, otherwise fallback to h.store)
	s := h.getStoreForRequest(r)

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
		result, err := s.IngestLore(r.Context(), validEntries)
		if err != nil {
			slog.Error("ingest failed",
				"component", "api",
				"action", "ingest_failed",
				"store_id", storeID,
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
		"store_id", storeID,
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

// Snapshot handles GET /api/v1/lore/snapshot and GET /api/v1/stores/{store_id}/lore/snapshot
// Streams the cached database snapshot as application/octet-stream.
// Returns 503 with Retry-After if no snapshot is available.
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sourceID := extractSourceID(r)
	storeID := StoreIDFromContext(r.Context())

	s := h.getStoreForRequest(r)

	reader, err := s.GetSnapshot(r.Context())
	if errors.Is(err, store.ErrSnapshotNotAvailable) {
		slog.Warn("snapshot not available",
			"component", "api",
			"action", "client_bootstrap_failed",
			"store_id", storeID,
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
			"store_id", storeID,
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
			"store_id", storeID,
			"source_id", sourceID,
			"error", err,
		)
		return
	}

	slog.Info("recall client bootstrap",
		"component", "api",
		"action", "client_bootstrap",
		"store_id", storeID,
		"source_id", sourceID,
		"remote_addr", r.RemoteAddr,
		"bytes_served", bytesWritten,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// Delta handles GET /api/v1/lore/delta and GET /api/v1/stores/{store_id}/lore/delta
// Requires `since` query parameter in RFC3339 format.
// Returns 400 if since is missing or invalid.
// Returns JSON with lore[], deleted_ids[], and as_of.
func (h *Handler) Delta(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sourceID := extractSourceID(r)
	storeID := StoreIDFromContext(r.Context())

	s := h.getStoreForRequest(r)

	// Parse and validate since parameter
	since := r.URL.Query().Get("since")
	if since == "" {
		slog.Warn("delta request missing since",
			"component", "api",
			"action", "client_sync_failed",
			"store_id", storeID,
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
			"store_id", storeID,
			"source_id", sourceID,
			"remote_addr", r.RemoteAddr,
			"since", since,
			"error", err,
		)
		WriteProblem(w, r, http.StatusBadRequest,
			"Invalid since timestamp: must be RFC3339 format (e.g., 2026-01-29T10:00:00Z)")
		return
	}

	result, err := s.GetDelta(r.Context(), sinceTime)
	if err != nil {
		slog.Error("delta retrieval failed",
			"component", "api",
			"action", "client_sync_failed",
			"store_id", storeID,
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
		"store_id", storeID,
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

// --- Store Management API Types ---

// ListStoresResponse is the response for GET /api/v1/stores.
type ListStoresResponse struct {
	Stores []StoreListItem `json:"stores"`
	Total  int             `json:"total"`
}

// StoreListItem represents a store in the list response.
type StoreListItem struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	SchemaVersion int       `json:"schema_version"`
	RecordCount   int64     `json:"record_count"`
	LastAccessed  time.Time `json:"last_accessed"`
	SizeBytes     int64     `json:"size_bytes"`
	Description   string    `json:"description,omitempty"`
}

// StoreInfoResponse is the response for GET /api/v1/stores/{store_id}.
type StoreInfoResponse struct {
	ID            string               `json:"id"`
	Type          string               `json:"type"`
	SchemaVersion int                  `json:"schema_version"`
	Created       time.Time            `json:"created"`
	LastAccessed  time.Time            `json:"last_accessed"`
	Description   string               `json:"description,omitempty"`
	SizeBytes     int64                `json:"size_bytes"`
	Stats         *types.ExtendedStats `json:"stats"`
}

// CreateStoreRequest is the request body for POST /api/v1/stores.
type CreateStoreRequest struct {
	StoreID     string `json:"store_id"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateStoreResponse is the response for POST /api/v1/stores.
type CreateStoreResponse struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	SchemaVersion int       `json:"schema_version"`
	Created       time.Time `json:"created"`
	Description   string    `json:"description,omitempty"`
}

// Feedback handles POST /api/v1/lore/feedback and POST /api/v1/stores/{store_id}/lore/feedback
func (h *Handler) Feedback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	storeID := StoreIDFromContext(r.Context())

	s := h.getStoreForRequest(r)

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
	result, err := s.RecordFeedback(r.Context(), feedbackEntries)
	if err != nil {
		slog.Error("feedback processing failed",
			"component", "api",
			"action", "feedback_failed",
			"store_id", storeID,
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
			"store_id", storeID,
			"source_id", req.SourceID,
			"remote_addr", r.RemoteAddr,
			"duration_ms", duration.Milliseconds(),
			"count", len(req.Feedback),
		)
	}

	slog.Info("feedback processed",
		"component", "api",
		"action", "feedback",
		"store_id", storeID,
		"source_id", req.SourceID,
		"remote_addr", r.RemoteAddr,
		"updated_count", len(result.Updates),
		"skipped_count", len(result.Skipped),
		"duration_ms", duration.Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// DeleteLore handles DELETE /api/v1/lore/{id} and DELETE /api/v1/stores/{store_id}/lore/{id}
func (h *Handler) DeleteLore(w http.ResponseWriter, r *http.Request) {
	storeID := StoreIDFromContext(r.Context())
	id := chi.URLParam(r, "id")

	s := h.getStoreForRequest(r)

	// Validate ULID format using shared validation (consistent with Feedback handler)
	if err := validation.ValidateULID("id", id); err != nil {
		WriteProblem(w, r, http.StatusBadRequest,
			"Invalid lore ID format: must be valid ULID")
		return
	}

	err := s.DeleteLore(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Generic message - don't echo user-supplied ID (Issue #3)
			WriteProblem(w, r, http.StatusNotFound,
				"Lore entry not found")
			return
		}
		slog.Error("delete lore failed",
			"store_id", storeID,
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
		"store_id", storeID,
		"id", id,
		"request_id", GetRequestID(r.Context()),
		"remote_addr", r.RemoteAddr,
	)

	w.WriteHeader(http.StatusNoContent)
}

// --- Store Management Handlers ---

// ListStores handles GET /api/v1/stores
func (h *Handler) ListStores(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.storeManager == nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "Multi-store support not configured")
		return
	}

	storeInfos, err := h.storeManager.ListStores(ctx)
	if err != nil {
		slog.Error("list stores failed", "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error listing stores")
		return
	}

	// Enrich with record counts
	items := make([]StoreListItem, 0, len(storeInfos))
	for _, info := range storeInfos {
		item := StoreListItem{
			ID:            info.ID,
			Type:          info.Type,
			SchemaVersion: info.SchemaVersion,
			LastAccessed:  info.LastAccessed,
			SizeBytes:     info.SizeBytes,
			Description:   info.Description,
		}

		// Get record count from store (if loaded or loadable)
		managed, err := h.storeManager.GetStore(ctx, info.ID)
		if err == nil {
			stats, err := managed.Store.GetStats(ctx)
			if err == nil {
				item.RecordCount = stats.LoreCount
			}
		}

		items = append(items, item)
	}

	// Sort by ID
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	resp := ListStoresResponse{
		Stores: items,
		Total:  len(items),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	slog.Info("stores listed",
		"component", "api",
		"action", "list_stores",
		"count", len(items),
	)
}

// GetStoreInfo handles GET /api/v1/stores/{store_id}
func (h *Handler) GetStoreInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	storeID := chi.URLParam(r, "store_id")

	if h.storeManager == nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "Multi-store support not configured")
		return
	}

	// URL decode the store ID (handles neuralmux%2Fengram -> neuralmux/engram)
	decodedID, err := url.PathUnescape(storeID)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "Invalid store ID encoding")
		return
	}

	// Validate store ID format
	if err := multistore.ValidateStoreID(decodedID); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Get store (this also validates existence)
	managed, err := h.storeManager.GetStore(ctx, decodedID)
	if err != nil {
		if errors.Is(err, multistore.ErrStoreNotFound) {
			WriteProblem(w, r, http.StatusNotFound, "Store not found")
			return
		}
		slog.Error("get store failed", "store_id", decodedID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error")
		return
	}

	// Get extended stats
	stats, err := managed.Store.GetExtendedStats(ctx)
	if err != nil {
		slog.Error("get store stats failed", "store_id", decodedID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error getting stats")
		return
	}

	// Get database file size
	dbPath := filepath.Join(managed.BasePath, "engram.db")
	var sizeBytes int64
	if info, err := os.Stat(dbPath); err == nil {
		sizeBytes = info.Size()
	}

	resp := StoreInfoResponse{
		ID:            decodedID,
		Type:          managed.Type(),
		SchemaVersion: managed.SchemaVersion(ctx),
		Created:       managed.Meta.Created,
		LastAccessed:  managed.Meta.LastAccessed,
		Description:   managed.Meta.Description,
		SizeBytes:     sizeBytes,
		Stats:         stats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	slog.Info("store info retrieved",
		"component", "api",
		"action", "get_store_info",
		"store_id", decodedID,
	)
}

// CreateStore handles POST /api/v1/stores
func (h *Handler) CreateStore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.storeManager == nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "Multi-store support not configured")
		return
	}

	var req CreateStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %s", err.Error()))
		return
	}

	// Validate store ID
	if err := multistore.ValidateStoreID(req.StoreID); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Create store with type
	managed, err := h.storeManager.CreateStore(ctx, req.StoreID, req.Type, req.Description)
	if err != nil {
		if errors.Is(err, multistore.ErrStoreAlreadyExists) {
			WriteProblemConflict(w, r, fmt.Sprintf("Store already exists: %s", req.StoreID))
			return
		}
		if errors.Is(err, multistore.ErrInvalidStoreID) {
			WriteProblem(w, r, http.StatusBadRequest, err.Error())
			return
		}
		slog.Error("create store failed", "store_id", req.StoreID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error creating store")
		return
	}

	resp := CreateStoreResponse{
		ID:            managed.ID,
		Type:          managed.Type(),
		SchemaVersion: managed.SchemaVersion(ctx),
		Created:       managed.Meta.Created,
		Description:   managed.Meta.Description,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)

	slog.Info("store created via API",
		"component", "api",
		"action", "create_store",
		"store_id", req.StoreID,
		"store_type", managed.Type(),
		"request_id", GetRequestID(ctx),
		"remote_addr", r.RemoteAddr,
	)
}

// DeleteStore handles DELETE /api/v1/stores/{store_id}
func (h *Handler) DeleteStore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	storeID := chi.URLParam(r, "store_id")

	if h.storeManager == nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "Multi-store support not configured")
		return
	}

	// URL decode
	decodedID, err := url.PathUnescape(storeID)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "Invalid store ID encoding")
		return
	}

	// Require confirm=true
	if r.URL.Query().Get("confirm") != "true" {
		WriteProblem(w, r, http.StatusBadRequest, "Delete requires confirm=true query parameter")
		return
	}

	// Prevent default store deletion
	if multistore.IsDefaultStore(decodedID) {
		WriteProblemForbidden(w, r, "Cannot delete the default store")
		return
	}

	// Validate store ID
	if err := multistore.ValidateStoreID(decodedID); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Delete store
	if err := h.storeManager.DeleteStore(ctx, decodedID); err != nil {
		if errors.Is(err, multistore.ErrStoreNotFound) {
			WriteProblem(w, r, http.StatusNotFound, "Store not found")
			return
		}
		slog.Error("delete store failed", "store_id", decodedID, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, "Internal error deleting store")
		return
	}

	slog.Info("store deleted via API",
		"component", "api",
		"action", "delete_store",
		"store_id", decodedID,
		"request_id", GetRequestID(ctx),
		"remote_addr", r.RemoteAddr,
	)

	w.WriteHeader(http.StatusNoContent)
}
