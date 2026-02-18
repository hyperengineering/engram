package plugin

import (
	"errors"
	"testing"
)

func TestValidationError_Error_WithField(t *testing.T) {
	err := ValidationError{
		Sequence:  1,
		TableName: "lore",
		EntityID:  "e1",
		Field:     "content",
		Message:   "is required",
	}
	want := "lore.content: is required"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationError_Error_NoField(t *testing.T) {
	err := ValidationError{
		Sequence:  2,
		TableName: "goals",
		EntityID:  "g1",
		Message:   "unknown table",
	}
	want := "goals: unknown table"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Error_Multiple(t *testing.T) {
	errs := ValidationErrors{
		Errors: []ValidationError{
			{TableName: "lore", Field: "content", Message: "is required"},
			{TableName: "goals", Message: "unknown table"},
		},
	}
	// Should return the first error's message.
	want := "lore.content: is required"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Error_Empty(t *testing.T) {
	errs := ValidationErrors{}
	want := "validation failed"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Unwrap(t *testing.T) {
	errs := ValidationErrors{
		Errors: []ValidationError{
			{TableName: "lore", Message: "bad data"},
		},
	}

	if !errors.Is(errs, ErrValidationFailed) {
		t.Error("errors.Is(errs, ErrValidationFailed) = false, want true")
	}
}

func TestValidationErrors_Is_ErrValidationFailed(t *testing.T) {
	var errs error = ValidationErrors{
		Errors: []ValidationError{
			{TableName: "lore", Message: "bad"},
		},
	}

	// Verify via interface value — callers will typically use this pattern.
	if !errors.Is(errs, ErrValidationFailed) {
		t.Error("errors.Is(errs, ErrValidationFailed) = false, want true")
	}
}

func TestValidationErrors_As(t *testing.T) {
	var errs error = ValidationErrors{
		Errors: []ValidationError{
			{TableName: "lore", Field: "content", Message: "is required"},
		},
	}

	var ve ValidationErrors
	if !errors.As(errs, &ve) {
		t.Fatal("errors.As failed")
	}
	if len(ve.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(ve.Errors))
	}
	if ve.Errors[0].Field != "content" {
		t.Errorf("Errors[0].Field = %q, want %q", ve.Errors[0].Field, "content")
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	sentinels := []error{
		ErrValidationFailed,
		ErrUnknownTable,
		ErrMissingRequiredField,
		ErrInvalidPayload,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors %d and %d should be distinct", i, j)
			}
		}
	}
}
