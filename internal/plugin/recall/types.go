package recall

import "encoding/json"

// LorePayload represents the JSON structure of a lore_entries row.
// Used for validation during sync push.
type LorePayload struct {
	ID              string          `json:"id"`
	Content         string          `json:"content"`
	Context         string          `json:"context,omitempty"`
	Category        string          `json:"category"`
	Confidence      float64         `json:"confidence"`
	Embedding       json.RawMessage `json:"embedding,omitempty"`
	EmbeddingStatus string          `json:"embedding_status,omitempty"`
	SourceID        string          `json:"source_id"`
	Sources         []string        `json:"sources,omitempty"`
	ValidationCount int             `json:"validation_count,omitempty"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	DeletedAt       *string         `json:"deleted_at,omitempty"`
	LastValidatedAt *string         `json:"last_validated_at,omitempty"`
}

// ValidCategories defines the allowed lore categories.
var ValidCategories = map[string]bool{
	"ARCHITECTURAL_DECISION":  true,
	"PATTERN_OUTCOME":         true,
	"INTERFACE_LESSON":        true,
	"EDGE_CASE_DISCOVERY":     true,
	"IMPLEMENTATION_FRICTION": true,
	"TESTING_STRATEGY":        true,
	"DEPENDENCY_BEHAVIOR":     true,
	"PERFORMANCE_INSIGHT":     true,
}

// isValidCategory checks if a category is in the allowed set.
func isValidCategory(category string) bool {
	return ValidCategories[category]
}
