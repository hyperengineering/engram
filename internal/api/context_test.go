package api

import (
	"context"
	"testing"

	"github.com/hyperengineering/engram/internal/store"
)

// TestWithStore_StoreFromContext_RoundTrip verifies store can be added and extracted from context.
func TestWithStore_StoreFromContext_RoundTrip(t *testing.T) {
	mockStore := &mockStore{}
	ctx := context.Background()

	// Add store to context
	ctx = WithStore(ctx, mockStore)

	// Extract store from context
	got, err := StoreFromContext(ctx)
	if err != nil {
		t.Fatalf("StoreFromContext returned error: %v", err)
	}

	if got != mockStore {
		t.Errorf("got different store instance, want same instance")
	}
}

// TestStoreFromContext_NoStore verifies error when no store in context.
func TestStoreFromContext_NoStore(t *testing.T) {
	ctx := context.Background()

	_, err := StoreFromContext(ctx)
	if err != ErrNoStoreInContext {
		t.Errorf("error = %v, want ErrNoStoreInContext", err)
	}
}

// TestStoreFromContext_NilStore verifies error when nil store in context.
func TestStoreFromContext_NilStore(t *testing.T) {
	ctx := context.Background()
	ctx = WithStore(ctx, nil)

	_, err := StoreFromContext(ctx)
	if err != ErrNoStoreInContext {
		t.Errorf("error = %v, want ErrNoStoreInContext", err)
	}
}

// TestMustStoreFromContext_Panics verifies panic when no store in context.
func TestMustStoreFromContext_Panics(t *testing.T) {
	ctx := context.Background()

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustStoreFromContext did not panic")
		}
	}()

	MustStoreFromContext(ctx)
}

// TestMustStoreFromContext_Success verifies successful extraction.
func TestMustStoreFromContext_Success(t *testing.T) {
	mockStore := &mockStore{}
	ctx := WithStore(context.Background(), mockStore)

	got := MustStoreFromContext(ctx)
	if got != mockStore {
		t.Errorf("got different store instance")
	}
}

// TestStoreIDFromContext_Default verifies default value when no store ID.
func TestStoreIDFromContext_Default(t *testing.T) {
	ctx := context.Background()

	got := StoreIDFromContext(ctx)
	if got != "default" {
		t.Errorf("StoreIDFromContext() = %q, want %q", got, "default")
	}
}

// TestStoreIDFromContext_Custom verifies custom store ID extraction.
func TestStoreIDFromContext_Custom(t *testing.T) {
	ctx := context.Background()
	ctx = WithStoreID(ctx, "test-store")

	got := StoreIDFromContext(ctx)
	if got != "test-store" {
		t.Errorf("StoreIDFromContext() = %q, want %q", got, "test-store")
	}
}

// TestWithStoreID_EmptyString verifies empty string returns default.
func TestWithStoreID_EmptyString(t *testing.T) {
	ctx := context.Background()
	ctx = WithStoreID(ctx, "")

	got := StoreIDFromContext(ctx)
	if got != "default" {
		t.Errorf("StoreIDFromContext() = %q, want %q for empty string", got, "default")
	}
}

// TestIsStoreScoped_Default verifies false when not scoped.
func TestIsStoreScoped_Default(t *testing.T) {
	ctx := context.Background()

	if IsStoreScoped(ctx) {
		t.Error("IsStoreScoped() = true for empty context, want false")
	}
}

// TestIsStoreScoped_WhenScoped verifies true when WithStoreScoped was called.
func TestIsStoreScoped_WhenScoped(t *testing.T) {
	ctx := context.Background()
	ctx = WithStoreScoped(ctx)

	if !IsStoreScoped(ctx) {
		t.Error("IsStoreScoped() = false after WithStoreScoped, want true")
	}
}

// TestStoreScoped_WithOtherContextValues verifies scoped flag is independent.
func TestStoreScoped_WithOtherContextValues(t *testing.T) {
	ctx := context.Background()
	ctx = WithStoreID(ctx, "my-store")
	ctx = WithStore(ctx, &mockStore{})

	// Not scoped yet
	if IsStoreScoped(ctx) {
		t.Error("IsStoreScoped() should be false before WithStoreScoped")
	}

	// Now scope it
	ctx = WithStoreScoped(ctx)

	if !IsStoreScoped(ctx) {
		t.Error("IsStoreScoped() should be true after WithStoreScoped")
	}

	// Verify other context values are still accessible
	if StoreIDFromContext(ctx) != "my-store" {
		t.Error("StoreIDFromContext should still return my-store")
	}
	if _, err := StoreFromContext(ctx); err != nil {
		t.Errorf("StoreFromContext returned error: %v", err)
	}
}

// mockStoreForInterface is a compile-time check that mockStore implements store.Store
var _ store.Store = (*mockStore)(nil)
