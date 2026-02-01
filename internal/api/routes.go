package api

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates a new router with all routes configured.
// The mgr parameter provides multi-store support; if nil, store-scoped routes
// will not be available and default store middleware will not be applied.
func NewRouter(h *Handler, mgr StoreGetter) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware (all routes)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(LoggingMiddleware)
	r.Use(middleware.Recoverer)

	// Rate limiter for DELETE operations: 100 deletes max, refill 1 per 100ms
	// This allows burst of 100 deletes, then sustained rate of 10/second
	deleteRateLimiter := NewDeleteRateLimiter(100, 100*time.Millisecond)

	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth required per NFR8)
		r.Get("/health", h.Health)
		r.Get("/stats", h.Stats)

		// Store-scoped public stats (no auth required)
		if mgr != nil {
			r.With(StoreContextMiddleware(mgr)).Get("/stores/{store_id}/stats", h.Stats)
		}

		// Protected routes (auth required)
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(h.apiKey))

			// Store management routes
			r.Get("/stores", h.ListStores)
			r.Post("/stores", h.CreateStore)
			r.Get("/stores/{store_id}", h.GetStoreInfo)
			r.Delete("/stores/{store_id}", h.DeleteStore)

			// Store-scoped lore routes (NEW for Story 7.3)
			if mgr != nil {
				r.Route("/stores/{store_id}/lore", func(r chi.Router) {
					r.Use(StoreContextMiddleware(mgr))

					r.Post("/", h.IngestLore)
					r.Get("/snapshot", h.Snapshot)
					r.Get("/delta", h.Delta)
					r.Post("/feedback", h.Feedback)
					r.With(deleteRateLimiter.Middleware).Delete("/{id}", h.DeleteLore)
				})
			}

			// Backward-compatible lore routes (default store)
			r.Route("/lore", func(r chi.Router) {
				// Apply default store middleware if manager available
				if mgr != nil {
					r.Use(DefaultStoreMiddleware(mgr))
				}

				r.Post("/", h.IngestLore)
				r.Get("/snapshot", h.Snapshot)
				r.Get("/delta", h.Delta)
				r.Post("/feedback", h.Feedback)
				// DELETE has additional rate limiting to prevent abuse
				r.With(deleteRateLimiter.Middleware).Delete("/{id}", h.DeleteLore)
			})
		})
	})

	return r
}
