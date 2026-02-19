package main

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/spf13/cobra"
)

var deleteForce bool

var storeDeleteCmd = &cobra.Command{
	Use:   "delete <store-id>",
	Short: "Delete a store and all its data",
	Long:  "Permanently delete a store and all its data. The default store cannot be deleted. Requires --force or interactive confirmation.",
	Args:  cobra.ExactArgs(1),
	RunE:  runStoreDelete,
}

func init() {
	storeDeleteCmd.Flags().BoolVar(&deleteForce, "force", false,
		"Skip confirmation prompt")
}

func runStoreDelete(cmd *cobra.Command, args []string) error {
	storeID := args[0]
	ctx := context.Background()

	// Early check: prevent default store deletion with a clear message
	if multistore.IsDefaultStore(storeID) {
		return fmt.Errorf("cannot delete the default store")
	}

	mgr, err := resolveStoreManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	// Validate store ID
	if err := multistore.ValidateStoreID(storeID); err != nil {
		return err
	}

	// Interactive confirmation unless --force
	if !deleteForce {
		errOut := cmd.ErrOrStderr()
		fmt.Fprintf(errOut, "WARNING: This will permanently delete store %q and all its data.\n", storeID)
		fmt.Fprint(errOut, "Type the store ID to confirm: ")

		reader := bufio.NewReader(cmd.InOrStdin())
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}

		if strings.TrimSpace(input) != storeID {
			fmt.Fprintln(errOut, "Aborted. Store ID did not match.")
			return nil
		}
	}

	if err := mgr.DeleteStore(ctx, storeID); err != nil {
		return err
	}

	if storeJSONOutput {
		return printJSON(cmd.OutOrStdout(), map[string]any{
			"id":      storeID,
			"deleted": true,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted store %q\n", storeID)
	return nil
}
