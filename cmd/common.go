package cmd

import (
	"fmt"
	"time"

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

// tokenNeedsRefresh reports whether the stored token for the currently active
// Claude Code account should be proactively refreshed. The narrow 1-hour window
// avoids racing with Claude Code's own background refresh (refresh-token rotation
// means two concurrent refresh attempts will leave one with a 400).
func tokenNeedsRefresh(expiresAtMs int64) bool {
	if expiresAtMs == 0 {
		return false // unknown expiry – don't touch
	}
	return time.UnixMilli(expiresAtMs).Before(time.Now().Add(time.Hour))
}

// tokenNeedsRefreshInactive reports whether a background (non-active) account's
// token should be proactively refreshed. Inactive accounts are never touched by
// Claude Code's background rotation, so we can use a wider look-ahead window.
//
// When both issuedAt and expiresAt are known the window is computed dynamically
// as half the token's total lifetime — the token is refreshed once more than half
// of its validity period has elapsed. This self-calibrates to Claude's actual
// OAuth token lifetime without any hardcoded duration.
//
// For tokens whose issuedAt is not recorded (e.g. accounts saved before this
// field was added) we fall back to a 24-hour look-ahead as a safe default.
func tokenNeedsRefreshInactive(expiresAt, issuedAt int64) bool {
	if expiresAt == 0 {
		return false // unknown expiry – don't touch
	}
	now := time.Now()
	expiry := time.UnixMilli(expiresAt)
	if issuedAt != 0 {
		lifetime := expiry.Sub(time.UnixMilli(issuedAt))
		if lifetime > 0 {
			// Refresh when remaining time < half the total lifetime.
			return expiry.Sub(now) < lifetime/2
		}
		// lifetime <= 0 means issuedAt >= expiresAt (clock skew or corrupt data);
		// fall through to the 24-hour fallback below.
	}
	// Fallback for tokens without an IssuedAt: refresh within 24 hours of expiry.
	return expiry.Before(now.Add(24 * time.Hour))
}

// refreshClaudeCredentials refreshes all saved Claude account credentials in-place.
// activeEmail identifies the account currently active in Claude Code; its token is
// only refreshed when expiring within 1 hour to avoid racing with Claude Code's own
// background refresh. Tokens for all other (inactive) accounts are refreshed once
// more than half of their total validity period has elapsed, so the window
// self-calibrates to Claude's actual OAuth token lifetime.
func refreshClaudeCredentials(cfg *store.Config, activeEmail string) error {
	updated := false

	for i, a := range cfg.Accounts {
		if a.Provider != "claude" {
			continue
		}
		if a.Credentials.RefreshToken == "" {
			continue
		}
		// Active account: narrow window to avoid racing Claude Code's refresh.
		// Inactive accounts: wider window so rarely-used accounts stay fresh.
		// When activeEmail is empty we cannot determine which account is active,
		// so fall back to the narrow window for all accounts to stay safe.
		needsRefresh := false
		if activeEmail == "" || a.Email == activeEmail {
			needsRefresh = tokenNeedsRefresh(a.Credentials.ExpiresAt)
		} else {
			needsRefresh = tokenNeedsRefreshInactive(a.Credentials.ExpiresAt, a.Credentials.IssuedAt)
		}
		if !needsRefresh {
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
		cfg.Accounts[i].Credentials.IssuedAt = time.Now().UnixMilli()
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
