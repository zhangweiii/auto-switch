package store

import (
	"fmt"
)

// SyncActiveToken checks whether the currently active Claude Code account matches
// any stored account and, if so, updates the stored credentials with the latest
// token from Keychain / credentials file.
//
// This handles the case where Claude Code silently refreshes the OAuth token.
// Returns true if any account was updated.
func SyncActiveToken(cfg *Config, readCurrentToken func() (accessToken, refreshToken string, expiresAt int64, err error), activeEmail string) (bool, error) {
	if activeEmail == "" {
		return false, nil
	}

	accessToken, refreshToken, expiresAt, err := readCurrentToken()
	if err != nil {
		return false, err
	}

	updated := false
	for i, a := range cfg.Accounts {
		if a.Provider == "claude" && a.Email == activeEmail {
			if cfg.Accounts[i].Credentials.AccessToken != accessToken {
				cfg.Accounts[i].Credentials.AccessToken = accessToken
				cfg.Accounts[i].Credentials.RefreshToken = refreshToken
				cfg.Accounts[i].Credentials.ExpiresAt = expiresAt
				updated = true
				fmt.Printf("auto-synced token for %q\n", a.Alias)
			}
			break
		}
	}

	return updated, nil
}
