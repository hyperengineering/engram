package validation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidationError represents a single field validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Collector accumulates validation errors without failing on first.
type Collector struct {
	errors []ValidationError
}

// Add appends a validation error to the collector if non-nil.
func (c *Collector) Add(err *ValidationError) {
	if err != nil {
		c.errors = append(c.errors, *err)
	}
}

// HasErrors returns true if the collector has accumulated any errors.
func (c *Collector) HasErrors() bool {
	return len(c.errors) > 0
}

// Errors returns all accumulated validation errors.
func (c *Collector) Errors() []ValidationError {
	return c.errors
}

// ValidateUTF8 returns an error if the value is not valid UTF-8.
func ValidateUTF8(field, value string) *ValidationError {
	if !utf8.ValidString(value) {
		return &ValidationError{
			Field:   field,
			Message: "must be valid UTF-8",
		}
	}
	return nil
}

// ValidateNoNullBytes returns an error if the value contains null bytes.
func ValidateNoNullBytes(field, value string) *ValidationError {
	if strings.Contains(value, "\x00") {
		return &ValidationError{
			Field:   field,
			Message: "must not contain null bytes",
		}
	}
	return nil
}

// ValidateMaxLength returns an error if the value exceeds max runes.
func ValidateMaxLength(field, value string, max int) *ValidationError {
	if utf8.RuneCountInString(value) > max {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("exceeds maximum length of %d characters", max),
		}
	}
	return nil
}

// ValidateULID returns an error if the value is not a valid ULID format.
// ULIDs are 26 characters using Crockford Base32 (excludes I, L, O, U).
func ValidateULID(field, value string) *ValidationError {
	if len(value) != 26 {
		return &ValidationError{
			Field:   field,
			Message: "must be a valid ULID (26 characters)",
		}
	}

	// Crockford Base32 alphabet: 0123456789ABCDEFGHJKMNPQRSTVWXYZ
	// Excludes: I, L, O, U (to avoid confusion)
	const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for _, r := range value {
		upper := strings.ToUpper(string(r))
		if !strings.Contains(crockfordBase32, upper) {
			return &ValidationError{
				Field:   field,
				Message: "must be a valid ULID (invalid character)",
			}
		}
	}
	return nil
}

// ValidateRequired returns an error if the value is empty or whitespace-only.
func ValidateRequired(field, value string) *ValidationError {
	if strings.TrimSpace(value) == "" {
		return &ValidationError{
			Field:   field,
			Message: "is required",
		}
	}
	return nil
}

// ValidateEnum returns an error if the value is not in the allowed list.
func ValidateEnum(field, value string, allowed []string) *ValidationError {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return &ValidationError{
		Field:   field,
		Message: fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")),
	}
}

// ValidateRange returns an error if the value is outside [min, max].
func ValidateRange(field string, value, min, max float64) *ValidationError {
	if value < min || value > max {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be between %.1f and %.1f", min, max),
		}
	}
	return nil
}
