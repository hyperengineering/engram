package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/validation"
)

// Problem represents an RFC 7807 Problem Details response.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

// problemTypes maps HTTP status codes to RFC 7807 type URIs and titles.
var problemTypes = map[int]struct {
	typeURI string
	title   string
}{
	http.StatusUnauthorized: {
		typeURI: "https://engram.dev/errors/unauthorized",
		title:   "Unauthorized",
	},
	http.StatusBadRequest: {
		typeURI: "https://engram.dev/errors/bad-request",
		title:   "Bad Request",
	},
	http.StatusNotFound: {
		typeURI: "https://engram.dev/errors/not-found",
		title:   "Not Found",
	},
	http.StatusInternalServerError: {
		typeURI: "https://engram.dev/errors/internal-error",
		title:   "Internal Server Error",
	},
	http.StatusUnprocessableEntity: {
		typeURI: "https://engram.dev/errors/validation-error",
		title:   "Validation Error",
	},
	http.StatusServiceUnavailable: {
		typeURI: "https://engram.dev/errors/service-unavailable",
		title:   "Service Unavailable",
	},
	http.StatusConflict: {
		typeURI: "https://engram.dev/errors/conflict",
		title:   "Conflict",
	},
	http.StatusForbidden: {
		typeURI: "https://engram.dev/errors/forbidden",
		title:   "Forbidden",
	},
	http.StatusTooManyRequests: {
		typeURI: "https://engram.dev/errors/rate-limit",
		title:   "Too Many Requests",
	},
}

// WriteProblem writes an RFC 7807 Problem Details response.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	pt, ok := problemTypes[status]
	if !ok {
		pt = struct {
			typeURI string
			title   string
		}{
			typeURI: "https://engram.dev/errors/unknown",
			title:   http.StatusText(status),
		}
	}

	p := Problem{
		Type:     pt.typeURI,
		Title:    pt.title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Error("failed to encode problem response", "error", err)
	}
}

// ProblemWithErrors extends Problem with validation error details.
type ProblemWithErrors struct {
	Problem
	Errors []validation.ValidationError `json:"errors,omitempty"`
}

// WriteProblemWithErrors writes a 422 Problem Details response with field errors.
func WriteProblemWithErrors(w http.ResponseWriter, r *http.Request, detail string, errs []validation.ValidationError) {
	pt := problemTypes[http.StatusUnprocessableEntity]

	p := ProblemWithErrors{
		Problem: Problem{
			Type:     pt.typeURI,
			Title:    pt.title,
			Status:   http.StatusUnprocessableEntity,
			Detail:   detail,
			Instance: r.URL.Path,
		},
		Errors: errs,
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Error("failed to encode problem response", "error", err)
	}
}

// WriteProblemConflict writes a 409 Conflict problem response.
func WriteProblemConflict(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, http.StatusConflict, detail)
}

// WriteProblemForbidden writes a 403 Forbidden problem response.
func WriteProblemForbidden(w http.ResponseWriter, r *http.Request, detail string) {
	WriteProblem(w, r, http.StatusForbidden, detail)
}

// MapStoreError converts domain errors to Problem Details responses.
func MapStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		WriteProblem(w, r, http.StatusNotFound, "Resource not found")
	case errors.Is(err, store.ErrDuplicateLore):
		WriteProblem(w, r, http.StatusConflict, "Duplicate entry")
	case errors.Is(err, store.ErrEmbeddingUnavailable):
		WriteProblem(w, r, http.StatusServiceUnavailable, "Embedding service unavailable")
	default:
		// Never expose internal error details to client
		WriteProblem(w, r, http.StatusInternalServerError, "Internal Server Error")
	}
}
