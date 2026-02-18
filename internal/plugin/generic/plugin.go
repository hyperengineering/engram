package generic

import (
	"context"

	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/sync"
)

// Plugin is the generic domain plugin.
// It provides pass-through behavior for stores without a specific plugin.
type Plugin struct{}

// New creates a new generic plugin.
func New() *Plugin {
	return &Plugin{}
}

// Type returns "generic".
func (p *Plugin) Type() string {
	return "generic"
}

// Migrations returns nil — generic plugin uses only base schema.
func (p *Plugin) Migrations() []plugin.Migration {
	return nil
}

// ValidatePush returns entries unchanged.
// Generic plugin performs no validation beyond what the sync layer does.
func (p *Plugin) ValidatePush(_ context.Context, entries []sync.ChangeLogEntry) ([]sync.ChangeLogEntry, error) {
	return entries, nil
}

// OnReplay performs no side effects.
// Generic stores have no domain-specific behavior.
func (p *Plugin) OnReplay(_ context.Context, _ plugin.ReplayStore, _ []sync.ChangeLogEntry) error {
	return nil
}

// Ensure Plugin implements DomainPlugin at compile time.
var _ plugin.DomainPlugin = (*Plugin)(nil)
