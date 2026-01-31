package multistore

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// StoreMeta contains store-level metadata persisted in meta.yaml.
type StoreMeta struct {
	// Created is when the store was first created.
	Created time.Time `yaml:"created"`
	// LastAccessed is when the store was last accessed (read or write).
	LastAccessed time.Time `yaml:"last_accessed"`
	// Description is an optional human-readable description.
	Description string `yaml:"description,omitempty"`
}

// StoreInfo contains summary information about a store.
type StoreInfo struct {
	ID           string    `json:"id"`
	Created      time.Time `json:"created"`
	LastAccessed time.Time `json:"last_accessed"`
	Description  string    `json:"description,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
}

// NewStoreMeta creates metadata for a new store.
func NewStoreMeta(description string) *StoreMeta {
	now := time.Now().UTC()
	return &StoreMeta{
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
