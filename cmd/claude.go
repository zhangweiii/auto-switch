package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

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
	if err := refreshClaudeCredentials(cfg); err != nil {
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
	usages := fetchClaudeUsages(accounts)

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

// switchAndLaunch prepares an isolated home for the account and replaces the
// current process with claude, pointing HOME at the isolated directory so that
// credentials and account info are per-account and terminals don't overwrite
// each other's global state.
func switchAndLaunch(a store.Account, args []string) error {
	token := &claude.OAuthToken{
		AccessToken:      a.Credentials.AccessToken,
		RefreshToken:     a.Credentials.RefreshToken,
		ExpiresAt:        a.Credentials.ExpiresAt,
		Scopes:           a.Credentials.Scopes,
		SubscriptionType: a.Credentials.SubscriptionType,
		RateLimitTier:    a.Credentials.RateLimitTier,
	}
	accountInfo := &claude.OAuthAccount{
		AccountUUID:      a.AccountUUID,
		EmailAddress:     a.Email,
		OrganizationUUID: a.OrgUUID,
		OrganizationName: a.OrgName,
		DisplayName:      a.DisplayName,
	}

	isolatedHome, err := claude.EnsureAccountHome(a.Alias, token, accountInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to prepare isolated home: %v\n", err)
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude command not found: %v", err)
	}

	env := removeEnv(os.Environ(), "HOME", "CLAUDE_CODE_OAUTH_TOKEN")
	env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+a.Credentials.AccessToken)
	if isolatedHome != "" {
		env = append(env, "HOME="+isolatedHome)
	}

	argv := append([]string{"claude"}, args...)
	return syscall.Exec(claudePath, argv, env)
}

// removeEnv returns a copy of env with entries for the given keys removed.
func removeEnv(env []string, keys ...string) []string {
	result := make([]string, 0, len(env))
	for _, e := range env {
		keep := true
		for _, key := range keys {
			if strings.HasPrefix(e, key+"=") {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, e)
		}
	}
	return result
}
