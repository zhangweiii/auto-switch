package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/store"
)

var loginAlias string

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Save the currently logged-in Claude Code account",
	Long: `Read the current Claude Code credentials and save them as a named account.

Workflow:
  1. Log in to your first account with the 'claude' command
  2. Run 'auto-switch login' to save that account
  3. Run /logout inside Claude Code, then log in with your next account
  4. Run 'auto-switch login' again to save the second account`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLogin(loginAlias)
	},
}

func init() {
	loginCmd.Flags().StringVarP(&loginAlias, "alias", "a", "", "account alias (e.g. personal, work)")
}

func runLogin(alias string) error {
	fmt.Println("Reading current Claude Code credentials...")

	cred, err := claude.ReadCurrentCredentials()
	if err != nil {
		return fmt.Errorf("cannot read credentials: %v\nPlease log in first by running 'claude'", err)
	}

	account, err := claude.ReadCurrentAccount()
	if err != nil {
		return fmt.Errorf("cannot read account info: %v", err)
	}

	fmt.Printf("Detected account: %s (%s)\n", account.EmailAddress, account.OrganizationName)

	// Token expiry warning
	days := cred.DaysUntilExpiry()
	if days < 0 {
		fmt.Println("⚠️  Token has expired, please log in to Claude Code again")
		return fmt.Errorf("token expired")
	} else if days < 30 {
		fmt.Printf("⚠️  Token expires in %d day(s), please re-login soon\n", days)
	}

	// Prompt for alias if not provided
	if alias == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter an alias for this account (e.g. personal, work): ")
		alias, _ = reader.ReadString('\n')
		alias = strings.TrimSpace(alias)
	}
	if alias == "" {
		return fmt.Errorf("alias cannot be empty")
	}

	cfg, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	newAccount := store.Account{
		Alias:    alias,
		Email:    account.EmailAddress,
		Provider: "claude",
		Credentials: store.Credentials{
			AccessToken:  cred.AccessToken,
			RefreshToken: cred.RefreshToken,
			ExpiresAt:    cred.ExpiresAt,
		},
		OrgUUID:     account.OrganizationUUID,
		AccountUUID: account.AccountUUID,
		OrgName:     account.OrganizationName,
		DisplayName: account.DisplayName,
		CreatedAt:   time.Now(),
	}

	if err := cfg.AddAccount(newAccount); err != nil {
		return err
	}
	if err := store.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %v", err)
	}

	fmt.Printf("✓ Account %q (%s) saved\n", alias, account.EmailAddress)

	accounts := cfg.AccountsByProvider("claude")
	if len(accounts) > 1 {
		fmt.Printf("\n%d Claude accounts saved:\n", len(accounts))
		for _, a := range accounts {
			fmt.Printf("  %-15s %s\n", a.Alias, a.Email)
		}
		fmt.Println("\nRun 'auto-switch claude' to automatically switch to the least-used account")
	} else {
		fmt.Println("\nTip: run /logout in Claude Code → log in with next account → run auto-switch login again")
	}

	return nil
}
