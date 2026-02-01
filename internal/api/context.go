package api

import (
	"context"
	"errors"

	"github.com/hyperengineering/engram/internal/store"
)

// storeContextKey is the context key for the resolved store.
type storeContextKey struct{}

// storeIDContextKey is the context key for the store ID (for logging).
type storeIDContextKey struct{}

// storeScopedContextKey is the context key for tracking explicit store-scoped routes.
type storeScopedContextKey struct{}

// ErrNoStoreInContext indicates no store was found in the context.
var ErrNoStoreInContext = errors.New("no store in context")

// WithStore returns a new context with the store attached.
func WithStore(ctx context.Context, s store.Store) context.Context {
	return context.WithValue(ctx, storeContextKey{}, s)
}

// StoreFromContext extracts the store from the context.
// Returns ErrNoStoreInContext if not present or nil.
func StoreFromContext(ctx context.Context) (store.Store, error) {
	s, ok := ctx.Value(storeContextKey{}).(store.Store)
	if !ok || s == nil {
		return nil, ErrNoStoreInContext
	}
	return s, nil
}

// MustStoreFromContext extracts the store or panics.
// Use only when middleware guarantees store presence.
func MustStoreFromContext(ctx context.Context) store.Store {
	s, err := StoreFromContext(ctx)
	if err != nil {
		panic("store not in context: middleware misconfiguration")
	}
	return s
}

// WithStoreID returns a new context with the store ID attached.
func WithStoreID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, storeIDContextKey{}, id)
}

// StoreIDFromContext extracts the store ID from the context.
// Returns "default" if not present or empty.
func StoreIDFromContext(ctx context.Context) string {
	id, ok := ctx.Value(storeIDContextKey{}).(string)
	if !ok || id == "" {
		return "default"
	}
	return id
}

// WithStoreScoped marks the context as explicitly store-scoped.
// Used by StoreContextMiddleware for store-scoped routes like /api/v1/stores/{store_id}/...
func WithStoreScoped(ctx context.Context) context.Context {
	return context.WithValue(ctx, storeScopedContextKey{}, true)
}

// IsStoreScoped returns true if the request was routed through an explicit store-scoped path.
// Returns false for default routes like /api/v1/stats that use the default store.
func IsStoreScoped(ctx context.Context) bool {
	scoped, ok := ctx.Value(storeScopedContextKey{}).(bool)
	return ok && scoped
}
