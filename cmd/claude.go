package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/store"
)

var claudeCmd = &cobra.Command{
	Use:                "claude [args...]",
	Short:              "Switch to the least-used Claude account and launch claude",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runClaude(accountAlias, args)
	},
}

func runClaude(accountAlias string, args []string) error {
	cfg, err := loadAndSync()
	if err != nil {
		return err
	}
	if err := refreshClaudeCredentials(cfg, claude.ActiveEmail()); err != nil {
		return err
	}

	accounts := cfg.AccountsByProvider("claude")
	if len(accounts) == 0 {
		return fmt.Errorf("no Claude accounts found, run 'auto-switch login' first")
	}

	// Forced account
	if accountAlias != "" {
		a := cfg.FindByAlias(accountAlias, "claude")
		if a == nil {
			return fmt.Errorf("account %q not found, run 'auto-switch list' to see saved accounts", accountAlias)
		}
		fmt.Printf("→ using specified account: %s (%s)\n", a.Alias, a.Email)
		return switchAndLaunch(*a, args)
	}

	// Single account: switch silently
	if len(accounts) == 1 {
		return switchAndLaunch(accounts[0], args)
	}

	// Multiple accounts: fetch usage concurrently and pick the best
	fmt.Printf("Checking usage for %d accounts...\n", len(accounts))
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
	bestIdx := -1
	bestScore := 999.0

	for i, a := range accounts {
		u := usages[i]
		if u.Error != "" {
			fmt.Printf("  %-12s  usage fetch failed\n", a.Alias)
			continue
		}
		score := u.Score()
		if !u.IsMaxed() && score < bestScore {
			bestScore = score
			bestIdx = i
		}
		fhBar := claude.ProgressBar(u.FiveHourUtilization, 8)
		sdBar := claude.ProgressBar(u.SevenDayUtilization, 8)
		maxMark := ""
		if u.IsMaxed() {
			maxMark = " [maxed]"
		}
		fmt.Printf("  %-12s  5h: %s %3.0f%% ↺%-6s  7d: %s %3.0f%%%s\n",
			a.Alias,
			fhBar, u.FiveHourUtilization, claude.FormatResetIn(u.FiveHourResetsAt),
			sdBar, u.SevenDayUtilization,
			maxMark,
		)
	}

	if bestIdx == -1 {
		fmt.Println("\n⚠️  All accounts maxed or unreachable, falling back to first account")
		bestIdx = 0
	}

	chosen := accounts[bestIdx]
	fmt.Printf("\n→ switching to %q (%s)\n", chosen.Alias, chosen.Email)

	return switchAndLaunch(chosen, args)
}

// switchAndLaunch writes credentials and replaces the current process with claude.
func switchAndLaunch(a store.Account, args []string) error {
	// Warn clearly if the token is expired so the user knows to re-login rather
	// than silently launching Claude Code with invalid credentials.
	if a.Credentials.ExpiresAt != 0 && time.Now().After(time.UnixMilli(a.Credentials.ExpiresAt)) {
		fmt.Fprintf(os.Stderr, "warning: token for account %q (%s) has expired\n", a.Alias, a.Email)
		fmt.Fprintf(os.Stderr, "  To refresh: log in to Claude Code as %s, then run 'auto-switch login --alias %s'.\n", a.Email, a.Alias)
	}

	token := &claude.OAuthToken{
		AccessToken:  a.Credentials.AccessToken,
		RefreshToken: a.Credentials.RefreshToken,
		ExpiresAt:    a.Credentials.ExpiresAt,
		Scopes:       a.Credentials.Scopes,
	}

	if err := claude.WriteCredentials(token); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write credentials: %v\n", err)
	}

	_ = claude.WriteAccountInfo(&claude.OAuthAccount{
		AccountUUID:      a.AccountUUID,
		EmailAddress:     a.Email,
		OrganizationUUID: a.OrgUUID,
		OrganizationName: a.OrgName,
		DisplayName:      a.DisplayName,
	})

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude command not found: %v", err)
	}

	// Inject token via env var (highest priority)
	env := os.Environ()
	env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+a.Credentials.AccessToken)

	argv := append([]string{"claude"}, args...)
	return syscall.Exec(claudePath, argv, env)
}
