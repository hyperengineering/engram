package multistore

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxStoreIDLength is the maximum length of a store ID.
	MaxStoreIDLength = 128
	// MaxStoreIDSegments is the maximum number of path segments.
	MaxStoreIDSegments = 4
	// DefaultStoreID is the auto-created default store.
	DefaultStoreID = "default"
)

var (
	// ErrInvalidStoreID indicates a store ID failed validation.
	ErrInvalidStoreID = errors.New("invalid store ID")
	// ErrStoreNotFound indicates the requested store does not exist.
	ErrStoreNotFound = errors.New("store not found")
	// ErrStoreAlreadyExists indicates a store already exists during creation.
	ErrStoreAlreadyExists = errors.New("store already exists")
)

// storeIDSegmentPattern matches a single valid segment.
// Segment must start and end with alphanumeric, can contain hyphens in middle.
var storeIDSegmentPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateStoreID validates a store ID against format rules.
// Returns nil if valid, ErrInvalidStoreID with details if invalid.
func ValidateStoreID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: empty store ID", ErrInvalidStoreID)
	}

	if len(id) > MaxStoreIDLength {
		return fmt.Errorf("%w: exceeds %d characters", ErrInvalidStoreID, MaxStoreIDLength)
	}

	segments := strings.Split(id, "/")
	if len(segments) > MaxStoreIDSegments {
		return fmt.Errorf("%w: exceeds %d path segments", ErrInvalidStoreID, MaxStoreIDSegments)
	}

	for i, seg := range segments {
		if seg == "" {
			return fmt.Errorf("%w: empty segment at position %d", ErrInvalidStoreID, i)
		}
		if !storeIDSegmentPattern.MatchString(seg) {
			return fmt.Errorf("%w: invalid segment %q (must be lowercase alphanumeric with hyphens)",
				ErrInvalidStoreID, seg)
		}
	}

	return nil
}

// IsDefaultStore returns true if the store ID is the default store.
func IsDefaultStore(id string) bool {
	return id == DefaultStoreID
}
