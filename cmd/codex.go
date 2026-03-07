package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/codex"
	"github.com/zhangweiii/auto-switch/internal/store"
)

var codexCmd = &cobra.Command{
	Use:                "codex [args...]",
	Short:              "Switch to the least-used Codex account and launch codex",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCodex(accountAlias, args)
	},
}

func runCodex(accountAlias string, args []string) error {
	cfg, err := loadAndSync()
	if err != nil {
		return err
	}

	accounts := cfg.AccountsByProvider("codex")
	if len(accounts) == 0 {
		return fmt.Errorf("no Codex accounts found, run 'auto-switch login --provider codex' first")
	}

	if accountAlias != "" {
		a := cfg.FindByAlias(accountAlias, "codex")
		if a == nil {
			return fmt.Errorf("account %q not found, run 'auto-switch list --provider codex' to see saved accounts", accountAlias)
		}
		fmt.Printf("→ using specified account: %s (%s)\n", a.Alias, a.Email)
		return switchAndLaunchCodex(*a, args)
	}

	if len(accounts) == 1 {
		return switchAndLaunchCodex(accounts[0], args)
	}

	fmt.Printf("Checking usage for %d accounts...\n", len(accounts))
	usages := fetchCodexUsages(accounts)

	fmt.Println()
	bestIdx := -1
	bestScore := 999.0

	for i, a := range accounts {
		u := usages[i]
		if u.Error != "" {
			fmt.Printf("  %-12s  usage unavailable\n", a.Alias)
			continue
		}
		score := u.Score()
		if !u.IsMaxed() && score < bestScore {
			bestScore = score
			bestIdx = i
		}
		pBar := codex.ProgressBar(u.PrimaryUtilization, 8)
		sBar := codex.ProgressBar(u.SecondaryUtilization, 8)
		maxMark := ""
		if u.IsMaxed() {
			maxMark = " [maxed]"
		}
		plan := ""
		if u.PlanType != "" {
			plan = " " + u.PlanType
		}
		fmt.Printf("  %-12s  5h: %s %3.0f%% ↺%-6s  7d: %s %3.0f%%%s%s\n",
			a.Alias,
			pBar, u.PrimaryUtilization, codex.FormatResetIn(u.PrimaryResetsAt),
			sBar, u.SecondaryUtilization,
			maxMark, plan,
		)
	}

	if bestIdx == -1 {
		fmt.Println("\n⚠️  All accounts are maxed or missing recent usage data, falling back to first account")
		bestIdx = 0
	}

	chosen := accounts[bestIdx]
	fmt.Printf("\n→ switching to %q (%s)\n", chosen.Alias, chosen.Email)

	return switchAndLaunchCodex(chosen, args)
}

func switchAndLaunchCodex(a store.Account, args []string) error {
	if a.RawAuth == "" {
		return fmt.Errorf("account %q is missing saved Codex auth, re-run 'auto-switch login --provider codex --alias %s'", a.Alias, a.Alias)
	}
	home, err := codex.EnsureAccountHome(a.Alias, []byte(a.RawAuth))
	if err != nil {
		return fmt.Errorf("failed to prepare Codex home: %v", err)
	}

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex command not found: %v", err)
	}

	env := filteredEnv("CODEX_HOME", "OPENAI_API_KEY")
	env = append(env, "CODEX_HOME="+home)

	argv := append([]string{"codex"}, args...)
	return syscall.Exec(codexPath, argv, env)
}

func filteredEnv(dropKeys ...string) []string {
	drop := map[string]struct{}{}
	for _, key := range dropKeys {
		drop[key] = struct{}{}
	}

	var env []string
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if _, ok := drop[parts[0]]; ok {
			continue
		}
		env = append(env, item)
	}
	return env
}
