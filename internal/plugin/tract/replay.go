package tract

import (
	"context"
	"fmt"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// onReplay dispatches change log entries to the appropriate domain tables.
// Unlike Recall, Tract does NOT queue embeddings — Tract entities are structural,
// not semantic.
func onReplay(ctx context.Context, store plugin.ReplayStore, entries []sync.ChangeLogEntry) error {
	for _, entry := range entries {
		if !allowedTables[entry.TableName] {
			// Skip unknown tables (same pattern as Recall plugin)
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
