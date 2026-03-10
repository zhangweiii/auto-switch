package cmd

import (
	"fmt"

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

	updated, err := store.SyncActiveToken(cfg, readToken, activeEmail)
	if err == nil && updated {
		_ = store.Save(cfg)
	}

	if auth, rawAuth, err := codex.ReadCurrentAuth(); err == nil {
		for i, a := range cfg.Accounts {
			if a.Provider != "codex" {
				continue
			}
			if a.Credentials.AccountID != "" && a.Credentials.AccountID != auth.Tokens.AccountID {
				continue
			}
			if a.Email != "" {
				account, err := codex.ReadCurrentAccount()
				if err == nil && account.Email != "" && a.Email != account.Email {
					continue
				}
			}
			if cfg.Accounts[i].Credentials.AccessToken != auth.Tokens.AccessToken || cfg.Accounts[i].RawAuth != string(rawAuth) {
				cfg.Accounts[i].Credentials.AccessToken = auth.Tokens.AccessToken
				cfg.Accounts[i].Credentials.RefreshToken = auth.Tokens.RefreshToken
				cfg.Accounts[i].Credentials.IDToken = auth.Tokens.IDToken
				cfg.Accounts[i].Credentials.AccountID = auth.Tokens.AccountID
				cfg.Accounts[i].Credentials.AuthMode = auth.AuthMode
				cfg.Accounts[i].RawAuth = string(rawAuth)
				if _, err := codex.EnsureAccountHome(cfg.Accounts[i].Alias, rawAuth); err == nil {
					_ = store.Save(cfg)
				}
			}
			break
		}
	}

	return cfg, nil
}

// refreshClaudeCredentials refreshes all saved Claude account credentials in-place.
func refreshClaudeCredentials(cfg *store.Config) error {
	updated := false

	for i, a := range cfg.Accounts {
		if a.Provider != "claude" {
			continue
		}
		if a.Credentials.RefreshToken == "" {
			continue
		}

		refreshed, err := claude.RefreshCredentials(&claude.OAuthToken{
			AccessToken:  a.Credentials.AccessToken,
			RefreshToken: a.Credentials.RefreshToken,
			ExpiresAt:    a.Credentials.ExpiresAt,
			Scopes:       a.Credentials.Scopes,
		})
		if err != nil {
			fmt.Printf("warning: refresh credentials for %q failed: %v\n", a.Alias, err)
			continue
		}

		if refreshed.AccessToken == a.Credentials.AccessToken &&
			refreshed.RefreshToken == a.Credentials.RefreshToken &&
			refreshed.ExpiresAt == a.Credentials.ExpiresAt {
			continue
		}

		cfg.Accounts[i].Credentials.AccessToken = refreshed.AccessToken
		cfg.Accounts[i].Credentials.RefreshToken = refreshed.RefreshToken
		cfg.Accounts[i].Credentials.ExpiresAt = refreshed.ExpiresAt
		cfg.Accounts[i].Credentials.Scopes = refreshed.Scopes
		updated = true
	}

	if updated {
		if err := store.Save(cfg); err != nil {
			return fmt.Errorf("save refreshed Claude credentials: %w", err)
		}
	}
	return nil
}
