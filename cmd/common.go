package cmd

import (
	"fmt"
	"slices"
	"sync"

	"github.com/zhangweiii/auto-switch/internal/claude"
	"github.com/zhangweiii/auto-switch/internal/codex"
	"github.com/zhangweiii/auto-switch/internal/store"
)

// loadAndSync loads config and auto-syncs the active account's token from Keychain.
func loadAndSync() (*store.Config, error) {
	cfg, err := store.Load()
	if err != nil {
		return nil, err
	}

	activeEmail := claude.ActiveEmail()
	readToken := func() (string, string, int64, error) {
		t, err := claude.ReadCurrentCredentials()
		if err != nil {
			return "", "", 0, err
		}
		return t.AccessToken, t.RefreshToken, t.ExpiresAt, nil
	}

	updated := false
	if synced, err := store.SyncActiveToken(cfg, readToken, activeEmail); err == nil && synced {
		updated = true
	}

	if syncActiveCodexAuth(cfg) {
		updated = true
	}
	if syncSavedCodexAuths(cfg) {
		updated = true
	}
	if updated {
		_ = store.Save(cfg)
	}

	return cfg, nil
}

func syncActiveCodexAuth(cfg *store.Config) bool {
	auth, rawAuth, err := codex.ReadCurrentAuth()
	if err != nil {
		return false
	}

	account, err := codex.ReadAccountFromAuth(auth)
	if err != nil {
		account = &codex.AccountInfo{
			AccountID: auth.Tokens.AccountID,
			AuthMode:  auth.AuthMode,
		}
	}

	for i := range cfg.Accounts {
		a := cfg.Accounts[i]
		if a.Provider != "codex" {
			continue
		}
		if a.Credentials.AccountID != "" && a.Credentials.AccountID != auth.Tokens.AccountID {
			continue
		}
		if account.Email != "" && a.Email != "" && a.Email != account.Email {
			continue
		}

		updated := syncCodexAccountRecord(&cfg.Accounts[i], auth, rawAuth, account)
		_, _ = codex.EnsureAccountHome(cfg.Accounts[i].Alias, rawAuth)
		return updated
	}
	return false
}

func syncSavedCodexAuths(cfg *store.Config) bool {
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

		account, err := codex.ReadAccountFromAuth(auth)
		if err != nil {
			account = &codex.AccountInfo{
				AccountID: auth.Tokens.AccountID,
				AuthMode:  auth.AuthMode,
			}
		}
		if syncCodexAccountRecord(&cfg.Accounts[i], auth, rawAuth, account) {
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
				AccessToken:  t.account.Credentials.AccessToken,
				RefreshToken: t.account.Credentials.RefreshToken,
				ExpiresAt:    t.account.Credentials.ExpiresAt,
				Scopes:       t.account.Credentials.Scopes,
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
			updated = true
			mu.Unlock()
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
