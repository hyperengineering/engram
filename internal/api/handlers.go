package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/types"
)

// HealthStore defines the store operations needed by the health endpoint
type HealthStore interface {
	GetStats(ctx context.Context) (*types.StoreStats, error)
}

// legacyStore defines the legacy store operations (used until full interface migration)
type legacyStore interface {
	Count() (int, error)
	FindSimilar(embedding []float32, threshold float32, limit int) ([]types.Lore, error)
	Record(lore types.Lore, embedding []float32) (*types.Lore, error)
}

// Handler implements the API handlers
type Handler struct {
	store       HealthStore
	legacyStore legacyStore
	embedder    embedding.Embedder
	apiKey      string
	version     string
}

// NewHandler creates a new Handler with store.Store interface
func NewHandler(s store.Store, e embedding.Embedder, apiKey, version string) *Handler {
	return &Handler{
		store:       s,
		legacyStore: nil, // Full interface stores don't have legacy methods
		embedder:    e,
		apiKey:      apiKey,
		version:     version,
	}
}

// NewHandlerWithLegacyStore creates a Handler for SQLiteStore (temporary until full migration)
func NewHandlerWithLegacyStore(s *store.SQLiteStore, e embedding.Embedder, apiKey, version string) *Handler {
	return &Handler{
		store:       s,
		legacyStore: s,
		embedder:    e,
		apiKey:      apiKey,
		version:     version,
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
	if h.legacyStore == nil {
		http.Error(w, "Not implemented", http.StatusNotImplemented)
		return
	}

	var req types.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := types.IngestResponse{
		Errors: []string{},
	}

	for _, lore := range req.Lore {
		lore.SourceID = req.SourceID

		// Generate embedding
		emb, err := h.embedder.Embed(r.Context(), lore.Content)
		if err != nil {
			resp.Errors = append(resp.Errors, err.Error())
			resp.Rejected++
			continue
		}

		// Check for duplicates
		similar, err := h.legacyStore.FindSimilar(emb, 0.92, 1)
		if err != nil {
			resp.Errors = append(resp.Errors, err.Error())
			resp.Rejected++
			continue
		}

		if len(similar) > 0 && similar[0].Category == lore.Category {
			// Merge with existing
			resp.Merged++
			continue
		}

		// Store new lore
		_, err = h.legacyStore.Record(lore, emb)
		if err != nil {
			resp.Errors = append(resp.Errors, err.Error())
			resp.Rejected++
			continue
		}

		resp.Accepted++
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
