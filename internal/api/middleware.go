// Package api provides HTTP handlers and middleware for the Engram API.
//
// =============================================================================
// OPERATION LOGGING CONVENTIONS
// =============================================================================
// All operation logs MUST use snake_case field names.
//
// Canonical Fields:
//
//	action      - Operation type: ingest, merge, deduplicate, feedback,
//	              snapshot, decay, sync
//	lore_id     - Lore entry identifier (ULID string)
//	source_id   - Source identifier (ULID string)
//	category    - Lore category: snippet, preference, procedure, context
//	component   - Originating package: api, store, embedding, worker
//	duration_ms - Operation timing in milliseconds
//	error       - Error message (for ERROR level logs)
//
// Usage Examples:
//
//	// Successful operation
//	slog.Info("lore ingested",
//	    "action", "ingest",
//	    "lore_id", entry.ID,
//	    "source_id", sourceID,
//	    "category", entry.Category,
//	    "component", "api",
//	    "duration_ms", elapsed.Milliseconds(),
//	)
//
//	// Failed operation
//	slog.Error("embedding generation failed",
//	    "action", "embed",
//	    "lore_id", entry.ID,
//	    "error", err.Error(),
//	    "component", "embedding",
//	)
//
// =============================================================================
package api

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// GetRequestID extracts the request ID from context.
// Returns empty string if no request ID is present.
func GetRequestID(ctx context.Context) string {
	return middleware.GetReqID(ctx)
}

// logLevelForStatus returns the appropriate log level based on HTTP status code.
func logLevelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// extractBearerToken extracts the token from Authorization header.
// Returns empty string for missing/malformed headers.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	// Must start with "Bearer " (case-sensitive per RFC 6750)
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}

	token := strings.TrimSpace(auth[len(prefix):])
	return token
}

// constantTimeEqual compares two strings using constant-time comparison
// to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// AuthMiddleware validates Bearer token using constant-time comparison.
// Returns 401 RFC 7807 Problem Details on auth failure.
// MUST NOT include expected API key in logs or responses.
func AuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if !constantTimeEqual(token, apiKey) {
				slog.Warn("auth failure",
					"path", r.URL.Path,
					"method", r.Method,
					"remote_ip", r.RemoteAddr,
				)
				WriteProblem(w, r, http.StatusUnauthorized, "Missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs HTTP requests with structured fields.
// Emits log at INFO for 2xx/3xx, WARN for 4xx, ERROR for 5xx.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		level := logLevelForStatus(wrapped.statusCode)
		slog.Log(r.Context(), level, "request completed",
			"request_id", GetRequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RecoveryMiddleware catches panics and returns 500 Problem Details.
// Panic details are logged but never exposed to the client.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic recovered",
					"error", recovered,
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
					"method", r.Method,
				)
				WriteProblem(w, r, http.StatusInternalServerError, "Internal Server Error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
