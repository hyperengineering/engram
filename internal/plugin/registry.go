package plugin

import (
	"sync"
)

// registry holds all registered domain plugins.
var (
	registryMu sync.RWMutex
	plugins    = make(map[string]DomainPlugin)
	generic    DomainPlugin // fallback plugin
)

// Register adds a domain plugin to the registry.
// Plugins should be registered during init() or early in main().
// Panics if a plugin with the same type is already registered.
func Register(p DomainPlugin) {
	registryMu.Lock()
	defer registryMu.Unlock()

	t := p.Type()
	if _, exists := plugins[t]; exists {
		panic("plugin already registered: " + t)
	}
	plugins[t] = p
}

// Get returns the plugin for the given store type.
// If no type-specific plugin is registered, returns the generic plugin.
// The boolean indicates whether a type-specific plugin was found.
func Get(storeType string) (DomainPlugin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if p, ok := plugins[storeType]; ok {
		return p, true
	}
	return generic, false
}

// MustGet returns the plugin for the given store type.
// Panics if no plugin is found and no generic plugin is registered.
func MustGet(storeType string) DomainPlugin {
	p, _ := Get(storeType)
	if p == nil {
		panic("no plugin for store type: " + storeType)
	}
	return p
}

// SetGeneric sets the fallback plugin used when no type-specific plugin exists.
// Should be called during initialization before any Get() calls.
func SetGeneric(p DomainPlugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	generic = p
}

// RegisteredTypes returns all registered plugin types.
// Useful for debugging and health checks.
func RegisteredTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	types := make([]string, 0, len(plugins))
	for t := range plugins {
		types = append(types, t)
	}
	return types
}

// Reset clears the registry. Only for testing.
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	plugins = make(map[string]DomainPlugin)
	generic = nil
}
