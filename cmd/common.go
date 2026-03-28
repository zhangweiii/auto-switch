package cmd

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/codex"
	"github.com/zhangweiii/auto-switch/internal/store"
)

// loadAndSync loads config and auto-syncs the active account's token from Keychain.
// It syncs based on email matching - if Claude is logged in as account A,
// we update auto-switch's stored credentials for account A.
func loadAndSync() (*store.Config, error) {
	cfg, err := store.Load()
	if err != nil {
		return nil, err
	}

	updated := false

	// Sync Claude: read current claude credentials and sync to auto-switch by email
	if synced := syncClaudeToStore(cfg); synced {
		updated = true
	}

	// Sync from each Claude account's isolated home directory
	if synced := syncClaudeAccountHomes(cfg); synced {
		updated = true
	}

	// Sync Codex: read current codex credentials and sync to auto-switch by email/accountID
	if synced := syncCodexToStore(cfg); synced {
		updated = true
	}

	// Sync from each Codex account's isolated home directory
	if synced := syncCodexAccountHomes(cfg); synced {
		updated = true
	}

	if updated {
		_ = store.Save(cfg)
	}

	return cfg, nil
}

// syncClaudeToStore reads Claude's current credentials and updates auto-switch
// storage if the same email account exists and has newer credentials.
func syncClaudeToStore(cfg *store.Config) bool {
	activeEmail := claude.ActiveEmail()
	if activeEmail == "" {
		return false
	}

	// Find account by email in auto-switch storage
	account := cfg.FindByEmail(activeEmail, "claude")
	if account == nil {
		return false
	}

	// Read current Claude credentials
	currentToken, err := claude.ReadCurrentCredentials()
	if err != nil {
		return false
	}

	// Check if Claude's credentials are different/newer
	if account.Credentials.AccessToken == currentToken.AccessToken &&
		account.Credentials.RefreshToken == currentToken.RefreshToken &&
		account.Credentials.ExpiresAt == currentToken.ExpiresAt {
		return false
	}

	// Update auto-switch storage with Claude's current credentials
	account.Credentials.AccessToken = currentToken.AccessToken
	account.Credentials.RefreshToken = currentToken.RefreshToken
	account.Credentials.ExpiresAt = currentToken.ExpiresAt
	account.Credentials.UpdatedAt = time.Now()
	if len(currentToken.Scopes) > 0 {
		account.Credentials.Scopes = currentToken.Scopes
	}
	if currentToken.SubscriptionType != "" {
		account.Credentials.SubscriptionType = currentToken.SubscriptionType
	}
	if currentToken.RateLimitTier != "" {
		account.Credentials.RateLimitTier = currentToken.RateLimitTier
	}

	fmt.Printf("synced Claude credentials for %q from active session\n", account.Alias)
	return true
}

// syncCodexToStore reads Codex's current credentials and updates auto-switch
// storage if the same account (by email or accountID) exists and has newer credentials.
func syncCodexToStore(cfg *store.Config) bool {
	auth, rawAuth, err := codex.ReadCurrentAuth()
	if err != nil {
		return false
	}

	accountInfo, err := codex.ReadAccountFromAuth(auth)
	if err != nil {
		// Fallback: use AccountID only
		accountInfo = &codex.AccountInfo{
			AccountID: auth.Tokens.AccountID,
			AuthMode:  auth.AuthMode,
		}
	}

	// Try to find account by email first, then by accountID
	var account *store.Account
	if accountInfo.Email != "" {
		account = cfg.FindByEmail(accountInfo.Email, "codex")
	}
	if account == nil && accountInfo.AccountID != "" {
		account = cfg.FindByAccountID(accountInfo.AccountID, "codex")
	}
	if account == nil {
		return false
	}

	// Check if credentials changed
	if account.Credentials.AccessToken == auth.Tokens.AccessToken &&
		account.Credentials.RefreshToken == auth.Tokens.RefreshToken &&
		account.Credentials.IDToken == auth.Tokens.IDToken &&
		account.RawAuth == string(rawAuth) {
		return false
	}

	// Update credentials
	account.Credentials.AccessToken = auth.Tokens.AccessToken
	account.Credentials.RefreshToken = auth.Tokens.RefreshToken
	account.Credentials.IDToken = auth.Tokens.IDToken
	account.Credentials.AccountID = auth.Tokens.AccountID
	account.Credentials.AuthMode = auth.AuthMode
	account.Credentials.UpdatedAt = time.Now()
	account.RawAuth = string(rawAuth)

	// Update account info if available
	if accountInfo.Email != "" {
		account.Email = accountInfo.Email
		account.DisplayName = accountInfo.Email
	}
	if accountInfo.AccountID != "" {
		account.AccountUUID = accountInfo.AccountID
	}

	// Ensure isolated account home
	_, _ = codex.EnsureAccountHome(account.Alias, rawAuth)

	fmt.Printf("synced Codex credentials for %q from active session\n", account.Alias)
	return true
}

// syncClaudeAccountHomes syncs credentials from each Claude account's isolated home directory.
// This picks up tokens refreshed by Claude Code during a session.
func syncClaudeAccountHomes(cfg *store.Config) bool {
	updated := false
	for i := range cfg.Accounts {
		a := cfg.Accounts[i]
		if a.Provider != "claude" {
			continue
		}
		home := claude.AccountHome(a.Alias)
		token, err := claude.ReadCredentialsFromHome(home)
		if err != nil {
			continue
		}
		if cfg.Accounts[i].Credentials.AccessToken == token.AccessToken &&
			cfg.Accounts[i].Credentials.RefreshToken == token.RefreshToken &&
			cfg.Accounts[i].Credentials.ExpiresAt == token.ExpiresAt {
			continue
		}
		cfg.Accounts[i].Credentials.AccessToken = token.AccessToken
		cfg.Accounts[i].Credentials.RefreshToken = token.RefreshToken
		cfg.Accounts[i].Credentials.ExpiresAt = token.ExpiresAt
		cfg.Accounts[i].Credentials.UpdatedAt = time.Now()
		if token.SubscriptionType != "" {
			cfg.Accounts[i].Credentials.SubscriptionType = token.SubscriptionType
		}
		if token.RateLimitTier != "" {
			cfg.Accounts[i].Credentials.RateLimitTier = token.RateLimitTier
		}
		fmt.Printf("auto-synced Claude credentials for %q from isolated home\n", a.Alias)
		updated = true
	}
	return updated
}

// syncCodexAccountHomes syncs credentials from each account's isolated home directory.
func syncCodexAccountHomes(cfg *store.Config) bool {
	updated := false

	for i := range cfg.Accounts {
		a := cfg.Accounts[i]
		if a.Provider != "codex" {
			continue
		}

		auth, rawAuth, err := codex.ReadAuth(codex.AccountHome(a.Alias))
		if err != nil {
			continue
		}

		accountInfo, err := codex.ReadAccountFromAuth(auth)
		if err != nil {
			accountInfo = &codex.AccountInfo{
				AccountID: auth.Tokens.AccountID,
				AuthMode:  auth.AuthMode,
			}
		}

		if syncCodexAccountRecord(&cfg.Accounts[i], auth, rawAuth, accountInfo) {
			fmt.Printf("auto-synced Codex auth for %q\n", a.Alias)
			updated = true
		}
	}

	return updated
}

func syncCodexAccountRecord(a *store.Account, auth *codex.AuthFile, rawAuth []byte, account *codex.AccountInfo) bool {
	updated := false

	if a.Credentials.AccessToken != auth.Tokens.AccessToken {
		a.Credentials.AccessToken = auth.Tokens.AccessToken
		updated = true
	}
	if a.Credentials.RefreshToken != auth.Tokens.RefreshToken {
		a.Credentials.RefreshToken = auth.Tokens.RefreshToken
		updated = true
	}
	if a.Credentials.IDToken != auth.Tokens.IDToken {
		a.Credentials.IDToken = auth.Tokens.IDToken
		updated = true
	}
	if a.Credentials.AccountID != auth.Tokens.AccountID {
		a.Credentials.AccountID = auth.Tokens.AccountID
		updated = true
	}
	if a.Credentials.AuthMode != auth.AuthMode {
		a.Credentials.AuthMode = auth.AuthMode
		updated = true
	}
	if a.RawAuth != string(rawAuth) {
		a.RawAuth = string(rawAuth)
		updated = true
	}
	if account != nil {
		if account.Email != "" && a.Email != account.Email {
			a.Email = account.Email
			updated = true
		}
		if account.AccountID != "" && a.AccountUUID != account.AccountID {
			a.AccountUUID = account.AccountID
			updated = true
		}
		if account.Email != "" && a.DisplayName != account.Email {
			a.DisplayName = account.Email
			updated = true
		}
	}

	if updated {
		a.Credentials.UpdatedAt = time.Now()
	}

	return updated
}

// refreshClaudeCredentials refreshes all inactive saved Claude accounts in-place.
// The currently active account is synced from Claude's own storage instead of
// being refreshed here, which avoids rotating the refresh token out from under
// a live Claude Code session.
func refreshClaudeCredentials(cfg *store.Config) error {
	type refreshTask struct {
		index   int
		account store.Account
	}

	activeEmail := claude.ActiveEmail()
	var tasks []refreshTask
	for i, a := range cfg.Accounts {
		if a.Provider != "claude" {
			continue
		}
		if a.Credentials.RefreshToken == "" {
			continue
		}
		if a.Email != "" && a.Email == activeEmail {
			continue
		}
		tasks = append(tasks, refreshTask{index: i, account: a})
	}

	if len(tasks) == 0 {
		return nil
	}

	var mu sync.Mutex
	updated := false
	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Add(1)
		go func(t refreshTask) {
			defer wg.Done()

			refreshed, err := claude.RefreshCredentials(&claude.OAuthToken{
				AccessToken:      t.account.Credentials.AccessToken,
				RefreshToken:     t.account.Credentials.RefreshToken,
				ExpiresAt:        t.account.Credentials.ExpiresAt,
				Scopes:           t.account.Credentials.Scopes,
				SubscriptionType: t.account.Credentials.SubscriptionType,
				RateLimitTier:    t.account.Credentials.RateLimitTier,
			})
			if err != nil {
				fmt.Printf("warning: refresh credentials for %q failed: %v\n", t.account.Alias, err)
				return
			}

			if refreshed.AccessToken == t.account.Credentials.AccessToken &&
				refreshed.RefreshToken == t.account.Credentials.RefreshToken &&
				refreshed.ExpiresAt == t.account.Credentials.ExpiresAt &&
				slices.Equal(refreshed.Scopes, t.account.Credentials.Scopes) {
				return
			}

			mu.Lock()
			cfg.Accounts[t.index].Credentials.AccessToken = refreshed.AccessToken
			cfg.Accounts[t.index].Credentials.RefreshToken = refreshed.RefreshToken
			cfg.Accounts[t.index].Credentials.ExpiresAt = refreshed.ExpiresAt
			cfg.Accounts[t.index].Credentials.Scopes = refreshed.Scopes
			cfg.Accounts[t.index].Credentials.UpdatedAt = time.Now()
			updated = true
			mu.Unlock()

			// Keep isolated home in sync with refreshed credentials
			home := claude.AccountHome(t.account.Alias)
			if _, statErr := os.Stat(home); statErr == nil {
				_ = claude.WriteCredentialsToHome(home, refreshed)
			}
		}(task)
	}

	wg.Wait()

	if updated {
		if err := store.Save(cfg); err != nil {
			return fmt.Errorf("save refreshed Claude credentials: %w", err)
		}
	}
	return nil
}

func fetchClaudeUsages(accounts []store.Account) []*claude.Usage {
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
	return usages
}

func fetchCodexUsages(accounts []store.Account) []*codex.Usage {
	usages := make([]*codex.Usage, len(accounts))

	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func(idx int, a store.Account) {
			defer wg.Done()
			usages[idx] = codex.FetchUsageWithCache(codex.AccountHome(a.Alias), a.Alias)
		}(i, a)
	}
	wg.Wait()
	return usages
}
