package validation

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/hyperengineering/engram/internal/types"
)

// Validation constants per architecture specification.
const (
	MaxContentLength = 4000
	MaxContextLength = 1000
	MaxBatchSize     = 50
)

// ValidLoreCategories defines the allowed category values from types.go.
var ValidLoreCategories = []string{
	"ARCHITECTURAL_DECISION",
	"PATTERN_OUTCOME",
	"INTERFACE_LESSON",
	"EDGE_CASE_DISCOVERY",
	"IMPLEMENTATION_FRICTION",
	"TESTING_STRATEGY",
	"DEPENDENCY_BEHAVIOR",
	"PERFORMANCE_INSIGHT",
}

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

// ValidateLoreEntry validates a single lore entry and returns all errors.
func ValidateLoreEntry(index int, entry types.Lore) []ValidationError {
	c := &Collector{}
	fieldPrefix := fmt.Sprintf("lore[%d]", index)

	// Content: required, max 4000 chars, UTF-8, no null bytes
	c.Add(ValidateRequired(fieldPrefix+".content", entry.Content))
	c.Add(ValidateMaxLength(fieldPrefix+".content", entry.Content, MaxContentLength))
	c.Add(ValidateUTF8(fieldPrefix+".content", entry.Content))
	c.Add(ValidateNoNullBytes(fieldPrefix+".content", entry.Content))

	// Context: optional, max 1000 chars, UTF-8, no null bytes
	if entry.Context != "" {
		c.Add(ValidateMaxLength(fieldPrefix+".context", entry.Context, MaxContextLength))
		c.Add(ValidateUTF8(fieldPrefix+".context", entry.Context))
		c.Add(ValidateNoNullBytes(fieldPrefix+".context", entry.Context))
	}

	// Category: required, valid enum
	c.Add(ValidateRequired(fieldPrefix+".category", string(entry.Category)))
	c.Add(ValidateEnum(fieldPrefix+".category", string(entry.Category), ValidLoreCategories))

	// Confidence: required, range 0.0-1.0
	c.Add(ValidateRange(fieldPrefix+".confidence", entry.Confidence, 0.0, 1.0))

	return c.Errors()
}

// ValidateIngestRequest validates request-level fields (not individual entries).
func ValidateIngestRequest(req types.IngestRequest) []ValidationError {
	c := &Collector{}
	c.Add(ValidateRequired("source_id", req.SourceID))
	if len(req.Lore) == 0 {
		c.Add(&ValidationError{Field: "lore", Message: "is required and must not be empty"})
	} else if len(req.Lore) > MaxBatchSize {
		c.Add(&ValidationError{Field: "lore", Message: fmt.Sprintf("exceeds maximum batch size of %d", MaxBatchSize)})
	}
	return c.Errors()
}
