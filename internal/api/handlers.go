package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
)

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

// IngestLore handles POST /api/v1/lore
func (h *Handler) IngestLore(w http.ResponseWriter, r *http.Request) {
	var req types.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert legacy Lore to NewLoreEntry
	entries := make([]types.NewLoreEntry, len(req.Lore))
	for i, lore := range req.Lore {
		entries[i] = types.NewLoreEntry{
			Content:    lore.Content,
			Context:    lore.Context,
			Category:   string(lore.Category),
			Confidence: lore.Confidence,
			SourceID:   req.SourceID,
		}
	}

	// Store entries (embeddings generated asynchronously by worker)
	result, err := h.store.IngestLore(r.Context(), entries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := types.IngestResponse{
		Accepted: result.Accepted,
		Merged:   result.Merged,
		Rejected: result.Rejected,
		Errors:   result.Errors,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Snapshot handles GET /api/v1/lore/snapshot
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement snapshot generation
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// Delta handles GET /api/v1/lore/delta
func (h *Handler) Delta(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement delta sync
	resp := types.DeltaResponse{
		Lore:       []types.Lore{},
		DeletedIDs: []string{},
		AsOf:       time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Feedback handles POST /api/v1/lore/feedback
func (h *Handler) Feedback(w http.ResponseWriter, r *http.Request) {
	var req types.FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := types.FeedbackResponse{
		Updates: []types.FeedbackUpdate{},
	}

	// TODO: Implement feedback processing

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
