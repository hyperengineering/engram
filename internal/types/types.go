package types

import (
	"encoding/json"
	"time"
)

// LoreCategory represents the classification of lore type
type LoreCategory string

const (
	CategoryArchitecturalDecision LoreCategory = "ARCHITECTURAL_DECISION"
	CategoryPatternOutcome        LoreCategory = "PATTERN_OUTCOME"
	CategoryInterfaceLesson       LoreCategory = "INTERFACE_LESSON"
	CategoryEdgeCaseDiscovery     LoreCategory = "EDGE_CASE_DISCOVERY"
	CategoryImplementationFriction LoreCategory = "IMPLEMENTATION_FRICTION"
	CategoryTestingStrategy       LoreCategory = "TESTING_STRATEGY"
	CategoryDependencyBehavior    LoreCategory = "DEPENDENCY_BEHAVIOR"
	CategoryPerformanceInsight    LoreCategory = "PERFORMANCE_INSIGHT"
)

// Lore represents a discrete unit of experiential knowledge
type Lore struct {
	ID              string       `json:"id"`
	Content         string       `json:"content"`
	Context         string       `json:"context,omitempty"`
	Category        LoreCategory `json:"category"`
	Confidence      float64      `json:"confidence"`
	Embedding       []byte       `json:"-"`
	SourceID        string       `json:"source_id"`
	Sources         []string     `json:"sources,omitempty"`
	ValidationCount int          `json:"validation_count"`
	LastValidated   *time.Time   `json:"last_validated,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	SyncedAt        *time.Time   `json:"synced_at,omitempty"`
}

// FeedbackOutcome represents the type of feedback for lore
type FeedbackOutcome string

const (
	FeedbackHelpful     FeedbackOutcome = "helpful"
	FeedbackNotRelevant FeedbackOutcome = "not_relevant"
	FeedbackIncorrect   FeedbackOutcome = "incorrect"
)

// IngestRequest represents a request to ingest lore
type IngestRequest struct {
	SourceID string `json:"source_id"`
	Lore     []Lore `json:"lore"`
	Flush    bool   `json:"flush,omitempty"`
}

// IngestResponse represents the response from ingesting lore
type IngestResponse struct {
	Accepted int      `json:"accepted"`
	Merged   int      `json:"merged"`
	Rejected int      `json:"rejected"`
	Errors   []string `json:"errors"`
}

// FeedbackItem represents a single feedback entry
type FeedbackItem struct {
	ID      string          `json:"id"`
	Outcome FeedbackOutcome `json:"outcome"`
}

// FeedbackRequest represents a request to provide feedback on lore
type FeedbackRequest struct {
	SourceID string         `json:"source_id"`
	Feedback []FeedbackItem `json:"feedback"`
}

// FeedbackUpdate represents the result of a feedback update
type FeedbackUpdate struct {
	ID              string  `json:"id"`
	Previous        float64 `json:"previous"`
	Current         float64 `json:"current"`
	ValidationCount int     `json:"validation_count"`
}

// FeedbackResponse represents the response from feedback submission
type FeedbackResponse struct {
	Updates []FeedbackUpdate `json:"updates"`
}

// DeltaResponse represents the response from a delta sync request
type DeltaResponse struct {
	Lore       []Lore   `json:"lore"`
	DeletedIDs []string `json:"deleted_ids"`
	AsOf       string   `json:"as_of"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status         string     `json:"status"`
	Version        string     `json:"version"`
	EmbeddingModel string     `json:"embedding_model"`
	LoreCount      int64      `json:"lore_count"`
	LastSnapshot   *time.Time `json:"last_snapshot"`
}

// --- Architecture-aligned domain types (Story 1.1) ---

// LoreEntry represents a discrete unit of experiential knowledge in the domain contract.
type LoreEntry struct {
	ID              string     `json:"id"`
	Content         string     `json:"content"`
	Context         string     `json:"context,omitempty"`
	Category        string     `json:"category"`
	Confidence      float64    `json:"confidence"`
	Embedding       []float32  `json:"embedding,omitempty"`
	SourceID        string     `json:"source_id"`
	Sources         []string   `json:"sources"`
	ValidationCount int        `json:"validation_count"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	LastValidatedAt *time.Time `json:"last_validated_at,omitempty"`
	EmbeddingStatus string     `json:"embedding_status"`
}

// NewLoreEntry is the input type for creating lore entries (without generated fields).
type NewLoreEntry struct {
	Content    string  `json:"content"`
	Context    string  `json:"context,omitempty"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	SourceID   string  `json:"source_id"`
}

// IngestResult represents the outcome of an ingest operation.
type IngestResult struct {
	Accepted int      `json:"accepted"`
	Merged   int      `json:"merged"`
	Rejected int      `json:"rejected"`
	Errors   []string `json:"errors"`
}

// DeltaResult represents the response from a delta sync query.
type DeltaResult struct {
	Lore       []LoreEntry `json:"lore"`
	DeletedIDs []string    `json:"deleted_ids"`
	AsOf       time.Time   `json:"as_of"`
}

// FeedbackEntry represents a single feedback submission.
type FeedbackEntry struct {
	LoreID   string `json:"lore_id"`
	Type     string `json:"type"`
	SourceID string `json:"source_id"`
}

// FeedbackResult represents the outcome of recording feedback.
type FeedbackResult struct {
	Updates []FeedbackResultUpdate `json:"updates"`
}

// FeedbackResultUpdate represents a single confidence change from feedback.
type FeedbackResultUpdate struct {
	LoreID             string  `json:"lore_id"`
	PreviousConfidence float64 `json:"previous_confidence"`
	CurrentConfidence  float64 `json:"current_confidence"`
}

// StoreMetadata holds store-level metadata.
type StoreMetadata struct {
	SchemaVersion  string `json:"schema_version"`
	EmbeddingModel string `json:"embedding_model"`
}

// StoreStats holds aggregate store statistics.
type StoreStats struct {
	LoreCount    int64      `json:"lore_count"`
	LastSnapshot *time.Time `json:"last_snapshot,omitempty"`
}

// SimilarEntry represents a lore entry with its similarity score.
type SimilarEntry struct {
	LoreEntry
	Similarity float64 `json:"similarity"`
}

// MarshalJSON ensures nil slices in LoreEntry marshal as [] not null.
func (l LoreEntry) MarshalJSON() ([]byte, error) {
	if l.Sources == nil {
		l.Sources = []string{}
	}
	if l.Embedding == nil {
		l.Embedding = []float32{}
	}
	type Alias LoreEntry
	return json.Marshal(Alias(l))
}

// MarshalJSON ensures nil slices in IngestResult marshal as [] not null.
func (r IngestResult) MarshalJSON() ([]byte, error) {
	if r.Errors == nil {
		r.Errors = []string{}
	}
	type Alias IngestResult
	return json.Marshal(Alias(r))
}

// MarshalJSON ensures nil slices in DeltaResult marshal as [] not null.
func (d DeltaResult) MarshalJSON() ([]byte, error) {
	if d.Lore == nil {
		d.Lore = []LoreEntry{}
	}
	if d.DeletedIDs == nil {
		d.DeletedIDs = []string{}
	}
	type Alias DeltaResult
	return json.Marshal(Alias(d))
}
