package tract

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// tableNameRegex validates table names to prevent SQL injection.
// Allows lowercase letters, digits, and underscores; must start with a letter.
var tableNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validatePayload validates the payload for an upsert operation.
// Only checks that the payload is present and valid JSON.
// Field-level validation is intentionally omitted because the Tract CLI
// schema evolves independently of the server.
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

	return errs
}
