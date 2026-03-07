package cmd

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/claude"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show real-time usage for all accounts",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func runStatus() error {
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

	fmt.Println("Fetching usage...")
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

	fmt.Println()
	fmt.Printf("Claude Code usage  (%s)\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println(strings.Repeat("─", 60))

	for i, a := range accounts {
		u := usages[i]

		activeMark := ""
		if a.Email == activeEmail {
			activeMark = " [active]"
		}
		fmt.Printf("\n%s (%s)%s\n", a.Alias, a.Email, activeMark)

		if u.Error != "" {
			fmt.Printf("  usage fetch failed: %s\n", u.Error)
			continue
		}

		cacheNote := ""
		if u.Cached {
			cacheNote = fmt.Sprintf(" (cached %s ago)", u.CacheAge())
		}

		fhBar := claude.ProgressBar(u.FiveHourUtilization, 20)
		sdBar := claude.ProgressBar(u.SevenDayUtilization, 20)

		fmt.Printf("  5h window: %s %5.1f%%  resets in %s%s\n",
			fhBar, u.FiveHourUtilization, claude.FormatResetIn(u.FiveHourResetsAt), cacheNote)
		fmt.Printf("  7d window: %s %5.1f%%  resets in %s\n",
			sdBar, u.SevenDayUtilization, claude.FormatResetIn(u.SevenDayResetsAt))

		// Token expiry warning
		days := a.Credentials.DaysUntilExpiry()
		if days < 30 {
			if days < 0 {
				fmt.Println("  ⚠️  Token expired, please log in again")
			} else {
				fmt.Printf("  ⚠️  Token expires in %d day(s)\n", days)
			}
		}
	}

	fmt.Println()
	return nil
}
