package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLoreEntry_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	validated := now.Add(-time.Hour)

	entry := LoreEntry{
		ID:              "01JTEST000000000000000000",
		Content:         "test content",
		Context:         "test context",
		Category:        "ARCHITECTURAL_DECISION",
		Confidence:      0.85,
		Embedding:       []float32{0.1, 0.2, 0.3},
		SourceID:        "source-1",
		Sources:         []string{"src-a", "src-b"},
		ValidationCount: 3,
		CreatedAt:       now,
		UpdatedAt:       now,
		DeletedAt:       nil,
		LastValidatedAt: &validated,
		EmbeddingStatus: "complete",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded LoreEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != entry.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, entry.ID)
	}
	if decoded.Content != entry.Content {
		t.Errorf("Content: got %q, want %q", decoded.Content, entry.Content)
	}
	if decoded.Category != entry.Category {
		t.Errorf("Category: got %q, want %q", decoded.Category, entry.Category)
	}
	if decoded.Confidence != entry.Confidence {
		t.Errorf("Confidence: got %v, want %v", decoded.Confidence, entry.Confidence)
	}
	if decoded.SourceID != entry.SourceID {
		t.Errorf("SourceID: got %q, want %q", decoded.SourceID, entry.SourceID)
	}
	if decoded.EmbeddingStatus != entry.EmbeddingStatus {
		t.Errorf("EmbeddingStatus: got %q, want %q", decoded.EmbeddingStatus, entry.EmbeddingStatus)
	}
	if decoded.ValidationCount != entry.ValidationCount {
		t.Errorf("ValidationCount: got %d, want %d", decoded.ValidationCount, entry.ValidationCount)
	}
	if !decoded.CreatedAt.Equal(entry.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", decoded.CreatedAt, entry.CreatedAt)
	}
	if decoded.LastValidatedAt == nil {
		t.Fatal("LastValidatedAt should not be nil")
	}
	if !decoded.LastValidatedAt.Equal(*entry.LastValidatedAt) {
		t.Errorf("LastValidatedAt: got %v, want %v", *decoded.LastValidatedAt, *entry.LastValidatedAt)
	}
}

func TestLoreEntry_JSONSnakeCaseKeys(t *testing.T) {
	entry := LoreEntry{
		ID:              "01JTEST000000000000000000",
		Content:         "test",
		Category:        "TEST",
		SourceID:        "src",
		Sources:         []string{},
		EmbeddingStatus: "pending",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)

	requiredKeys := []string{
		`"id"`, `"content"`, `"category"`, `"confidence"`,
		`"source_id"`, `"sources"`, `"validation_count"`,
		`"created_at"`, `"updated_at"`, `"embedding_status"`,
	}
	for _, key := range requiredKeys {
		if !strings.Contains(raw, key) {
			t.Errorf("Missing JSON key %s in output: %s", key, raw)
		}
	}

	// Ensure no camelCase keys leak through
	forbiddenKeys := []string{
		`"sourceId"`, `"createdAt"`, `"updatedAt"`, `"deletedAt"`,
		`"lastValidatedAt"`, `"embeddingStatus"`, `"validationCount"`,
	}
	for _, key := range forbiddenKeys {
		if strings.Contains(raw, key) {
			t.Errorf("Found camelCase JSON key %s in output: %s", key, raw)
		}
	}
}

func TestLoreEntry_EmptySourcesMarshalAsArray(t *testing.T) {
	entry := LoreEntry{
		Sources: []string{},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"sources":[]`) {
		t.Errorf("Empty Sources should marshal as [], got: %s", raw)
	}
}

func TestIngestResult_EmptyErrorsMarshalAsArray(t *testing.T) {
	result := IngestResult{
		Accepted: 1,
		Errors:   []string{},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"errors":[]`) {
		t.Errorf("Empty Errors should marshal as [], got: %s", raw)
	}
}

func TestDeltaResult_EmptyArraysMarshalAsArrays(t *testing.T) {
	result := DeltaResult{
		Lore:       []LoreEntry{},
		DeletedIDs: []string{},
		AsOf:       time.Now().UTC(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"lore":[]`) {
		t.Errorf("Empty Lore should marshal as [], got: %s", raw)
	}
	if !strings.Contains(raw, `"deleted_ids":[]`) {
		t.Errorf("Empty DeletedIDs should marshal as [], got: %s", raw)
	}
}

func TestLoreEntry_NilSourcesMarshalAsArray(t *testing.T) {
	// Zero-value slice (nil) must marshal as [] not null
	var entry LoreEntry

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, `"sources":null`) {
		t.Errorf("Nil Sources must not marshal as null, got: %s", raw)
	}
	if !strings.Contains(raw, `"sources":[]`) {
		t.Errorf("Nil Sources should marshal as [], got: %s", raw)
	}
	// Embedding has omitempty but MarshalJSON still coerces nil to []
	if strings.Contains(raw, `"embedding":null`) {
		t.Errorf("Nil Embedding must not marshal as null, got: %s", raw)
	}
}

func TestIngestResult_NilErrorsMarshalAsArray(t *testing.T) {
	var result IngestResult

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, `"errors":null`) {
		t.Errorf("Nil Errors must not marshal as null, got: %s", raw)
	}
	if !strings.Contains(raw, `"errors":[]`) {
		t.Errorf("Nil Errors should marshal as [], got: %s", raw)
	}
}

func TestDeltaResult_NilSlicesMarshalAsArrays(t *testing.T) {
	result := DeltaResult{
		AsOf: time.Now().UTC(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, `"lore":null`) {
		t.Errorf("Nil Lore must not marshal as null, got: %s", raw)
	}
	if !strings.Contains(raw, `"lore":[]`) {
		t.Errorf("Nil Lore should marshal as [], got: %s", raw)
	}
	if strings.Contains(raw, `"deleted_ids":null`) {
		t.Errorf("Nil DeletedIDs must not marshal as null, got: %s", raw)
	}
	if !strings.Contains(raw, `"deleted_ids":[]`) {
		t.Errorf("Nil DeletedIDs should marshal as [], got: %s", raw)
	}
}

func TestTimestamp_RFC3339Serialization(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	entry := LoreEntry{
		Sources:   []string{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	// time.Time marshals as RFC 3339 by default
	if !strings.Contains(raw, "2025-06-15T10:30:00Z") {
		t.Errorf("Expected RFC 3339 timestamp, got: %s", raw)
	}
}

func TestFeedbackEntry_JSONTags(t *testing.T) {
	entry := FeedbackEntry{
		LoreID:   "01JTEST000000000000000000",
		Type:     "helpful",
		SourceID: "src-1",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	requiredKeys := []string{`"lore_id"`, `"type"`, `"source_id"`}
	for _, key := range requiredKeys {
		if !strings.Contains(raw, key) {
			t.Errorf("Missing JSON key %s in output: %s", key, raw)
		}
	}
}

func TestFeedbackResult_OmitsEmptySkipped(t *testing.T) {
	// When Skipped is empty, it should be omitted from JSON output
	result := FeedbackResult{
		Updates: []FeedbackResultUpdate{
			{LoreID: "01JTEST000000000000000000", PreviousConfidence: 0.5, CurrentConfidence: 0.58},
		},
		Skipped: nil,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, `"skipped"`) {
		t.Errorf("Expected skipped to be omitted when nil, got: %s", raw)
	}
	if !strings.Contains(raw, `"updates"`) {
		t.Errorf("Expected updates key in output: %s", raw)
	}
}

func TestFeedbackResult_IncludesSkippedWhenPopulated(t *testing.T) {
	// When Skipped has entries, it should be included in JSON output
	result := FeedbackResult{
		Updates: []FeedbackResultUpdate{},
		Skipped: []FeedbackSkipped{
			{LoreID: "01JTEST000000000000000001", Reason: "not_found"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"skipped"`) {
		t.Errorf("Expected skipped key when populated, got: %s", raw)
	}
	if !strings.Contains(raw, `"not_found"`) {
		t.Errorf("Expected not_found reason in output: %s", raw)
	}
}

func TestFeedbackSkipped_JSONTags(t *testing.T) {
	skipped := FeedbackSkipped{
		LoreID: "01JTEST000000000000000000",
		Reason: "not_found",
	}

	data, err := json.Marshal(skipped)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	requiredKeys := []string{`"lore_id"`, `"reason"`}
	for _, key := range requiredKeys {
		if !strings.Contains(raw, key) {
			t.Errorf("Missing JSON key %s in output: %s", key, raw)
		}
	}
}

func TestNewLoreEntry_Fields(t *testing.T) {
	entry := NewLoreEntry{
		Content:    "test content",
		Context:    "test context",
		Category:   "PATTERN_OUTCOME",
		Confidence: 0.7,
		SourceID:   "src-1",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"source_id"`) {
		t.Errorf("Expected source_id JSON key, got: %s", raw)
	}
}

func TestStoreMetadata_Fields(t *testing.T) {
	meta := StoreMetadata{
		SchemaVersion:  "1.0",
		EmbeddingModel: "text-embedding-3-small",
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"schema_version"`) {
		t.Errorf("Expected schema_version key, got: %s", raw)
	}
	if !strings.Contains(raw, `"embedding_model"`) {
		t.Errorf("Expected embedding_model key, got: %s", raw)
	}
}

func TestStoreStats_Fields(t *testing.T) {
	now := time.Now().UTC()
	stats := StoreStats{
		LoreCount:    42,
		LastSnapshot: &now,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"lore_count"`) {
		t.Errorf("Expected lore_count key, got: %s", raw)
	}
	if !strings.Contains(raw, `"last_snapshot"`) {
		t.Errorf("Expected last_snapshot key, got: %s", raw)
	}
}
