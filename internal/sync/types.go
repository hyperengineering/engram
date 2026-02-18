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
