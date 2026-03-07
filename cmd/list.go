package cmd

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/codex"
)

var listProvider string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all accounts with usage",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList(listProvider)
	},
}

func init() {
	listCmd.Flags().StringVarP(&listProvider, "provider", "p", "claude", "provider to show (claude or codex)")
}

func runList(provider string) error {
	switch provider {
	case "claude":
		return runClaudeList()
	case "codex":
		return runCodexList()
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}
}

func runClaudeList() error {
	cfg, err := loadAndSync()
	if err != nil {
		return err
	}

	accounts := cfg.AccountsByProvider("claude")
	if len(accounts) == 0 {
		fmt.Println("No accounts found. Run 'auto-switch login' to add one.")
		return nil
	}

	activeEmail := claude.ActiveEmail()

	fmt.Printf("Claude Code accounts (%d)\n\n", len(accounts))

	// Fetch usage concurrently
	usages := make([]*claude.Usage, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func(idx int, email, token string) {
			defer wg.Done()
			usages[idx] = claude.FetchUsageWithCache(token, email)
		}(i, a.Email, a.Credentials.AccessToken)
	}
	wg.Wait()

	header := fmt.Sprintf("  %-14s %-28s  %-24s  %s", "Alias", "Email", "5h window", "7d window")
	fmt.Println(header)
	fmt.Println("  " + strings.Repeat("─", len(header)))

	for i, a := range accounts {
		u := usages[i]

		// Mark active account
		marker := "  "
		if a.Email == activeEmail {
			marker = "* "
		}

		// Usage columns
		var fhStr, sdStr string
		if u.Error != "" {
			fhStr = fmt.Sprintf("%-24s", "fetch failed")
			sdStr = fmt.Sprintf("%-24s", "-")
		} else {
			age := ""
			if u.Cached {
				age = " ~" + u.CacheAge()
			}
			fhBar := claude.ProgressBar(u.FiveHourUtilization, 8)
			sdBar := claude.ProgressBar(u.SevenDayUtilization, 8)
			fhStr = fmt.Sprintf("%s %3.0f%% ↺%-6s%s", fhBar, u.FiveHourUtilization, claude.FormatResetIn(u.FiveHourResetsAt), age)
			sdStr = fmt.Sprintf("%s %3.0f%% ↺%-6s", sdBar, u.SevenDayUtilization, claude.FormatResetIn(u.SevenDayResetsAt))
		}

		fmt.Printf("%s%-14s %-28s  %-24s  %s\n",
			marker, a.Alias, a.Email, fhStr, sdStr)
	}

	fmt.Printf("\n* active account  refreshed at %s\n", time.Now().Format("15:04:05"))
	return nil
}

func runCodexList() error {
	cfg, err := loadAndSync()
	if err != nil {
		return err
	}

	accounts := cfg.AccountsByProvider("codex")
	if len(accounts) == 0 {
		fmt.Println("No Codex accounts found. Run 'auto-switch login --provider codex' to add one.")
		return nil
	}

	activeAccountID := codex.ActiveAccountID()

	fmt.Printf("Codex accounts (%d)\n\n", len(accounts))

	usages := make([]*codex.Usage, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func(idx int, alias string) {
			defer wg.Done()
			usages[idx] = codex.FetchUsageFromHome(codex.AccountHome(alias))
		}(i, a.Alias)
	}
	wg.Wait()

	header := fmt.Sprintf("  %-14s %-28s  %-24s  %s", "Alias", "Email", "5h window", "7d window")
	fmt.Println(header)
	fmt.Println("  " + strings.Repeat("─", len(header)))

	for i, a := range accounts {
		u := usages[i]

		marker := "  "
		if a.Credentials.AccountID != "" && a.Credentials.AccountID == activeAccountID {
			marker = "* "
		}

		var fhStr, sdStr string
		if u.Error != "" {
			fhStr = fmt.Sprintf("%-24s", "usage unavailable")
			sdStr = fmt.Sprintf("%-24s", "-")
		} else {
			age := ""
			if cacheAge := u.CacheAge(); cacheAge != "" {
				age = " ~" + cacheAge
			}
			fhBar := codex.ProgressBar(u.PrimaryUtilization, 8)
			sdBar := codex.ProgressBar(u.SecondaryUtilization, 8)
			fhStr = fmt.Sprintf("%s %3.0f%% ↺%-6s%s", fhBar, u.PrimaryUtilization, codex.FormatResetIn(u.PrimaryResetsAt), age)
			sdStr = fmt.Sprintf("%s %3.0f%% ↺%-6s", sdBar, u.SecondaryUtilization, codex.FormatResetIn(u.SecondaryResetsAt))
		}

		fmt.Printf("%s%-14s %-28s  %-24s  %s\n",
			marker, a.Alias, a.Email, fhStr, sdStr)
	}

	fmt.Printf("\n* active account  refreshed at %s\n", time.Now().Format("15:04:05"))
	return nil
}
