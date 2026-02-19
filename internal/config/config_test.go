package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Helper to clear all config-related env vars
func clearEnv(t *testing.T) {
	t.Helper()
	envVars := []string{
		"ENGRAM_PORT",
		"ENGRAM_READ_TIMEOUT",
		"ENGRAM_WRITE_TIMEOUT",
		"ENGRAM_SHUTDOWN_TIMEOUT",
		"ENGRAM_DB_PATH",
		"OPENAI_API_KEY",
		"ENGRAM_EMBEDDING_MODEL",
		"ENGRAM_API_KEY",
		"ENGRAM_SNAPSHOT_INTERVAL",
		"ENGRAM_DECAY_INTERVAL",
		"ENGRAM_EMBEDDING_RETRY_INTERVAL",
		"ENGRAM_EMBEDDING_RETRY_MAX_ATTEMPTS",
		"ENGRAM_LOG_LEVEL",
		"ENGRAM_LOG_FORMAT",
		"ENGRAM_CONFIG_PATH",
		"ENGRAM_DEV_MODE",
		"ENGRAM_DEDUPLICATION_ENABLED",
		"ENGRAM_SIMILARITY_THRESHOLD",
		"ENGRAM_STORES_ROOT",
		"ENGRAM_ADDRESS", // legacy
		"ENGRAM_SNAPSHOT_BUCKET",
		"ENGRAM_S3_ENDPOINT",
		"ENGRAM_S3_REGION",
		"ENGRAM_S3_ACCESS_KEY",
		"ENGRAM_S3_SECRET_KEY",
		"ENGRAM_S3_USE_SSL",
		"ENGRAM_S3_URL_EXPIRY",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
}

// Helper to set dev mode with required env vars for testing
func setDevModeEnv(t *testing.T) {
	t.Helper()
	os.Setenv("ENGRAM_DEV_MODE", "true")
}

// Helper to set production env vars (API keys required)
func setProdEnv(t *testing.T) {
	t.Helper()
	os.Setenv("OPENAI_API_KEY", "sk-test-openai-key")
	os.Setenv("ENGRAM_API_KEY", "test-api-key")
}

// dur converts Duration to time.Duration for comparison
func dur(d Duration) time.Duration {
	return time.Duration(d)
}

// Test: Default values when no config file and no env vars (dev mode)
func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Server defaults
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if dur(cfg.Server.ReadTimeout) != 30*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 30s", cfg.Server.ReadTimeout)
	}
	if dur(cfg.Server.WriteTimeout) != 30*time.Second {
		t.Errorf("Server.WriteTimeout = %v, want 30s", cfg.Server.WriteTimeout)
	}
	if dur(cfg.Server.ShutdownTimeout) != 15*time.Second {
		t.Errorf("Server.ShutdownTimeout = %v, want 15s", cfg.Server.ShutdownTimeout)
	}

	// Database defaults
	if cfg.Database.Path != "data/engram.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "data/engram.db")
	}

	// Embedding defaults
	if cfg.Embedding.Model != "text-embedding-3-small" {
		t.Errorf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "text-embedding-3-small")
	}
	if cfg.Embedding.Dimensions != 1536 {
		t.Errorf("Embedding.Dimensions = %d, want 1536", cfg.Embedding.Dimensions)
	}

	// Worker defaults
	if dur(cfg.Worker.SnapshotInterval) != 1*time.Hour {
		t.Errorf("Worker.SnapshotInterval = %v, want 1h", cfg.Worker.SnapshotInterval)
	}
	if dur(cfg.Worker.DecayInterval) != 24*time.Hour {
		t.Errorf("Worker.DecayInterval = %v, want 24h", cfg.Worker.DecayInterval)
	}
	if dur(cfg.Worker.EmbeddingRetryInterval) != 5*time.Minute {
		t.Errorf("Worker.EmbeddingRetryInterval = %v, want 5m", cfg.Worker.EmbeddingRetryInterval)
	}
	if cfg.Worker.EmbeddingRetryMaxAttempts != 10 {
		t.Errorf("Worker.EmbeddingRetryMaxAttempts = %d, want 10", cfg.Worker.EmbeddingRetryMaxAttempts)
	}

	// Log defaults
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, "json")
	}

	// Deduplication defaults
	if !cfg.Deduplication.Enabled {
		t.Error("Deduplication.Enabled should default to true")
	}
	if cfg.Deduplication.SimilarityThreshold != 0.92 {
		t.Errorf("Deduplication.SimilarityThreshold = %v, want 0.92", cfg.Deduplication.SimilarityThreshold)
	}
}

// Test: Validation fails without API keys (non-dev mode)
func TestLoad_ValidationFailsWithoutAPIKeys(t *testing.T) {
	clearEnv(t)
	// No ENGRAM_DEV_MODE set, so validation should fail

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error when API keys missing, got nil")
	}
}

// Test: Validation passes with API keys set via env vars
func TestLoad_ValidationPassesWithAPIKeys(t *testing.T) {
	clearEnv(t)
	setProdEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Embedding.APIKey != "sk-test-openai-key" {
		t.Errorf("Embedding.APIKey = %q, want %q", cfg.Embedding.APIKey, "sk-test-openai-key")
	}
	if cfg.Auth.APIKey != "test-api-key" {
		t.Errorf("Auth.APIKey = %q, want %q", cfg.Auth.APIKey, "test-api-key")
	}
}

// Test: Dev mode bypasses API key validation
func TestLoad_DevModeBypassesValidation(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// API keys should be empty in dev mode
	if cfg.Embedding.APIKey != "" {
		t.Errorf("Embedding.APIKey = %q, want empty", cfg.Embedding.APIKey)
	}
	if cfg.Auth.APIKey != "" {
		t.Errorf("Auth.APIKey = %q, want empty", cfg.Auth.APIKey)
	}
}

// Test: Environment variables override defaults
func TestLoad_EnvVarOverrides(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	os.Setenv("ENGRAM_PORT", "9090")
	os.Setenv("ENGRAM_DB_PATH", "/custom/path.db")
	os.Setenv("ENGRAM_LOG_LEVEL", "debug")
	os.Setenv("ENGRAM_SNAPSHOT_INTERVAL", "2h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Database.Path != "/custom/path.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "/custom/path.db")
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if dur(cfg.Worker.SnapshotInterval) != 2*time.Hour {
		t.Errorf("Worker.SnapshotInterval = %v, want 2h", cfg.Worker.SnapshotInterval)
	}
}

// Test: Empty env var does NOT override (only non-empty values override)
func TestLoad_EmptyEnvVarDoesNotOverride(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_PORT", "") // Empty string

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should use default, not empty value
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080 (default)", cfg.Server.Port)
	}
}

// Test: YAML file loading
func TestLoadFromFile_ValidYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	// Create temp YAML file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
server:
  port: 9999
  read_timeout: 60s
database:
  path: /yaml/path.db
log:
  level: warn
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want 9999", cfg.Server.Port)
	}
	if dur(cfg.Server.ReadTimeout) != 60*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 60s", cfg.Server.ReadTimeout)
	}
	if cfg.Database.Path != "/yaml/path.db" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "/yaml/path.db")
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "warn")
	}
}

// Test: Env vars override YAML values
func TestLoad_EnvOverridesYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	// Create temp YAML file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
server:
  port: 9000
log:
  level: warn
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	os.Setenv("ENGRAM_CONFIG_PATH", configPath)
	os.Setenv("ENGRAM_PORT", "8888") // Should override YAML

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Env should win over YAML
	if cfg.Server.Port != 8888 {
		t.Errorf("Server.Port = %d, want 8888 (env override)", cfg.Server.Port)
	}
	// YAML value should still apply where no env override
	if cfg.Log.Level != "warn" {
		t.Errorf("Log.Level = %q, want %q (from YAML)", cfg.Log.Level, "warn")
	}
}

// Test: Invalid YAML returns error
func TestLoadFromFile_InvalidYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	invalidYAML := `
server:
  port: not_a_number
  this is invalid yaml [
`
	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Error("LoadFromFile() expected error for invalid YAML, got nil")
	}
}

// Test: Missing config file is NOT an error (uses defaults)
func TestLoad_MissingConfigFileUsesDefaults(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_CONFIG_PATH", "/nonexistent/path/config.yaml")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not error on missing file, got: %v", err)
	}

	// Should use defaults
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080 (default)", cfg.Server.Port)
	}
}

// Test: Duration parsing with various formats
func TestLoadFromFile_DurationParsing(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "durations.yaml")
	yamlContent := `
server:
  read_timeout: 5m30s
  write_timeout: 90s
worker:
  snapshot_interval: 2h
  decay_interval: 48h
  embedding_retry_interval: 30s
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if dur(cfg.Server.ReadTimeout) != 5*time.Minute+30*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 5m30s", cfg.Server.ReadTimeout)
	}
	if dur(cfg.Server.WriteTimeout) != 90*time.Second {
		t.Errorf("Server.WriteTimeout = %v, want 90s", cfg.Server.WriteTimeout)
	}
	if dur(cfg.Worker.SnapshotInterval) != 2*time.Hour {
		t.Errorf("Worker.SnapshotInterval = %v, want 2h", cfg.Worker.SnapshotInterval)
	}
}

// Test: Zero values in YAML should override defaults (explicit zero)
func TestLoadFromFile_ExplicitZeroOverridesDefault(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "zeros.yaml")
	yamlContent := `
worker:
  embedding_retry_max_attempts: 0
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// Explicit zero should override default (10)
	if cfg.Worker.EmbeddingRetryMaxAttempts != 0 {
		t.Errorf("Worker.EmbeddingRetryMaxAttempts = %d, want 0 (explicit)", cfg.Worker.EmbeddingRetryMaxAttempts)
	}
}

// Test: Invalid duration string returns error
func TestLoadFromFile_InvalidDuration(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad_duration.yaml")
	yamlContent := `
server:
  read_timeout: not_a_duration
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Error("LoadFromFile() expected error for invalid duration, got nil")
	}
}

// Test: Secrets are not serializable via YAML tag
func TestConfig_SecretsNotInYAML(t *testing.T) {
	cfg := &Config{
		Embedding: EmbeddingConfig{APIKey: "secret-key", Model: "test"},
		Auth:      AuthConfig{APIKey: "another-secret"},
	}

	// Marshal to YAML and verify secrets are not present
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	yamlStr := string(data)
	if strings.Contains(yamlStr, "secret-key") {
		t.Errorf("YAML contains Embedding.APIKey secret: %s", yamlStr)
	}
	if strings.Contains(yamlStr, "another-secret") {
		t.Errorf("YAML contains Auth.APIKey secret: %s", yamlStr)
	}
}

// Test: All env var mappings work correctly
func TestLoad_AllEnvVarMappings(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	os.Setenv("ENGRAM_PORT", "3000")
	os.Setenv("ENGRAM_READ_TIMEOUT", "45s")
	os.Setenv("ENGRAM_WRITE_TIMEOUT", "45s")
	os.Setenv("ENGRAM_SHUTDOWN_TIMEOUT", "20s")
	os.Setenv("ENGRAM_DB_PATH", "/env/db.sqlite")
	os.Setenv("OPENAI_API_KEY", "sk-openai")
	os.Setenv("ENGRAM_EMBEDDING_MODEL", "text-embedding-ada-002")
	os.Setenv("ENGRAM_API_KEY", "api-key-123")
	os.Setenv("ENGRAM_SNAPSHOT_INTERVAL", "30m")
	os.Setenv("ENGRAM_DECAY_INTERVAL", "12h")
	os.Setenv("ENGRAM_EMBEDDING_RETRY_INTERVAL", "10m")
	os.Setenv("ENGRAM_EMBEDDING_RETRY_MAX_ATTEMPTS", "5")
	os.Setenv("ENGRAM_LOG_LEVEL", "error")
	os.Setenv("ENGRAM_LOG_FORMAT", "text")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Server
	if cfg.Server.Port != 3000 {
		t.Errorf("Server.Port = %d, want 3000", cfg.Server.Port)
	}
	if dur(cfg.Server.ReadTimeout) != 45*time.Second {
		t.Errorf("Server.ReadTimeout = %v, want 45s", cfg.Server.ReadTimeout)
	}
	if dur(cfg.Server.WriteTimeout) != 45*time.Second {
		t.Errorf("Server.WriteTimeout = %v, want 45s", cfg.Server.WriteTimeout)
	}
	if dur(cfg.Server.ShutdownTimeout) != 20*time.Second {
		t.Errorf("Server.ShutdownTimeout = %v, want 20s", cfg.Server.ShutdownTimeout)
	}

	// Database
	if cfg.Database.Path != "/env/db.sqlite" {
		t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "/env/db.sqlite")
	}

	// Embedding
	if cfg.Embedding.APIKey != "sk-openai" {
		t.Errorf("Embedding.APIKey = %q, want %q", cfg.Embedding.APIKey, "sk-openai")
	}
	if cfg.Embedding.Model != "text-embedding-ada-002" {
		t.Errorf("Embedding.Model = %q, want %q", cfg.Embedding.Model, "text-embedding-ada-002")
	}

	// Auth
	if cfg.Auth.APIKey != "api-key-123" {
		t.Errorf("Auth.APIKey = %q, want %q", cfg.Auth.APIKey, "api-key-123")
	}

	// Worker
	if dur(cfg.Worker.SnapshotInterval) != 30*time.Minute {
		t.Errorf("Worker.SnapshotInterval = %v, want 30m", cfg.Worker.SnapshotInterval)
	}
	if dur(cfg.Worker.DecayInterval) != 12*time.Hour {
		t.Errorf("Worker.DecayInterval = %v, want 12h", cfg.Worker.DecayInterval)
	}
	if dur(cfg.Worker.EmbeddingRetryInterval) != 10*time.Minute {
		t.Errorf("Worker.EmbeddingRetryInterval = %v, want 10m", cfg.Worker.EmbeddingRetryInterval)
	}
	if cfg.Worker.EmbeddingRetryMaxAttempts != 5 {
		t.Errorf("Worker.EmbeddingRetryMaxAttempts = %d, want 5", cfg.Worker.EmbeddingRetryMaxAttempts)
	}

	// Log
	if cfg.Log.Level != "error" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "error")
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, "text")
	}
}

// --- Deduplication Config Tests (Story 3.1) ---

// Test: SimilarityThreshold default value
func TestConfig_SimilarityThreshold_Default(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Deduplication.SimilarityThreshold != 0.92 {
		t.Errorf("Deduplication.SimilarityThreshold = %v, want 0.92", cfg.Deduplication.SimilarityThreshold)
	}
}

// Test: ENGRAM_SIMILARITY_THRESHOLD env var overrides default
func TestConfig_SimilarityThreshold_EnvOverride(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_SIMILARITY_THRESHOLD", "0.85")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Deduplication.SimilarityThreshold != 0.85 {
		t.Errorf("Deduplication.SimilarityThreshold = %v, want 0.85", cfg.Deduplication.SimilarityThreshold)
	}
}

// Test: SimilarityThreshold from YAML file
func TestConfig_SimilarityThreshold_FromYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
deduplication:
  similarity_threshold: 0.88
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Deduplication.SimilarityThreshold != 0.88 {
		t.Errorf("Deduplication.SimilarityThreshold = %v, want 0.88", cfg.Deduplication.SimilarityThreshold)
	}
}

// Test: Env var overrides YAML for SimilarityThreshold
func TestConfig_SimilarityThreshold_EnvOverridesYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
deduplication:
  similarity_threshold: 0.88
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	os.Setenv("ENGRAM_CONFIG_PATH", configPath)
	os.Setenv("ENGRAM_SIMILARITY_THRESHOLD", "0.95") // Should override YAML

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Deduplication.SimilarityThreshold != 0.95 {
		t.Errorf("Deduplication.SimilarityThreshold = %v, want 0.95 (env override)", cfg.Deduplication.SimilarityThreshold)
	}
}

// Test: Deduplication.Enabled default value
func TestConfig_DeduplicationEnabled_Default(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Deduplication.Enabled {
		t.Error("Deduplication.Enabled should default to true")
	}
}

// Test: ENGRAM_DEDUPLICATION_ENABLED=false disables deduplication
func TestConfig_DeduplicationEnabled_EnvDisables(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_DEDUPLICATION_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Deduplication.Enabled {
		t.Error("Deduplication.Enabled should be false when env var is 'false'")
	}
}

// Test: ENGRAM_DEDUPLICATION_ENABLED=1 enables deduplication
func TestConfig_DeduplicationEnabled_EnvEnables(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_DEDUPLICATION_ENABLED", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Deduplication.Enabled {
		t.Error("Deduplication.Enabled should be true when env var is '1'")
	}
}

// Test: Deduplication.Enabled from YAML
func TestConfig_DeduplicationEnabled_FromYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
deduplication:
  enabled: false
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Deduplication.Enabled {
		t.Error("Deduplication.Enabled should be false from YAML")
	}
}

// --- Stores Config Tests (Story 7.1) ---

// Test: Stores.RootPath default value
func TestConfig_StoresRootPath_Default(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Stores.RootPath != "~/.engram/stores" {
		t.Errorf("Stores.RootPath = %q, want %q", cfg.Stores.RootPath, "~/.engram/stores")
	}
}

// Test: ENGRAM_STORES_ROOT env var overrides default
func TestConfig_StoresRootPath_EnvOverride(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)
	os.Setenv("ENGRAM_STORES_ROOT", "/custom/stores/path")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Stores.RootPath != "/custom/stores/path" {
		t.Errorf("Stores.RootPath = %q, want %q", cfg.Stores.RootPath, "/custom/stores/path")
	}
}

// Test: Stores.RootPath from YAML file
func TestConfig_StoresRootPath_FromYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
stores:
  root_path: /yaml/stores
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Stores.RootPath != "/yaml/stores" {
		t.Errorf("Stores.RootPath = %q, want %q", cfg.Stores.RootPath, "/yaml/stores")
	}
}

// Test: Env var overrides YAML for Stores.RootPath
func TestConfig_StoresRootPath_EnvOverridesYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
stores:
  root_path: /yaml/stores
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	os.Setenv("ENGRAM_CONFIG_PATH", configPath)
	os.Setenv("ENGRAM_STORES_ROOT", "/env/stores") // Should override YAML

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Stores.RootPath != "/env/stores" {
		t.Errorf("Stores.RootPath = %q, want %q (env override)", cfg.Stores.RootPath, "/env/stores")
	}
}

// --- Snapshot Storage Config Tests ---

// Test: SnapshotStorage defaults
func TestConfig_SnapshotStorage_Defaults(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Bucket should be empty by default (S3 not configured)
	if cfg.SnapshotStorage.Bucket != "" {
		t.Errorf("SnapshotStorage.Bucket = %q, want empty", cfg.SnapshotStorage.Bucket)
	}
	// Region defaults to us-east-1
	if cfg.SnapshotStorage.Region != "us-east-1" {
		t.Errorf("SnapshotStorage.Region = %q, want %q", cfg.SnapshotStorage.Region, "us-east-1")
	}
	// UseSSL defaults to true
	if cfg.SnapshotStorage.UseSSL == nil {
		t.Fatal("SnapshotStorage.UseSSL should not be nil")
	}
	if !*cfg.SnapshotStorage.UseSSL {
		t.Error("SnapshotStorage.UseSSL should default to true")
	}
	// URLExpiry defaults to 15 minutes
	if dur(cfg.SnapshotStorage.URLExpiry) != 15*time.Minute {
		t.Errorf("SnapshotStorage.URLExpiry = %v, want 15m", dur(cfg.SnapshotStorage.URLExpiry))
	}
	// Secrets should be empty
	if cfg.SnapshotStorage.AccessKey != "" {
		t.Errorf("SnapshotStorage.AccessKey = %q, want empty", cfg.SnapshotStorage.AccessKey)
	}
	if cfg.SnapshotStorage.SecretKey != "" {
		t.Errorf("SnapshotStorage.SecretKey = %q, want empty", cfg.SnapshotStorage.SecretKey)
	}
}

// Test: S3 env var overrides
func TestConfig_SnapshotStorage_EnvOverrides(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	os.Setenv("ENGRAM_SNAPSHOT_BUCKET", "my-snapshots")
	os.Setenv("ENGRAM_S3_ENDPOINT", "s3.us-west-2.amazonaws.com")
	os.Setenv("ENGRAM_S3_REGION", "us-west-2")
	os.Setenv("ENGRAM_S3_ACCESS_KEY", "AKIAIOSFODNN7EXAMPLE")
	os.Setenv("ENGRAM_S3_SECRET_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	os.Setenv("ENGRAM_S3_USE_SSL", "false")
	os.Setenv("ENGRAM_S3_URL_EXPIRY", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.SnapshotStorage.Bucket != "my-snapshots" {
		t.Errorf("Bucket = %q, want %q", cfg.SnapshotStorage.Bucket, "my-snapshots")
	}
	if cfg.SnapshotStorage.Endpoint != "s3.us-west-2.amazonaws.com" {
		t.Errorf("Endpoint = %q, want %q", cfg.SnapshotStorage.Endpoint, "s3.us-west-2.amazonaws.com")
	}
	if cfg.SnapshotStorage.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", cfg.SnapshotStorage.Region, "us-west-2")
	}
	if cfg.SnapshotStorage.AccessKey != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("AccessKey = %q, want %q", cfg.SnapshotStorage.AccessKey, "AKIAIOSFODNN7EXAMPLE")
	}
	if cfg.SnapshotStorage.SecretKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("SecretKey = %q, want %q", cfg.SnapshotStorage.SecretKey, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	}
	if cfg.SnapshotStorage.UseSSL == nil || *cfg.SnapshotStorage.UseSSL {
		t.Error("UseSSL should be false when env var is 'false'")
	}
	if dur(cfg.SnapshotStorage.URLExpiry) != 30*time.Minute {
		t.Errorf("URLExpiry = %v, want 30m", dur(cfg.SnapshotStorage.URLExpiry))
	}
}

// Test: S3 secrets are NOT serializable via YAML
func TestConfig_SnapshotStorage_SecretsNotInYAML(t *testing.T) {
	cfg := &Config{
		SnapshotStorage: SnapshotStorageConfig{
			Bucket:    "test-bucket",
			AccessKey: "secret-access-key",
			SecretKey: "secret-secret-key",
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	yamlStr := string(data)
	if strings.Contains(yamlStr, "secret-access-key") {
		t.Errorf("YAML contains S3 AccessKey secret: %s", yamlStr)
	}
	if strings.Contains(yamlStr, "secret-secret-key") {
		t.Errorf("YAML contains S3 SecretKey secret: %s", yamlStr)
	}
}

// Test: SnapshotStorage from YAML file
func TestConfig_SnapshotStorage_FromYAML(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
snapshot_storage:
  bucket: yaml-bucket
  endpoint: minio.local:9000
  region: eu-west-1
  use_ssl: false
  url_expiry: 10m
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.SnapshotStorage.Bucket != "yaml-bucket" {
		t.Errorf("Bucket = %q, want %q", cfg.SnapshotStorage.Bucket, "yaml-bucket")
	}
	if cfg.SnapshotStorage.Endpoint != "minio.local:9000" {
		t.Errorf("Endpoint = %q, want %q", cfg.SnapshotStorage.Endpoint, "minio.local:9000")
	}
	if cfg.SnapshotStorage.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", cfg.SnapshotStorage.Region, "eu-west-1")
	}
	if cfg.SnapshotStorage.UseSSL == nil || *cfg.SnapshotStorage.UseSSL {
		t.Error("UseSSL should be false from YAML")
	}
	if dur(cfg.SnapshotStorage.URLExpiry) != 10*time.Minute {
		t.Errorf("URLExpiry = %v, want 10m", dur(cfg.SnapshotStorage.URLExpiry))
	}
}

// Test: UseSSL defaults to true when not set in YAML
func TestConfig_SnapshotStorage_UseSSLDefault(t *testing.T) {
	clearEnv(t)
	setDevModeEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
snapshot_storage:
  bucket: some-bucket
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	// UseSSL should retain default true even when YAML only sets bucket
	if cfg.SnapshotStorage.UseSSL == nil {
		t.Fatal("UseSSL should not be nil")
	}
	if !*cfg.SnapshotStorage.UseSSL {
		t.Error("UseSSL should default to true when not set in YAML")
	}
}
