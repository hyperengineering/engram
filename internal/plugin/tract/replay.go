package tract

import (
	"context"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// onReplay is a no-op for the Tract plugin.
// All entries are stored in the change_log (by the sync handler) which is the
// source of truth for client sync. Domain table replay is skipped because the
// Tract CLI schema evolves independently and the server's domain table
// migrations may not match the client's actual payload structure.
func onReplay(_ context.Context, _ plugin.ReplayStore, _ []sync.ChangeLogEntry) error {
	return nil
}
