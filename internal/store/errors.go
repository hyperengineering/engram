package store

import "errors"

var (
	ErrNotFound             = errors.New("lore entry not found")
	ErrDuplicateLore        = errors.New("duplicate lore entry")
	ErrEmbeddingUnavailable = errors.New("embedding service unavailable")
	ErrEmbeddingPending     = errors.New("embedding generation pending")
)
