package tract

import "encoding/json"

// GoalPayload represents the JSON structure of a goals row.
type GoalPayload struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description,omitempty"`
	Status       string  `json:"status"`
	Priority     int     `json:"priority,omitempty"`
	ParentGoalID *string `json:"parent_goal_id"`
	CreatedAt    string  `json:"created_at,omitempty"`
	UpdatedAt    string  `json:"updated_at,omitempty"`
}

// CSFPayload represents the JSON structure of a csfs row.
type CSFPayload struct {
	ID           string `json:"id"`
	GoalID       string `json:"goal_id"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Metric       string `json:"metric,omitempty"`
	TargetValue  string `json:"target_value,omitempty"`
	CurrentValue string `json:"current_value,omitempty"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// FWUPayload represents the JSON structure of a fwus row.
type FWUPayload struct {
	ID              string  `json:"id"`
	CSFID           string  `json:"csf_id"`
	Title           string  `json:"title"`
	Description     string  `json:"description,omitempty"`
	Priority        int     `json:"priority,omitempty"`
	Status          string  `json:"status"`
	EstimatedEffort *string `json:"estimated_effort"`
	ActualEffort    *string `json:"actual_effort"`
	CreatedAt       string  `json:"created_at,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
}

// ICPayload represents the JSON structure of an implementation_contexts row.
type ICPayload struct {
	ID          string          `json:"id"`
	FWUID       string          `json:"fwu_id"`
	ContextType string          `json:"context_type,omitempty"`
	Content     string          `json:"content,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
}
