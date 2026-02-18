package multistore

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultStoreType is used when no type is specified or for backward compatibility.
const DefaultStoreType = "recall"

// StoreMeta contains store-level metadata persisted in meta.yaml.
type StoreMeta struct {
	// Type is the store type (e.g., "recall", "tract", "generic").
	// Determines which domain plugin handles this store.
	Type string `yaml:"type"`
	// Created is when the store was first created.
	Created time.Time `yaml:"created"`
	// LastAccessed is when the store was last accessed (read or write).
	LastAccessed time.Time `yaml:"last_accessed"`
	// Description is an optional human-readable description.
	Description string `yaml:"description,omitempty"`
}

// StoreInfo contains summary information about a store.
type StoreInfo struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	SchemaVersion int       `json:"schema_version"`
	Created       time.Time `json:"created"`
	LastAccessed  time.Time `json:"last_accessed"`
	Description   string    `json:"description,omitempty"`
	SizeBytes     int64     `json:"size_bytes"`
}

// NewStoreMeta creates metadata for a new store.
func NewStoreMeta(storeType, description string) *StoreMeta {
	if storeType == "" {
		storeType = DefaultStoreType
	}
	now := time.Now().UTC()
	return &StoreMeta{
		Type:         storeType,
		Created:      now,
		LastAccessed: now,
		Description:  description,
	}
}

// LoadStoreMeta reads store metadata from a file path.
// Returns an error if the file doesn't exist or is malformed.
func LoadStoreMeta(path string) (*StoreMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta StoreMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse store metadata: %w", err)
	}

	// Backward compatibility: default to "recall" if type not set
	if meta.Type == "" {
		meta.Type = DefaultStoreType
	}

	return &meta, nil
}

// SaveStoreMeta writes store metadata to a file path.
func SaveStoreMeta(path string, meta *StoreMeta) error {
	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal store metadata: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}
