package plugin

import (
	"fmt"
	"regexp"
	"sync"
)

// columnNameRegex validates column names to prevent SQL injection.
var columnNameRegex = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// registry holds all registered domain plugins.
var (
	registryMu sync.RWMutex
	plugins    = make(map[string]DomainPlugin)
	generic    DomainPlugin // fallback plugin
)

// table schema registry
var (
	tableSchemaMu sync.RWMutex
	tableSchemas  = make(map[string]TableSchema) // tableName -> schema
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
	registerTableSchemas(p)
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
	ResetTableSchemas()
}

// registerTableSchemas registers table schemas from a plugin.
// Called automatically during Register() for plugins that declare schemas.
func registerTableSchemas(p DomainPlugin) {
	schemas := p.TableSchemas()
	if schemas == nil {
		return
	}
	tableSchemaMu.Lock()
	defer tableSchemaMu.Unlock()
	for _, s := range schemas {
		// Validate column names at registration time to prevent SQL injection
		for _, col := range s.Columns {
			if !columnNameRegex.MatchString(col) {
				panic(fmt.Sprintf("invalid column name %q in table %q", col, s.Name))
			}
		}
		tableSchemas[s.Name] = s
	}
}

// GetTableSchema returns the schema for the given table name.
// Returns ok=false if no schema is registered (table uses legacy hardcoded path).
func GetTableSchema(tableName string) (TableSchema, bool) {
	tableSchemaMu.RLock()
	defer tableSchemaMu.RUnlock()
	s, ok := tableSchemas[tableName]
	return s, ok
}

// ResetTableSchemas clears all registered schemas. Only for testing.
func ResetTableSchemas() {
	tableSchemaMu.Lock()
	defer tableSchemaMu.Unlock()
	tableSchemas = make(map[string]TableSchema)
}

// RegisterTableSchemas registers individual table schemas directly.
// This is primarily used by tests and store initialization code.
func RegisterTableSchemas(schemas ...TableSchema) {
	tableSchemaMu.Lock()
	defer tableSchemaMu.Unlock()
	for _, s := range schemas {
		// Validate column names at registration time
		for _, col := range s.Columns {
			if !columnNameRegex.MatchString(col) {
				panic(fmt.Sprintf("invalid column name %q in table %q", col, s.Name))
			}
		}
		tableSchemas[s.Name] = s
	}
}
