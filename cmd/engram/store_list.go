package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

var storeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stores",
	Args:  cobra.NoArgs,
	RunE:  runStoreList,
}

func runStoreList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	mgr, err := resolveStoreManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	stores, err := mgr.ListStores(ctx)
	if err != nil {
		return fmt.Errorf("list stores: %w", err)
	}

	// Sort by ID
	sort.Slice(stores, func(i, j int) bool {
		return stores[i].ID < stores[j].ID
	})

	if storeJSONOutput {
		items := make([]map[string]any, len(stores))
		for i, s := range stores {
			items[i] = map[string]any{
				"id":            s.ID,
				"type":          s.Type,
				"size_bytes":    s.SizeBytes,
				"created":       s.Created,
				"last_accessed": s.LastAccessed,
				"description":   s.Description,
			}
		}
		return printJSON(cmd.OutOrStdout(), map[string]any{
			"stores": items,
			"total":  len(items),
		})
	}

	if len(stores) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No stores found.")
		return nil
	}

	w := newTabWriter(cmd.OutOrStdout())
	fmt.Fprintln(w, "ID\tTYPE\tSIZE\tCREATED\tDESCRIPTION")
	for _, s := range stores {
		desc := s.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.ID,
			s.Type,
			formatSize(s.SizeBytes),
			s.Created.Format("2006-01-02 15:04"),
			desc,
		)
	}
	w.Flush()

	return nil
}
