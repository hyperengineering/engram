package main

import (
	"github.com/hyperengineering/engram/internal/plugin"
	"github.com/hyperengineering/engram/internal/plugin/generic"
	"github.com/hyperengineering/engram/internal/plugin/recall"
	"github.com/hyperengineering/engram/internal/plugin/tract"
)

// initPlugins registers all built-in domain plugins.
// Called early in main() before any store operations.
func initPlugins() {
	// Set generic as fallback for unrecognized store types.
	plugin.SetGeneric(generic.New())

	// Register type-specific plugins.
	plugin.Register(recall.New())
	plugin.Register(tract.New())
}
