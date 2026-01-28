package store

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_Identity(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrDuplicateLore", ErrDuplicateLore},
		{"ErrEmbeddingUnavailable", ErrEmbeddingUnavailable},
		{"ErrEmbeddingPending", ErrEmbeddingPending},
	}

	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.err == nil {
				t.Fatal("Sentinel error should not be nil")
			}
			if s.err.Error() == "" {
				t.Fatal("Sentinel error should have a message")
			}
		})
	}
}

func TestSentinelErrors_WrappedIdentity(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", ErrNotFound},
		{"ErrDuplicateLore", ErrDuplicateLore},
		{"ErrEmbeddingUnavailable", ErrEmbeddingUnavailable},
		{"ErrEmbeddingPending", ErrEmbeddingPending},
	}

	for _, s := range sentinels {
		t.Run(s.name+"_wrapped", func(t *testing.T) {
			wrapped := fmt.Errorf("operation failed: %w", s.err)
			if !errors.Is(wrapped, s.err) {
				t.Errorf("errors.Is should return true for wrapped %s", s.name)
			}
		})
	}
}
