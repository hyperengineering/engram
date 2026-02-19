package recall

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// Plugin implements the DomainPlugin interface for Recall stores.
type Plugin struct{}

// New creates a new Recall plugin.
func New() *Plugin {
	return &Plugin{}
}

// Type returns "recall".
func (p *Plugin) Type() string {
	return "recall"
}

// Migrations returns nil — Recall uses only the base schema.
func (p *Plugin) Migrations() []plugin.Migration {
	return nil
}

// ValidatePush validates change log entries for lore_entries table.
// Returns entries unchanged (no reordering needed for single-table store).
func (p *Plugin) ValidatePush(_ context.Context, entries []sync.ChangeLogEntry) ([]sync.ChangeLogEntry, error) {
	var validationErrors []plugin.ValidationError

	for _, entry := range entries {
		if entry.TableName != "lore_entries" {
			validationErrors = append(validationErrors, plugin.ValidationError{
				Sequence:  entry.Sequence,
				TableName: entry.TableName,
				EntityID:  entry.EntityID,
				Message:   fmt.Sprintf("unknown table %q for recall store", entry.TableName),
			})
			continue
		}

		if entry.Operation == sync.OperationUpsert {
			if err := p.validateLorePayload(entry); err != nil {
				validationErrors = append(validationErrors, plugin.ValidationError{
					Sequence:  entry.Sequence,
					TableName: entry.TableName,
					EntityID:  entry.EntityID,
					Message:   err.Error(),
				})
			}
		}
	}

	if len(validationErrors) > 0 {
		return nil, plugin.ValidationErrors{Errors: validationErrors}
	}

	return entries, nil
}

// validateLorePayload checks that a lore_entries payload has required fields.
func (p *Plugin) validateLorePayload(entry sync.ChangeLogEntry) error {
	if entry.Payload == nil {
		return fmt.Errorf("payload required for upsert")
	}

	// Detect double-encoded JSON in payload.
	// If the raw bytes start with `"` (quoted string) and parse to a string
	// that itself is valid JSON, the payload is double-encoded.
	if len(entry.Payload) > 0 && entry.Payload[0] == '"' {
		var stringPayload string
		if json.Unmarshal(entry.Payload, &stringPayload) == nil {
			var nested map[string]interface{}
			if json.Unmarshal([]byte(stringPayload), &nested) == nil {
				return fmt.Errorf("payload is double-encoded JSON string; send raw JSON object, not a stringified JSON")
			}
		}
	}

	var payload LorePayload
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload JSON: %w", err)
	}

	if payload.ID == "" {
		return fmt.Errorf("missing required field: id")
	}
	if payload.Content == "" {
		return fmt.Errorf("missing required field: content")
	}
	if payload.Category == "" {
		return fmt.Errorf("missing required field: category")
	}
	if payload.SourceID == "" {
		return fmt.Errorf("missing required field: source_id")
	}

	if !isValidCategory(payload.Category) {
		return fmt.Errorf("invalid category: %s", payload.Category)
	}

	if payload.Confidence < 0 || payload.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}

	return nil
}

// OnReplay applies change log entries to the lore_entries table.
//
// Entries are processed individually without a transaction wrapper.
// If entry N fails, entries 1..N-1 are already committed. This is
// acceptable because replay is idempotent — re-running the same
// entries converges to the correct state.
func (p *Plugin) OnReplay(ctx context.Context, store plugin.ReplayStore, entries []sync.ChangeLogEntry) error {
	for _, entry := range entries {
		if entry.TableName != "lore_entries" {
			continue
		}

		switch entry.Operation {
		case sync.OperationUpsert:
			if err := store.UpsertRow(ctx, entry.TableName, entry.EntityID, entry.Payload); err != nil {
				return fmt.Errorf("upsert %s: %w", entry.EntityID, err)
			}
			// Queue embedding generation for synced entries.
			// Errors are non-fatal — the embedding retry worker handles failures.
			_ = store.QueueEmbedding(ctx, entry.EntityID)

		case sync.OperationDelete:
			if err := store.DeleteRow(ctx, entry.TableName, entry.EntityID); err != nil {
				return fmt.Errorf("delete %s: %w", entry.EntityID, err)
			}
		}
	}

	return nil
}

// Ensure Plugin implements DomainPlugin at compile time.
var _ plugin.DomainPlugin = (*Plugin)(nil)
