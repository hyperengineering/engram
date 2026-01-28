package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperengineering/engram/internal/store"
	"github.com/hyperengineering/engram/internal/validation"
)

func TestProblem_JSONSerialization(t *testing.T) {
	p := Problem{
		Type:     "https://engram.dev/errors/unauthorized",
		Title:    "Unauthorized",
		Status:   401,
		Detail:   "Missing or invalid API key",
		Instance: "/api/v1/lore",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal Problem: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Problem JSON: %v", err)
	}

	// Verify all RFC 7807 fields present
	if decoded["type"] != "https://engram.dev/errors/unauthorized" {
		t.Errorf("type = %v, want %v", decoded["type"], "https://engram.dev/errors/unauthorized")
	}
	if decoded["title"] != "Unauthorized" {
		t.Errorf("title = %v, want %v", decoded["title"], "Unauthorized")
	}
	if decoded["status"] != float64(401) {
		t.Errorf("status = %v, want %v", decoded["status"], 401)
	}
	if decoded["detail"] != "Missing or invalid API key" {
		t.Errorf("detail = %v, want %v", decoded["detail"], "Missing or invalid API key")
	}
	if decoded["instance"] != "/api/v1/lore" {
		t.Errorf("instance = %v, want %v", decoded["instance"], "/api/v1/lore")
	}
}

func TestWriteProblem_ContentType(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)

	WriteProblem(w, r, http.StatusUnauthorized, "Missing or invalid API key")

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %v, want application/problem+json", contentType)
	}
}

func TestWriteProblem_StatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)

	WriteProblem(w, r, http.StatusUnauthorized, "Missing or invalid API key")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWriteProblem_BodyFormat(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)

	WriteProblem(w, r, http.StatusUnauthorized, "Missing or invalid API key")

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	if p.Type != "https://engram.dev/errors/unauthorized" {
		t.Errorf("type = %v, want https://engram.dev/errors/unauthorized", p.Type)
	}
	if p.Title != "Unauthorized" {
		t.Errorf("title = %v, want Unauthorized", p.Title)
	}
	if p.Status != 401 {
		t.Errorf("status = %d, want 401", p.Status)
	}
	if p.Detail != "Missing or invalid API key" {
		t.Errorf("detail = %v, want 'Missing or invalid API key'", p.Detail)
	}
	if p.Instance != "/api/v1/lore" {
		t.Errorf("instance = %v, want /api/v1/lore", p.Instance)
	}
}

// --- ProblemWithErrors Tests ---

func TestProblemWithErrors_JSONSerialization(t *testing.T) {
	p := ProblemWithErrors{
		Problem: Problem{
			Type:     "https://engram.dev/errors/validation-error",
			Title:    "Validation Error",
			Status:   422,
			Detail:   "Request contains invalid fields",
			Instance: "/api/v1/lore",
		},
		Errors: []validation.ValidationError{
			{Field: "lore[0].content", Message: "exceeds maximum length of 4000 characters"},
			{Field: "lore[1].category", Message: "must be one of: DEBUGGING_INSIGHT"},
		},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal ProblemWithErrors: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	// Verify errors array is present
	errorsArr, ok := decoded["errors"].([]interface{})
	if !ok {
		t.Fatalf("errors field missing or not array: %v", decoded["errors"])
	}
	if len(errorsArr) != 2 {
		t.Errorf("len(errors) = %d, want 2", len(errorsArr))
	}

	// Verify first error
	firstErr, ok := errorsArr[0].(map[string]interface{})
	if !ok {
		t.Fatalf("errors[0] not an object")
	}
	if firstErr["field"] != "lore[0].content" {
		t.Errorf("errors[0].field = %v, want lore[0].content", firstErr["field"])
	}
}

func TestWriteProblemWithErrors_422(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)

	errors := []validation.ValidationError{
		{Field: "content", Message: "is required"},
	}
	WriteProblemWithErrors(w, r, "Request contains invalid fields", errors)

	// Check status code
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	// Check Content-Type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %v, want application/problem+json", contentType)
	}

	// Parse response
	var p ProblemWithErrors
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if p.Type != "https://engram.dev/errors/validation-error" {
		t.Errorf("type = %v, want https://engram.dev/errors/validation-error", p.Type)
	}
	if p.Title != "Validation Error" {
		t.Errorf("title = %v, want Validation Error", p.Title)
	}
	if len(p.Errors) != 1 {
		t.Errorf("len(errors) = %d, want 1", len(p.Errors))
	}
}

// --- MapStoreError Tests ---

func TestMapStoreError_NotFound(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/lore/123", nil)

	MapStoreError(w, r, store.ErrNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/not-found" {
		t.Errorf("type = %v, want https://engram.dev/errors/not-found", p.Type)
	}
}

func TestMapStoreError_Duplicate(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)

	MapStoreError(w, r, store.ErrDuplicateLore)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/conflict" {
		t.Errorf("type = %v, want https://engram.dev/errors/conflict", p.Type)
	}
}

func TestMapStoreError_EmbeddingUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/recall", nil)

	MapStoreError(w, r, store.ErrEmbeddingUnavailable)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/service-unavailable" {
		t.Errorf("type = %v, want https://engram.dev/errors/service-unavailable", p.Type)
	}
}

func TestMapStoreError_Unknown(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)

	MapStoreError(w, r, errors.New("some unknown error"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/internal-error" {
		t.Errorf("type = %v, want https://engram.dev/errors/internal-error", p.Type)
	}
	// Should not expose internal error details
	if p.Detail != "Internal Server Error" {
		t.Errorf("detail = %v, want 'Internal Server Error' (no leak)", p.Detail)
	}
}

// --- WriteProblem status code tests ---

func TestWriteProblem_422_Type(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)

	WriteProblem(w, r, http.StatusUnprocessableEntity, "validation failed")

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/validation-error" {
		t.Errorf("type = %v, want https://engram.dev/errors/validation-error", p.Type)
	}
}

func TestWriteProblem_503_Type(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/recall", nil)

	WriteProblem(w, r, http.StatusServiceUnavailable, "embedding service down")

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/service-unavailable" {
		t.Errorf("type = %v, want https://engram.dev/errors/service-unavailable", p.Type)
	}
}

func TestWriteProblem_409_Type(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)

	WriteProblem(w, r, http.StatusConflict, "duplicate entry")

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Type != "https://engram.dev/errors/conflict" {
		t.Errorf("type = %v, want https://engram.dev/errors/conflict", p.Type)
	}
}
