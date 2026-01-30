package api

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates a new router with all routes configured
func NewRouter(h *Handler) *chi.Mux {
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

		// Protected routes (auth required)
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(h.apiKey))
			r.Post("/lore", h.IngestLore)
			r.Get("/lore/snapshot", h.Snapshot)
			r.Get("/lore/delta", h.Delta)
			r.Post("/lore/feedback", h.Feedback)
			// DELETE has additional rate limiting to prevent abuse
			r.With(deleteRateLimiter.Middleware).Delete("/lore/{id}", h.DeleteLore)
		})
	})

	return r
}
