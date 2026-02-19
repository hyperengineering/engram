package tract

import (
	"encoding/json"
	"fmt"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// allowedTables defines the tables managed by the Tract plugin.
var allowedTables = map[string]bool{
	"goals":                   true,
	"csfs":                    true,
	"fwus":                    true,
	"implementation_contexts": true,
}

// requiredFields defines required fields per table for upsert operations.
var requiredFields = map[string][]string{
	"goals":                   {"id", "title", "status"},
	"csfs":                    {"id", "goal_id", "title", "status"},
	"fwus":                    {"id", "csf_id", "title", "status"},
	"implementation_contexts": {"id", "fwu_id"},
}

// validatePayload validates the payload for an upsert operation.
func validatePayload(entry sync.ChangeLogEntry) []plugin.ValidationError {
	var errs []plugin.ValidationError

	if entry.Payload == nil || len(entry.Payload) == 0 {
		errs = append(errs, plugin.ValidationError{
			Sequence:  entry.Sequence,
			TableName: entry.TableName,
			EntityID:  entry.EntityID,
			Message:   "payload required for upsert",
		})
		return errs
	}

	var data map[string]interface{}
	if err := json.Unmarshal(entry.Payload, &data); err != nil {
		errs = append(errs, plugin.ValidationError{
			Sequence:  entry.Sequence,
			TableName: entry.TableName,
			EntityID:  entry.EntityID,
			Message:   fmt.Sprintf("invalid payload JSON: %v", err),
		})
		return errs
	}

	fields, ok := requiredFields[entry.TableName]
	if !ok {
		return errs
	}

	for _, field := range fields {
		val, exists := data[field]
		if !exists || val == nil {
			errs = append(errs, plugin.ValidationError{
				Sequence:  entry.Sequence,
				TableName: entry.TableName,
				EntityID:  entry.EntityID,
				Field:     field,
				Message:   fmt.Sprintf("missing required field: %s", field),
			})
			continue
		}

		// For string fields, check that the value is a non-empty string
		if str, isStr := val.(string); isStr && str == "" {
			errs = append(errs, plugin.ValidationError{
				Sequence:  entry.Sequence,
				TableName: entry.TableName,
				EntityID:  entry.EntityID,
				Field:     field,
				Message:   fmt.Sprintf("missing required field: %s", field),
			})
		}
	}

	return errs
}
