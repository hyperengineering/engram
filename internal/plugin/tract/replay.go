package tract

import (
	"context"
	"fmt"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// replayableTables are tables with registered schemas that support domain table replay.
// Tables not in this set are still stored in the change_log but not materialized
// into domain tables. This allows the Tract CLI to evolve its schema independently.
var replayableTables = map[string]bool{
	"goals":                   true,
	"csfs":                    true,
	"fwus":                    true,
	"implementation_contexts": true,
}

// onReplay dispatches change log entries to the appropriate domain tables.
// Unlike Recall, Tract does NOT queue embeddings — Tract entities are structural,
// not semantic.
// Tables without registered schemas are silently skipped (stored in change_log only).
func onReplay(ctx context.Context, store plugin.ReplayStore, entries []sync.ChangeLogEntry) error {
	for _, entry := range entries {
		if !replayableTables[entry.TableName] {
			// Unknown tables are stored in change_log but not replayed
			continue
		}

		switch entry.Operation {
		case sync.OperationUpsert:
			if err := store.UpsertRow(ctx, entry.TableName, entry.EntityID, entry.Payload); err != nil {
				return fmt.Errorf("upsert %s/%s: %w", entry.TableName, entry.EntityID, err)
			}
			// No embedding queuing for Tract tables

		case sync.OperationDelete:
			if err := store.DeleteRow(ctx, entry.TableName, entry.EntityID); err != nil {
				return fmt.Errorf("delete %s/%s: %w", entry.TableName, entry.EntityID, err)
			}
		}
	}
	return nil
}
