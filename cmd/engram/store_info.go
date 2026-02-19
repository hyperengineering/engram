package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var storeInfoCmd = &cobra.Command{
	Use:   "info <store-id>",
	Short: "Show detailed information about a store",
	Args:  cobra.ExactArgs(1),
	RunE:  runStoreInfo,
}

func runStoreInfo(cmd *cobra.Command, args []string) error {
	storeID := args[0]
	ctx := context.Background()

	mgr, err := resolveStoreManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	managed, err := mgr.GetStore(ctx, storeID)
	if err != nil {
		return err
	}

	// Get database file size
	dbPath := filepath.Join(managed.BasePath, "engram.db")
	var sizeBytes int64
	if info, statErr := os.Stat(dbPath); statErr == nil {
		sizeBytes = info.Size()
	}

	schemaVersion := managed.SchemaVersion(ctx)

	out := cmd.OutOrStdout()

	if storeJSONOutput {
		return printJSON(out, map[string]any{
			"id":             managed.ID,
			"type":           managed.Type(),
			"description":    managed.Meta.Description,
			"created":        managed.Meta.Created,
			"last_accessed":  managed.Meta.LastAccessed,
			"size_bytes":     sizeBytes,
			"schema_version": schemaVersion,
			"path":           managed.BasePath,
		})
	}

	fmt.Fprintf(out, "Store:         %s\n", managed.ID)
	fmt.Fprintf(out, "Type:          %s\n", managed.Type())
	if managed.Meta.Description != "" {
		fmt.Fprintf(out, "Description:   %s\n", managed.Meta.Description)
	}
	fmt.Fprintf(out, "Created:       %s\n", managed.Meta.Created.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(out, "Last Accessed: %s\n", managed.Meta.LastAccessed.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(out, "Size:          %s\n", formatSize(sizeBytes))
	fmt.Fprintf(out, "Schema:        v%d\n", schemaVersion)
	fmt.Fprintf(out, "Path:          %s\n", managed.BasePath)

	return nil
}
