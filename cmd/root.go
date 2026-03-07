package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"
var accountAlias string

var rootCmd = &cobra.Command{
	Use:              "auto-switch",
	Short:            "Automatically switch AI coding assistant accounts",
	Long:             `auto-switch manages multiple Claude Code / Codex accounts and automatically selects the account with the lowest usage.`,
	Version:          version,
	TraverseChildren: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&accountAlias, "account", "", "force a specific saved account alias for the selected provider")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(claudeCmd)
	rootCmd.AddCommand(codexCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(statusCmd)
}
