package validation

import (
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/types"
)

// --- ValidateUTF8 Tests ---

func TestValidateUTF8_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"ascii", "hello world"},
		{"empty", ""},
		{"unicode", "Hello, 世界"},
		{"emoji", "Hello 👋🏻"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUTF8("field", tt.value)
			if err != nil {
				t.Errorf("ValidateUTF8(%q) = %v, want nil", tt.value, err)
			}
		})
	}
}

func TestValidateUTF8_Invalid(t *testing.T) {
	// Invalid UTF-8 byte sequence
	invalidUTF8 := string([]byte{0xff, 0xfe})

	err := ValidateUTF8("content", invalidUTF8)
	if err == nil {
		t.Error("ValidateUTF8(invalid) = nil, want error")
	}
	if err != nil && err.Field != "content" {
		t.Errorf("error.Field = %q, want %q", err.Field, "content")
	}
}

// --- ValidateNoNullBytes Tests ---

func TestValidateNoNullBytes_Clean(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"normal", "hello world"},
		{"empty", ""},
		{"unicode", "Hello, 世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNoNullBytes("field", tt.value)
			if err != nil {
				t.Errorf("ValidateNoNullBytes(%q) = %v, want nil", tt.value, err)
			}
		})
	}
}

func TestValidateNoNullBytes_WithNull(t *testing.T) {
	err := ValidateNoNullBytes("content", "hello\x00world")
	if err == nil {
		t.Error("ValidateNoNullBytes(with null) = nil, want error")
	}
	if err != nil && err.Field != "content" {
		t.Errorf("error.Field = %q, want %q", err.Field, "content")
	}
}

// --- ValidateMaxLength Tests ---

func TestValidateMaxLength_Within(t *testing.T) {
	value := strings.Repeat("a", 100)
	err := ValidateMaxLength("content", value, 4000)
	if err != nil {
		t.Errorf("ValidateMaxLength(100 chars, max 4000) = %v, want nil", err)
	}
}

func TestValidateMaxLength_AtLimit(t *testing.T) {
	value := strings.Repeat("a", 4000)
	err := ValidateMaxLength("content", value, 4000)
	if err != nil {
		t.Errorf("ValidateMaxLength(4000 chars, max 4000) = %v, want nil", err)
	}
}

func TestValidateMaxLength_Exceeds(t *testing.T) {
	value := strings.Repeat("a", 4001)
	err := ValidateMaxLength("content", value, 4000)
	if err == nil {
		t.Error("ValidateMaxLength(4001 chars, max 4000) = nil, want error")
	}
	if err != nil && err.Field != "content" {
		t.Errorf("error.Field = %q, want %q", err.Field, "content")
	}
}

func TestValidateMaxLength_MultibyteRunes(t *testing.T) {
	// 4000 emoji characters (each 4 bytes in UTF-8, but counts as 1 rune)
	value := strings.Repeat("👋", 4000)
	err := ValidateMaxLength("content", value, 4000)
	if err != nil {
		t.Errorf("ValidateMaxLength(4000 emoji, max 4000) = %v, want nil (counts runes)", err)
	}
}

func TestValidateMaxLength_MultibyteRunes_Exceeds(t *testing.T) {
	// 4001 emoji characters (exceeds 4000 rune limit)
	value := strings.Repeat("👋", 4001)
	err := ValidateMaxLength("content", value, 4000)
	if err == nil {
		t.Error("ValidateMaxLength(4001 emoji, max 4000) = nil, want error")
	}
}

// --- ValidateULID Tests ---

func TestValidateULID_Valid(t *testing.T) {
	// Valid ULIDs use Crockford Base32 (excludes I, L, O, U)
	validULIDs := []string{
		"01ARYZ6S41TSV4RRFFQ69G5FAV",
		"01HGW2N5E56F2ZXQWRR78YQRZ8",
		"00000000000000000000000000", // minimum ULID
		"7ZZZZZZZZZZZZZZZZZZZZZZZZZ", // maximum ULID
	}

	for _, ulid := range validULIDs {
		t.Run(ulid, func(t *testing.T) {
			err := ValidateULID("id", ulid)
			if err != nil {
				t.Errorf("ValidateULID(%q) = %v, want nil", ulid, err)
			}
		})
	}
}

func TestValidateULID_Invalid_TooShort(t *testing.T) {
	err := ValidateULID("id", "01ARYZ6S41")
	if err == nil {
		t.Error("ValidateULID(too short) = nil, want error")
	}
}

func TestValidateULID_Invalid_TooLong(t *testing.T) {
	err := ValidateULID("id", "01ARYZ6S41TSV4RRFFQ69G5FAVX")
	if err == nil {
		t.Error("ValidateULID(too long) = nil, want error")
	}
}

func TestValidateULID_Invalid_BadChar(t *testing.T) {
	// I, L, O, U are invalid in Crockford Base32
	invalidULIDs := []string{
		"01ARYZ6S41TSV4RRFFQ69GILOU", // contains I, L, O, U
		"01ARYZ6S41TSV4RRFFQ69G5FAi", // lowercase i
		"01ARYZ6S41TSV4RRFFQ69G5FAl", // lowercase l
		"01ARYZ6S41TSV4RRFFQ69G5FAo", // lowercase o
		"01ARYZ6S41TSV4RRFFQ69G5FAu", // lowercase u
	}

	for _, ulid := range invalidULIDs {
		t.Run(ulid, func(t *testing.T) {
			err := ValidateULID("id", ulid)
			if err == nil {
				t.Errorf("ValidateULID(%q) = nil, want error", ulid)
			}
		})
	}
}

func TestValidateULID_Invalid_Empty(t *testing.T) {
	err := ValidateULID("id", "")
	if err == nil {
		t.Error("ValidateULID(empty) = nil, want error")
	}
}

// --- ValidateRequired Tests ---

func TestValidateRequired_NonEmpty(t *testing.T) {
	err := ValidateRequired("field", "value")
	if err != nil {
		t.Errorf("ValidateRequired(value) = %v, want nil", err)
	}
}

func TestValidateRequired_Empty(t *testing.T) {
	err := ValidateRequired("source_id", "")
	if err == nil {
		t.Error("ValidateRequired(empty) = nil, want error")
	}
	if err != nil && err.Field != "source_id" {
		t.Errorf("error.Field = %q, want %q", err.Field, "source_id")
	}
}

func TestValidateRequired_WhitespaceOnly(t *testing.T) {
	tests := []string{" ", "   ", "\t", "\n", "  \t\n  "}
	for _, value := range tests {
		t.Run("whitespace", func(t *testing.T) {
			err := ValidateRequired("field", value)
			if err == nil {
				t.Errorf("ValidateRequired(%q) = nil, want error", value)
			}
		})
	}
}

// --- ValidateEnum Tests ---

func TestValidateEnum_Valid(t *testing.T) {
	allowed := []string{
		"DEPENDENCY_BEHAVIOR",
		"ARCHITECTURAL_DECISION",
		"PATTERN_OUTCOME",
		"EDGE_CASE",
		"DEBUGGING_INSIGHT",
		"PERFORMANCE_INSIGHT",
		"TESTING_INSIGHT",
		"TOOLING_TIP",
	}

	for _, category := range allowed {
		t.Run(category, func(t *testing.T) {
			err := ValidateEnum("category", category, allowed)
			if err != nil {
				t.Errorf("ValidateEnum(%q) = %v, want nil", category, err)
			}
		})
	}
}

func TestValidateEnum_Invalid(t *testing.T) {
	allowed := []string{"DEBUGGING_INSIGHT", "TESTING_INSIGHT"}
	err := ValidateEnum("category", "INVALID_CATEGORY", allowed)
	if err == nil {
		t.Error("ValidateEnum(invalid) = nil, want error")
	}
	if err != nil && err.Field != "category" {
		t.Errorf("error.Field = %q, want %q", err.Field, "category")
	}
}

func TestValidateEnum_CaseSensitive(t *testing.T) {
	allowed := []string{"DEBUGGING_INSIGHT"}
	err := ValidateEnum("category", "debugging_insight", allowed)
	if err == nil {
		t.Error("ValidateEnum(lowercase) = nil, want error (case sensitive)")
	}
}

// --- ValidateRange Tests ---

func TestValidateRange_Within(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"middle", 0.5},
		{"min", 0.0},
		{"max", 1.0},
		{"near_min", 0.001},
		{"near_max", 0.999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange("confidence", tt.value, 0.0, 1.0)
			if err != nil {
				t.Errorf("ValidateRange(%v, 0.0, 1.0) = %v, want nil", tt.value, err)
			}
		})
	}
}

func TestValidateRange_BelowMin(t *testing.T) {
	err := ValidateRange("confidence", -0.1, 0.0, 1.0)
	if err == nil {
		t.Error("ValidateRange(-0.1, 0.0, 1.0) = nil, want error")
	}
	if err != nil && err.Field != "confidence" {
		t.Errorf("error.Field = %q, want %q", err.Field, "confidence")
	}
}

func TestValidateRange_AboveMax(t *testing.T) {
	err := ValidateRange("confidence", 1.1, 0.0, 1.0)
	if err == nil {
		t.Error("ValidateRange(1.1, 0.0, 1.0) = nil, want error")
	}
}

// --- Collector Tests ---

func TestCollector_AccumulatesErrors(t *testing.T) {
	c := &Collector{}
	c.Add(&ValidationError{Field: "field1", Message: "error1"})
	c.Add(&ValidationError{Field: "field2", Message: "error2"})
	c.Add(&ValidationError{Field: "field3", Message: "error3"})

	errors := c.Errors()
	if len(errors) != 3 {
		t.Errorf("len(Errors()) = %d, want 3", len(errors))
	}
}

func TestCollector_IgnoresNil(t *testing.T) {
	c := &Collector{}
	c.Add(nil)
	c.Add(&ValidationError{Field: "field", Message: "error"})
	c.Add(nil)

	errors := c.Errors()
	if len(errors) != 1 {
		t.Errorf("len(Errors()) = %d, want 1 (nil should be ignored)", len(errors))
	}
}

func TestCollector_HasErrors_Empty(t *testing.T) {
	c := &Collector{}
	if c.HasErrors() {
		t.Error("HasErrors() = true, want false for empty collector")
	}
}

func TestCollector_HasErrors_WithErrors(t *testing.T) {
	c := &Collector{}
	c.Add(&ValidationError{Field: "field", Message: "error"})
	if !c.HasErrors() {
		t.Error("HasErrors() = false, want true for collector with errors")
	}
}

func TestCollector_Errors_ReturnsSlice(t *testing.T) {
	c := &Collector{}
	c.Add(&ValidationError{Field: "f1", Message: "m1"})
	c.Add(&ValidationError{Field: "f2", Message: "m2"})

	errors := c.Errors()
	if errors[0].Field != "f1" || errors[0].Message != "m1" {
		t.Errorf("errors[0] = %+v, want {Field:f1, Message:m1}", errors[0])
	}
	if errors[1].Field != "f2" || errors[1].Message != "m2" {
		t.Errorf("errors[1] = %+v, want {Field:f2, Message:m2}", errors[1])
	}
}

// --- ValidateLoreEntry Tests ---

func TestValidateLoreEntry_Valid(t *testing.T) {
	entry := types.Lore{
		Content:    "ORM generates N+1 queries for polymorphic associations",
		Context:    "Discovered while profiling Rails app",
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.7,
	}

	errs := ValidateLoreEntry(0, entry)
	if len(errs) != 0 {
		t.Errorf("ValidateLoreEntry(valid) = %v, want no errors", errs)
	}
}

func TestValidateLoreEntry_AllFieldsInvalid(t *testing.T) {
	// Create an entry where all validatable fields are invalid
	invalidUTF8 := string([]byte{0xff, 0xfe})
	entry := types.Lore{
		Content:    invalidUTF8,      // Invalid UTF-8
		Context:    "valid\x00null",  // Contains null byte
		Category:   "INVALID",        // Invalid enum
		Confidence: 1.5,              // Out of range
	}

	errs := ValidateLoreEntry(0, entry)
	// Should have at least 3 errors: content UTF-8, context null byte, category enum, confidence range
	if len(errs) < 3 {
		t.Errorf("ValidateLoreEntry(all invalid) = %d errors, want >= 3", len(errs))
	}
}

func TestValidateLoreEntry_ContentRequired(t *testing.T) {
	entry := types.Lore{
		Content:    "",
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasContentError := false
	for _, e := range errs {
		if e.Field == "lore[0].content" && strings.Contains(e.Message, "required") {
			hasContentError = true
			break
		}
	}
	if !hasContentError {
		t.Errorf("ValidateLoreEntry(empty content) missing content required error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ContentTooLong(t *testing.T) {
	entry := types.Lore{
		Content:    strings.Repeat("a", 4001),
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasLengthError := false
	for _, e := range errs {
		if e.Field == "lore[0].content" && strings.Contains(e.Message, "4000") {
			hasLengthError = true
			break
		}
	}
	if !hasLengthError {
		t.Errorf("ValidateLoreEntry(4001 chars) missing length error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ContentNullBytes(t *testing.T) {
	entry := types.Lore{
		Content:    "hello\x00world",
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasNullError := false
	for _, e := range errs {
		if e.Field == "lore[0].content" && strings.Contains(e.Message, "null") {
			hasNullError = true
			break
		}
	}
	if !hasNullError {
		t.Errorf("ValidateLoreEntry(null bytes) missing null byte error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ContentInvalidUTF8(t *testing.T) {
	invalidUTF8 := string([]byte{0xff, 0xfe})
	entry := types.Lore{
		Content:    invalidUTF8,
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasUTF8Error := false
	for _, e := range errs {
		if e.Field == "lore[0].content" && strings.Contains(e.Message, "UTF-8") {
			hasUTF8Error = true
			break
		}
	}
	if !hasUTF8Error {
		t.Errorf("ValidateLoreEntry(invalid UTF-8) missing UTF-8 error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ContextTooLong(t *testing.T) {
	entry := types.Lore{
		Content:    "valid content",
		Context:    strings.Repeat("a", 1001),
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasLengthError := false
	for _, e := range errs {
		if e.Field == "lore[0].context" && strings.Contains(e.Message, "1000") {
			hasLengthError = true
			break
		}
	}
	if !hasLengthError {
		t.Errorf("ValidateLoreEntry(context 1001 chars) missing length error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ContextOptional(t *testing.T) {
	entry := types.Lore{
		Content:    "valid content",
		Context:    "", // Empty is valid
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	for _, e := range errs {
		if strings.Contains(e.Field, "context") {
			t.Errorf("ValidateLoreEntry(empty context) should not error on context, got: %v", e)
		}
	}
}

func TestValidateLoreEntry_InvalidCategory(t *testing.T) {
	entry := types.Lore{
		Content:    "valid content",
		Category:   "INVALID_CATEGORY",
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(0, entry)
	hasCategoryError := false
	for _, e := range errs {
		if e.Field == "lore[0].category" && strings.Contains(e.Message, "must be one of") {
			hasCategoryError = true
			break
		}
	}
	if !hasCategoryError {
		t.Errorf("ValidateLoreEntry(invalid category) missing category error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ConfidenceBelowRange(t *testing.T) {
	entry := types.Lore{
		Content:    "valid content",
		Category:   types.CategoryDependencyBehavior,
		Confidence: -0.1,
	}

	errs := ValidateLoreEntry(0, entry)
	hasRangeError := false
	for _, e := range errs {
		if e.Field == "lore[0].confidence" && strings.Contains(e.Message, "between") {
			hasRangeError = true
			break
		}
	}
	if !hasRangeError {
		t.Errorf("ValidateLoreEntry(confidence -0.1) missing range error, got: %v", errs)
	}
}

func TestValidateLoreEntry_ConfidenceAboveRange(t *testing.T) {
	entry := types.Lore{
		Content:    "valid content",
		Category:   types.CategoryDependencyBehavior,
		Confidence: 1.1,
	}

	errs := ValidateLoreEntry(0, entry)
	hasRangeError := false
	for _, e := range errs {
		if e.Field == "lore[0].confidence" && strings.Contains(e.Message, "between") {
			hasRangeError = true
			break
		}
	}
	if !hasRangeError {
		t.Errorf("ValidateLoreEntry(confidence 1.1) missing range error, got: %v", errs)
	}
}

func TestValidateLoreEntry_IndexInFieldName(t *testing.T) {
	entry := types.Lore{
		Content:    "",
		Category:   types.CategoryDependencyBehavior,
		Confidence: 0.5,
	}

	errs := ValidateLoreEntry(5, entry)
	hasIndexedField := false
	for _, e := range errs {
		if strings.Contains(e.Field, "lore[5]") {
			hasIndexedField = true
			break
		}
	}
	if !hasIndexedField {
		t.Errorf("ValidateLoreEntry(index 5) should use lore[5] prefix, got: %v", errs)
	}
}

// --- ValidateIngestRequest Tests ---

func TestValidateIngestRequest_Valid(t *testing.T) {
	req := types.IngestRequest{
		SourceID: "devcontainer-abc123",
		Lore: []types.Lore{
			{
				Content:    "valid content",
				Category:   types.CategoryDependencyBehavior,
				Confidence: 0.5,
			},
		},
	}

	errs := ValidateIngestRequest(req)
	if len(errs) != 0 {
		t.Errorf("ValidateIngestRequest(valid) = %v, want no errors", errs)
	}
}

func TestValidateIngestRequest_MissingSourceID(t *testing.T) {
	req := types.IngestRequest{
		SourceID: "",
		Lore: []types.Lore{
			{Content: "valid", Category: types.CategoryDependencyBehavior, Confidence: 0.5},
		},
	}

	errs := ValidateIngestRequest(req)
	hasSourceIDError := false
	for _, e := range errs {
		if e.Field == "source_id" {
			hasSourceIDError = true
			break
		}
	}
	if !hasSourceIDError {
		t.Errorf("ValidateIngestRequest(missing source_id) should have source_id error, got: %v", errs)
	}
}

func TestValidateIngestRequest_EmptyLoreArray(t *testing.T) {
	req := types.IngestRequest{
		SourceID: "devcontainer-abc123",
		Lore:     []types.Lore{},
	}

	errs := ValidateIngestRequest(req)
	hasLoreError := false
	for _, e := range errs {
		if e.Field == "lore" && strings.Contains(e.Message, "empty") {
			hasLoreError = true
			break
		}
	}
	if !hasLoreError {
		t.Errorf("ValidateIngestRequest(empty lore) should have lore error, got: %v", errs)
	}
}

func TestValidateIngestRequest_MaxBatchSize(t *testing.T) {
	loreEntries := make([]types.Lore, 50)
	for i := range loreEntries {
		loreEntries[i] = types.Lore{
			Content:    "valid content",
			Category:   types.CategoryDependencyBehavior,
			Confidence: 0.5,
		}
	}

	req := types.IngestRequest{
		SourceID: "devcontainer-abc123",
		Lore:     loreEntries,
	}

	errs := ValidateIngestRequest(req)
	if len(errs) != 0 {
		t.Errorf("ValidateIngestRequest(50 entries) = %v, want no errors (at limit)", errs)
	}
}

func TestValidateIngestRequest_ExceedsMaxBatch(t *testing.T) {
	loreEntries := make([]types.Lore, 51)
	for i := range loreEntries {
		loreEntries[i] = types.Lore{
			Content:    "valid content",
			Category:   types.CategoryDependencyBehavior,
			Confidence: 0.5,
		}
	}

	req := types.IngestRequest{
		SourceID: "devcontainer-abc123",
		Lore:     loreEntries,
	}

	errs := ValidateIngestRequest(req)
	hasBatchError := false
	for _, e := range errs {
		if e.Field == "lore" && strings.Contains(e.Message, "50") {
			hasBatchError = true
			break
		}
	}
	if !hasBatchError {
		t.Errorf("ValidateIngestRequest(51 entries) should have batch size error, got: %v", errs)
	}
}
