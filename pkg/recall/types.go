package recall

import (
	"time"
)

// LoreCategory represents the classification of lore type
type LoreCategory string

const (
	CategoryArchitecturalDecision  LoreCategory = "ARCHITECTURAL_DECISION"
	CategoryPatternOutcome         LoreCategory = "PATTERN_OUTCOME"
	CategoryInterfaceLesson        LoreCategory = "INTERFACE_LESSON"
	CategoryEdgeCaseDiscovery      LoreCategory = "EDGE_CASE_DISCOVERY"
	CategoryImplementationFriction LoreCategory = "IMPLEMENTATION_FRICTION"
	CategoryTestingStrategy        LoreCategory = "TESTING_STRATEGY"
	CategoryDependencyBehavior     LoreCategory = "DEPENDENCY_BEHAVIOR"
	CategoryPerformanceInsight     LoreCategory = "PERFORMANCE_INSIGHT"
)

// Config holds the Recall client configuration
type Config struct {
	LocalPath    string        // Local lore database path
	EngramURL    string        // Engram central service URL
	APIKey       string        // API key for authentication
	SyncInterval time.Duration // Sync interval (default: 5 minutes)
	AutoSync     bool          // Enable automatic sync (default: true)
	OfflineMode  bool          // Enable offline mode (default: false)
}

// Lore represents a discrete unit of experiential knowledge
type Lore struct {
	ID              string       `json:"id"`
	Content         string       `json:"content"`
	Context         string       `json:"context,omitempty"`
	Category        LoreCategory `json:"category"`
	Confidence      float64      `json:"confidence"`
	ValidationCount int          `json:"validation_count"`
	LastValidated   *time.Time   `json:"last_validated,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// RecordParams holds parameters for recording lore
type RecordParams struct {
	Content    string       // The lore itself
	Context    string       // Where/when this was learned
	Category   LoreCategory // Classification
	Confidence float64      // Reliability score (0.0-1.0)
}

// QueryParams holds parameters for querying lore
type QueryParams struct {
	Query         string         // Semantic query
	K             int            // Number of results (default: 5)
	MinConfidence float64        // Minimum confidence threshold (default: 0.5)
	Categories    []LoreCategory // Filter by categories
}

// QueryResult holds the results of a query
type QueryResult struct {
	Lore        []Lore            // Matched lore entries
	SessionRefs map[string]string // e.g., {"L1": "01HXK4...", "L2": "01HXK5..."}
}

// FeedbackParams holds parameters for providing feedback
type FeedbackParams struct {
	Helpful     []string // Session refs, content snippets, or IDs
	NotRelevant []string // Surfaced but didn't apply
	Incorrect   []string // Wrong or misleading
}

// FeedbackResult holds the results of feedback submission
type FeedbackResult struct {
	Updated []FeedbackUpdate
}

// FeedbackUpdate represents a single feedback update
type FeedbackUpdate struct {
	ID              string
	Previous        float64
	Current         float64
	ValidationCount int
}

// SessionLore represents lore surfaced during a session
type SessionLore struct {
	SessionRef string       // L1, L2, etc.
	ID         string       // Lore ID
	Content    string       // First 100 chars for recognition
	Category   LoreCategory // Classification
	Confidence float64      // Current confidence
	Source     string       // "passive" or "query"
}

// SyncStats holds sync operation statistics
type SyncStats struct {
	Pulled   int
	Pushed   int
	Merged   int
	Errors   int
	Duration time.Duration
}

// StoreStats holds store statistics
type StoreStats struct {
	LoreCount    int
	PendingSync  int
	LastSync     *time.Time
	DatabaseSize int64
}

// HealthStatus represents the health status
type HealthStatus struct {
	LocalStore  bool
	CentralSync bool
	LastError   string
}
