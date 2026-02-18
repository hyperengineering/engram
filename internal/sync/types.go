package sync

import (
	"encoding/json"
	"time"
)

// ChangeLogEntry represents a single entry in the change log.
type ChangeLogEntry struct {
	Sequence   int64           `json:"sequence"`
	TableName  string          `json:"table_name"`
	EntityID   string          `json:"entity_id"`
	Operation  string          `json:"operation"` // "upsert" or "delete"
	Payload    json.RawMessage `json:"payload,omitempty"`
	SourceID   string          `json:"source_id"`
	CreatedAt  time.Time       `json:"created_at"`
	ReceivedAt time.Time       `json:"received_at"`
}

// Operation constants
const (
	OperationUpsert = "upsert"
	OperationDelete = "delete"
)

// PushIdempotencyEntry tracks a processed push for idempotency.
type PushIdempotencyEntry struct {
	PushID    string    `json:"push_id"`
	StoreID   string    `json:"store_id"`
	Response  string    `json:"response"` // JSON-encoded PushResponse
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SyncMeta keys
const (
	SyncMetaSchemaVersion     = "schema_version"
	SyncMetaLastCompactionSeq = "last_compaction_seq"
	SyncMetaLastCompactionAt  = "last_compaction_at"
)

// PushRequest is the request body for POST /sync/push.
type PushRequest struct {
	PushID        string           `json:"push_id"`
	SourceID      string           `json:"source_id"`
	SchemaVersion int              `json:"schema_version"`
	Entries       []ChangeLogEntry `json:"entries"`
}

// PushResponse is the success response for POST /sync/push.
type PushResponse struct {
	Accepted       int   `json:"accepted"`
	RemoteSequence int64 `json:"remote_sequence"`
}

// PushError represents a single entry error in a failed push.
type PushError struct {
	Sequence  int64  `json:"sequence"`
	TableName string `json:"table_name"`
	EntityID  string `json:"entity_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// PushErrorResponse is the failure response for POST /sync/push.
type PushErrorResponse struct {
	Accepted int         `json:"accepted"`
	Errors   []PushError `json:"errors"`
}

// Error codes for push errors
const (
	PushErrorValidation   = "VALIDATION_ERROR"
	PushErrorUnknownTable = "UNKNOWN_TABLE"
	PushErrorMissingField = "MISSING_FIELD"
	PushErrorInvalidFormat = "INVALID_FORMAT"
)

// DeltaRequest parameters (parsed from query string).
type DeltaRequest struct {
	After int64
	Limit int
}

// DeltaResponse is the response for GET /sync/delta.
type DeltaResponse struct {
	Entries        []ChangeLogEntry `json:"entries"`
	LastSequence   int64            `json:"last_sequence"`
	LatestSequence int64            `json:"latest_sequence"`
	HasMore        bool             `json:"has_more"`
}

// Default and max limits for delta queries.
const (
	DefaultDeltaLimit = 500
	MaxDeltaLimit     = 1000
)
