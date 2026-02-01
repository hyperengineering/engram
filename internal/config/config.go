package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
// It is read-only after Load() returns and thread-safe for concurrent reads.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Embedding     EmbeddingConfig     `yaml:"embedding"`
	Auth          AuthConfig          `yaml:"auth"`
	Worker        WorkerConfig        `yaml:"worker"`
	Log           LogConfig           `yaml:"log"`
	Deduplication DeduplicationConfig `yaml:"deduplication"`
	Stores        StoresConfig        `yaml:"stores"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port            int      `yaml:"port"`
	ReadTimeout     Duration `yaml:"read_timeout"`
	WriteTimeout    Duration `yaml:"write_timeout"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
}

// DatabaseConfig contains database settings.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// EmbeddingConfig contains embedding service settings.
type EmbeddingConfig struct {
	APIKey     string `yaml:"-"` // env-only, never in YAML
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

// AuthConfig contains authentication settings.
type AuthConfig struct {
	APIKey string `yaml:"-"` // env-only, never in YAML
}

// WorkerConfig contains background worker settings.
type WorkerConfig struct {
	SnapshotInterval          Duration `yaml:"snapshot_interval"`
	DecayInterval             Duration `yaml:"decay_interval"`
	EmbeddingRetryInterval    Duration `yaml:"embedding_retry_interval"`
	EmbeddingRetryMaxAttempts int      `yaml:"embedding_retry_max_attempts"`
	EmbeddingRetryBatchSize   int      `yaml:"embedding_retry_batch_size"`
}

// LogConfig contains logging settings.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// DeduplicationConfig contains semantic deduplication settings.
type DeduplicationConfig struct {
	Enabled             bool    `yaml:"enabled"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
}

// StoresConfig contains multi-store settings.
type StoresConfig struct {
	RootPath string `yaml:"root_path"`
}

// GetDeduplicationEnabled returns whether deduplication is enabled.
func (c *Config) GetDeduplicationEnabled() bool {
	return c.Deduplication.Enabled
}

// GetSimilarityThreshold returns the similarity threshold for deduplication.
func (c *Config) GetSimilarityThreshold() float64 {
	return c.Deduplication.SimilarityThreshold
}

// Duration is a wrapper around time.Duration that supports YAML string parsing.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Load loads configuration with precedence: defaults → YAML file → env vars.
// Returns an immutable Config suitable for concurrent read access.
func Load() (*Config, error) {
	cfg := newDefaults()

	// Determine config path
	configPath := getEnv("ENGRAM_CONFIG_PATH", "config/engram.yaml")

	// Load YAML file if it exists (missing file is not an error)
	if err := loadYAMLFile(cfg, configPath); err != nil {
		return nil, err
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadFromFile loads configuration from a specific path.
// Used for testing and explicit path specification.
func LoadFromFile(path string) (*Config, error) {
	cfg := newDefaults()

	// Load YAML file (file must exist for this function)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// newDefaults returns a Config with all default values.
func newDefaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            8080,
			ReadTimeout:     Duration(30 * time.Second),
			WriteTimeout:    Duration(30 * time.Second),
			ShutdownTimeout: Duration(15 * time.Second),
		},
		Database: DatabaseConfig{
			Path: "data/engram.db",
		},
		Embedding: EmbeddingConfig{
			Model:      "text-embedding-3-small",
			Dimensions: 1536,
		},
		Worker: WorkerConfig{
			SnapshotInterval:          Duration(1 * time.Hour),
			DecayInterval:             Duration(24 * time.Hour),
			EmbeddingRetryInterval:    Duration(5 * time.Minute),
			EmbeddingRetryMaxAttempts: 10,
			EmbeddingRetryBatchSize:   50,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Deduplication: DeduplicationConfig{
			Enabled:             true,
			SimilarityThreshold: 0.92,
		},
		Stores: StoresConfig{
			RootPath: "~/.engram/stores",
		},
	}
}

// loadYAMLFile loads configuration from a YAML file if it exists.
// Missing file is not an error; we just use defaults.
func loadYAMLFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing file is OK; use defaults
			return nil
		}
		return fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides to the config.
// Only non-empty env vars override config values.
func applyEnvOverrides(cfg *Config) {
	// Server
	if v := os.Getenv("ENGRAM_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	if v := os.Getenv("ENGRAM_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ReadTimeout = Duration(d)
		}
	}
	if v := os.Getenv("ENGRAM_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.WriteTimeout = Duration(d)
		}
	}
	if v := os.Getenv("ENGRAM_SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ShutdownTimeout = Duration(d)
		}
	}

	// Database
	if v := os.Getenv("ENGRAM_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}

	// Embedding (OPENAI_API_KEY is industry convention)
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.Embedding.APIKey = v
	}
	if v := os.Getenv("ENGRAM_EMBEDDING_MODEL"); v != "" {
		cfg.Embedding.Model = v
	}

	// Auth
	if v := os.Getenv("ENGRAM_API_KEY"); v != "" {
		cfg.Auth.APIKey = v
	}

	// Worker
	if v := os.Getenv("ENGRAM_SNAPSHOT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Worker.SnapshotInterval = Duration(d)
		}
	}
	if v := os.Getenv("ENGRAM_DECAY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Worker.DecayInterval = Duration(d)
		}
	}
	if v := os.Getenv("ENGRAM_EMBEDDING_RETRY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Worker.EmbeddingRetryInterval = Duration(d)
		}
	}
	if v := os.Getenv("ENGRAM_EMBEDDING_RETRY_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Worker.EmbeddingRetryMaxAttempts = n
		}
	}
	if v := os.Getenv("ENGRAM_EMBEDDING_RETRY_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Worker.EmbeddingRetryBatchSize = n
		}
	}

	// Log
	if v := os.Getenv("ENGRAM_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("ENGRAM_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}

	// Deduplication
	if v := os.Getenv("ENGRAM_DEDUPLICATION_ENABLED"); v != "" {
		cfg.Deduplication.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("ENGRAM_SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Deduplication.SimilarityThreshold = f
		}
	}

	// Stores
	if v := os.Getenv("ENGRAM_STORES_ROOT"); v != "" {
		cfg.Stores.RootPath = v
	}
}

// validate checks that required configuration values are set.
// In dev mode (ENGRAM_DEV_MODE=true), API key validation is skipped.
func (c *Config) validate() error {
	// Dev mode bypasses API key validation
	if os.Getenv("ENGRAM_DEV_MODE") == "true" {
		return nil
	}

	if c.Embedding.APIKey == "" {
		return errors.New("OPENAI_API_KEY is required")
	}
	if c.Auth.APIKey == "" {
		return errors.New("ENGRAM_API_KEY is required")
	}
	return nil
}

// getEnv returns the value of an environment variable or a default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
