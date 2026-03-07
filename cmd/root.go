package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "auto-switch",
	Short: "Automatically switch AI coding assistant accounts",
	Long:  `auto-switch manages multiple Claude Code / Codex accounts and automatically selects the account with the lowest usage.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(claudeCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(statusCmd)
}
