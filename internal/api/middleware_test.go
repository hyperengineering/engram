package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

const testAPIKey = "test-secret-key-12345"

// mockHandler is a simple handler that records if it was called
func mockHandler() (http.Handler, *bool) {
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}), &called
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !*called {
		t.Error("handler was not called for valid token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if *called {
		t.Error("handler should not be called for missing header")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if *called {
		t.Error("handler should not be called for invalid token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MalformedHeader_NoBearer(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", testAPIKey) // Missing "Bearer " prefix
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if *called {
		t.Error("handler should not be called for malformed header")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MalformedHeader_EmptyToken(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer ") // Empty token after Bearer
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if *called {
		t.Error("handler should not be called for empty token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_MalformedHeader_WhitespaceOnlyToken(t *testing.T) {
	handler, called := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer    ") // Whitespace only token
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if *called {
		t.Error("handler should not be called for whitespace-only token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_ResponseFormat_RFC7807(t *testing.T) {
	handler, _ := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	// Check Content-Type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %v, want application/problem+json", contentType)
	}

	// Parse response body as RFC 7807 Problem
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal response as RFC 7807: %v", err)
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

func TestAuthMiddleware_NoKeyLeak(t *testing.T) {
	handler, _ := mockHandler()
	middleware := AuthMiddleware(testAPIKey)(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, testAPIKey) {
		t.Error("response body contains the expected API key - security violation!")
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid token", "Bearer abc123", "abc123"},
		{"missing header", "", ""},
		{"no bearer prefix", "abc123", ""},
		{"empty after bearer", "Bearer ", ""},
		{"whitespace after bearer", "Bearer    ", ""},
		{"lowercase bearer", "bearer abc123", ""},
		{"basic auth", "Basic abc123", ""},
		{"token with spaces trimmed", "Bearer  abc123 ", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			got := extractBearerToken(req)
			if got != tt.expected {
				t.Errorf("extractBearerToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestConstantTimeEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		{"equal strings", "abc123", "abc123", true},
		{"different strings", "abc123", "xyz789", false},
		{"different lengths", "abc", "abcdef", false},
		{"empty strings", "", "", true},
		{"one empty", "abc", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constantTimeEqual(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("constantTimeEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestLoggingMiddleware_NoAuthHeaderLeak(t *testing.T) {
	// Capture slog output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	// Create a simple handler
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	// Verify the API key is NOT in log output
	if strings.Contains(logOutput, testAPIKey) {
		t.Error("log output contains the API key - security violation!")
	}

	// Verify the Authorization header value is NOT in log output
	if strings.Contains(logOutput, "Bearer "+testAPIKey) {
		t.Error("log output contains the full Authorization header - security violation!")
	}

	// Verify we did log something (the request)
	if !strings.Contains(logOutput, "request") {
		t.Error("expected 'request' in log output")
	}
}

func TestAuthMiddleware_HealthBypass_ViaRoutes(t *testing.T) {
	// This test verifies that health endpoint is accessible without auth
	// when configured via route groups (not middleware path check).
	// Uses Chi directly to test the routing pattern.

	handlerCalled := make(map[string]bool)

	// Create a router with the same pattern as NewRouter
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes (no auth)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			handlerCalled["health"] = true
			w.WriteHeader(http.StatusOK)
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(testAPIKey))
			r.Post("/lore", func(w http.ResponseWriter, r *http.Request) {
				handlerCalled["lore"] = true
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	// Test health endpoint without auth - should succeed
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint without auth: status = %d, want %d", w.Code, http.StatusOK)
	}
	if !handlerCalled["health"] {
		t.Error("health handler was not called")
	}

	// Reset
	handlerCalled = make(map[string]bool)

	// Test protected endpoint without auth - should fail with 401
	req = httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("protected endpoint without auth: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if handlerCalled["lore"] {
		t.Error("lore handler should not be called without auth")
	}

	// Test protected endpoint with valid auth - should succeed
	req = httptest.NewRequest(http.MethodPost, "/api/v1/lore", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("protected endpoint with valid auth: status = %d, want %d", w.Code, http.StatusOK)
	}
	if !handlerCalled["lore"] {
		t.Error("lore handler was not called with valid auth")
	}
}

// --- RecoveryMiddleware Tests ---

func TestRecoveryMiddleware_NoPanic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	middleware := RecoveryMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "OK" {
		t.Errorf("body = %q, want %q", w.Body.String(), "OK")
	}
}

func TestRecoveryMiddleware_Panic(t *testing.T) {
	// Suppress log output during test
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	})

	middleware := RecoveryMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	// Should return 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	// Should be RFC 7807 format
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("Content-Type = %v, want application/problem+json", contentType)
	}

	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal response as RFC 7807: %v", err)
	}

	if p.Type != "https://engram.dev/errors/internal-error" {
		t.Errorf("type = %v, want https://engram.dev/errors/internal-error", p.Type)
	}
	if p.Status != 500 {
		t.Errorf("status = %d, want 500", p.Status)
	}

	// Should have logged the panic
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "panic recovered") {
		t.Error("expected 'panic recovered' in log output")
	}
	if !strings.Contains(logOutput, "something went wrong") {
		t.Error("expected panic message in log output")
	}
}

// --- Structured Logging Tests (Story 1.7) ---

func TestGetRequestID(t *testing.T) {
	// Use Chi's middleware to inject a request ID
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := GetRequestID(r.Context())
		if reqID == "" {
			t.Error("GetRequestID returned empty string, expected Chi-generated ID")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(reqID))
	})

	router := chi.NewRouter()
	router.Use(chiMiddleware.RequestID)
	router.Get("/test", handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// The body should contain the request ID (non-empty)
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty request ID in response body")
	}
}

func TestGetRequestID_NoContext(t *testing.T) {
	// When no request ID is in context, should return empty string
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqID := GetRequestID(req.Context())
	if reqID != "" {
		t.Errorf("GetRequestID without context = %q, want empty string", reqID)
	}
}

func TestLogLevelForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   slog.Level
	}{
		{200, slog.LevelInfo},
		{201, slog.LevelInfo},
		{204, slog.LevelInfo},
		{301, slog.LevelInfo},
		{304, slog.LevelInfo},
		{400, slog.LevelWarn},
		{401, slog.LevelWarn},
		{404, slog.LevelWarn},
		{422, slog.LevelWarn},
		{499, slog.LevelWarn},
		{500, slog.LevelError},
		{502, slog.LevelError},
		{503, slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			got := logLevelForStatus(tt.status)
			if got != tt.want {
				t.Errorf("logLevelForStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestLoggingMiddleware_RequestIDIncluded(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Use Chi's RequestID middleware to inject request ID
	router := chi.NewRouter()
	router.Use(chiMiddleware.RequestID)
	router.Use(LoggingMiddleware)
	router.Get("/test", innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	logOutput := logBuf.String()

	// Parse JSON to verify request_id field exists and is non-empty
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(logOutput), &logEntry); err != nil {
		t.Fatalf("failed to parse log as JSON: %v", err)
	}

	reqID, ok := logEntry["request_id"]
	if !ok {
		t.Error("log entry missing 'request_id' field")
	}
	if reqID == "" {
		t.Error("request_id is empty")
	}
}

func TestLoggingMiddleware_RemoteAddrIncluded(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(logOutput), &logEntry); err != nil {
		t.Fatalf("failed to parse log as JSON: %v", err)
	}

	remoteAddr, ok := logEntry["remote_addr"]
	if !ok {
		t.Error("log entry missing 'remote_addr' field")
	}
	if remoteAddr != "192.168.1.100:54321" {
		t.Errorf("remote_addr = %v, want 192.168.1.100:54321", remoteAddr)
	}
}

func TestLoggingMiddleware_LogLevelByStatus_2xx(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	if !strings.Contains(logOutput, `"level":"INFO"`) {
		t.Errorf("expected INFO level for 2xx status, got: %s", logOutput)
	}
}

func TestLoggingMiddleware_LogLevelByStatus_4xx(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	if !strings.Contains(logOutput, `"level":"WARN"`) {
		t.Errorf("expected WARN level for 4xx status, got: %s", logOutput)
	}
}

func TestLoggingMiddleware_LogLevelByStatus_5xx(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	if !strings.Contains(logOutput, `"level":"ERROR"`) {
		t.Errorf("expected ERROR level for 5xx status, got: %s", logOutput)
	}
}

func TestLoggingMiddleware_SnakeCaseFields(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(logOutput), &logEntry); err != nil {
		t.Fatalf("failed to parse log as JSON: %v", err)
	}

	// slog standard fields (time, level, msg) are allowed
	slogStandardFields := map[string]bool{"time": true, "level": true, "msg": true}

	for key := range logEntry {
		if slogStandardFields[key] {
			continue
		}
		// Check for snake_case: no hyphens, no uppercase letters
		if strings.Contains(key, "-") {
			t.Errorf("field %q contains hyphen, should be snake_case", key)
		}
		if key != strings.ToLower(key) {
			t.Errorf("field %q contains uppercase letters, should be snake_case", key)
		}
	}

	// Verify expected fields exist
	expectedFields := []string{"request_id", "method", "path", "status", "duration_ms", "remote_addr"}
	for _, field := range expectedFields {
		if _, ok := logEntry[field]; !ok {
			t.Errorf("missing expected field: %s", field)
		}
	}
}

func TestLoggingMiddleware_MessageChanged(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := LoggingMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	logOutput := logBuf.String()

	if !strings.Contains(logOutput, `"msg":"request completed"`) {
		t.Errorf("expected message 'request completed', got: %s", logOutput)
	}
}

func TestRecoveryMiddleware_PanicNoLeak(t *testing.T) {
	// Suppress log output during test
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, nil)
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	secretMessage := "super-secret-database-password-12345"
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(secretMessage)
	})

	middleware := RecoveryMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/lore", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	// Response body should NOT contain the secret
	body := w.Body.String()
	if strings.Contains(body, secretMessage) {
		t.Error("response body contains secret panic message - security violation!")
	}

	// Response detail should be generic
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if p.Detail != "Internal Server Error" {
		t.Errorf("detail = %q, want generic 'Internal Server Error'", p.Detail)
	}

	// But logs SHOULD contain the secret (for debugging)
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, secretMessage) {
		t.Error("expected secret in logs for debugging purposes")
	}
}
