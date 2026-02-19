package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/spf13/cobra"
)

var (
	createType        string
	createDescription string
	createIfNotExists bool
)

var storeCreateCmd = &cobra.Command{
	Use:   "create <store-id>",
	Short: "Create a new store",
	Long:  "Create a new Engram store with the given ID. Store IDs are lowercase alphanumeric with hyphens, optionally separated by / for namespacing (e.g., org/project).",
	Args:  cobra.ExactArgs(1),
	RunE:  runStoreCreate,
}

func init() {
	storeCreateCmd.Flags().StringVar(&createType, "type", "",
		"Store type: recall, tract (default: recall)")
	storeCreateCmd.Flags().StringVar(&createDescription, "description", "",
		"Human-readable description")
	storeCreateCmd.Flags().BoolVar(&createIfNotExists, "if-not-exists", false,
		"Exit 0 if store already exists")
}

func runStoreCreate(cmd *cobra.Command, args []string) error {
	storeID := args[0]
	ctx := context.Background()

	mgr, err := resolveStoreManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	managed, err := mgr.CreateStore(ctx, storeID, createType, createDescription)
	if err != nil {
		if errors.Is(err, multistore.ErrStoreAlreadyExists) && createIfNotExists {
			// Idempotent mode: load existing store and report it
			existing, loadErr := mgr.GetStore(ctx, storeID)
			if loadErr != nil {
				return fmt.Errorf("store exists but could not be loaded: %w", loadErr)
			}
			if storeJSONOutput {
				return printJSON(cmd.OutOrStdout(), map[string]any{
					"id":              existing.ID,
					"type":            existing.Type(),
					"created":         existing.Meta.Created,
					"description":     existing.Meta.Description,
					"already_existed": true,
				})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Store %q already exists (type: %s)\n", storeID, existing.Type())
			return nil
		}
		return err
	}

	if storeJSONOutput {
		return printJSON(cmd.OutOrStdout(), map[string]any{
			"id":          managed.ID,
			"type":        managed.Type(),
			"created":     managed.Meta.Created,
			"description": managed.Meta.Description,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created store %q (type: %s)\n", managed.ID, managed.Type())
	return nil
}
