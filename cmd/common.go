package cmd

import (
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
