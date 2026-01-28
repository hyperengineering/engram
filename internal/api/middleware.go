package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

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

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
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
