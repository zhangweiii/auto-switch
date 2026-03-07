package cmd

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/store"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all accounts with usage",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList()
	},
}

func runList() error {
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

	header := fmt.Sprintf("  %-14s %-28s  %-24s  %-24s  %s", "Alias", "Email", "5h window", "7d window", "Expires")
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

		// Token expiry
		expDays := store.Credentials{ExpiresAt: a.Credentials.ExpiresAt}.DaysUntilExpiry()
		expStr := ""
		if expDays < 0 {
			expStr = "expired!"
		} else if expDays < 30 {
			expStr = fmt.Sprintf("%dd", expDays)
		}

		fmt.Printf("%s%-14s %-28s  %-24s  %-24s  %s\n",
			marker, a.Alias, a.Email, fhStr, sdStr, expStr)
	}

	fmt.Printf("\n* active account  refreshed at %s\n", time.Now().Format("15:04:05"))
	return nil
}
