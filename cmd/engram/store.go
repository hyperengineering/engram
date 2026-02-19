package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/hyperengineering/engram/internal/config"
	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/spf13/cobra"
)

var (
	storeRootOverride string
	storeJSONOutput   bool
)

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Manage Engram stores",
	Long:  "Create, list, inspect, and delete Engram stores without running the server.",
}

func init() {
	storeCmd.PersistentFlags().StringVar(&storeRootOverride, "root", "",
		"Store root path (overrides config and ENGRAM_STORES_ROOT)")
	storeCmd.PersistentFlags().BoolVar(&storeJSONOutput, "json", false,
		"Output in JSON format")

	storeCmd.AddCommand(storeCreateCmd)
	storeCmd.AddCommand(storeListCmd)
	storeCmd.AddCommand(storeInfoCmd)
	storeCmd.AddCommand(storeDeleteCmd)
}

// resolveStoreManager creates a StoreManager from config with optional --root override.
// Calls initPlugins() to ensure schema migrations are available.
func resolveStoreManager() (*multistore.StoreManager, error) {
	initPlugins()

	rootPath := storeRootOverride
	if rootPath == "" {
		storesCfg, err := config.LoadStoresConfig()
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		rootPath = storesCfg.RootPath
	}

	return multistore.NewStoreManager(rootPath)
}

// printJSON marshals v to JSON and writes to the given writer.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// newTabWriter returns a configured tabwriter for aligned columns.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

// formatSize returns a human-readable file size.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
