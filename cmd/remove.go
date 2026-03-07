package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/codex"
	"github.com/zhangweiii/auto-switch/internal/store"
)

var removeProvider string

var removeCmd = &cobra.Command{
	Use:   "remove <alias>",
	Short: "Remove a saved account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		alias := args[0]
		cfg, err := store.Load()
		if err != nil {
			return err
		}
		if !cfg.RemoveByAlias(alias, removeProvider) {
			return fmt.Errorf("account %q not found, run 'auto-switch list' to see saved accounts", alias)
		}
		if err := store.Save(cfg); err != nil {
			return err
		}
		if removeProvider == "codex" {
			_ = os.RemoveAll(codex.AccountHome(alias))
		}
		fmt.Printf("✓ account %q removed\n", alias)
		return nil
	},
}

func init() {
	removeCmd.Flags().StringVarP(&removeProvider, "provider", "p", "claude", "provider to remove from (claude or codex)")
}
