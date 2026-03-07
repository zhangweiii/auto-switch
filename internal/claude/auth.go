package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// credentialsFile mirrors the structure of ~/.claude/.credentials.json.
type credentialsFile struct {
	ClaudeAiOauth OAuthToken `json:"claudeAiOauth"`
}

// OAuthToken holds the raw Claude OAuth token data.
type OAuthToken struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken"`
	ExpiresAt        int64    `json:"expiresAt"` // Unix milliseconds
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// ExpiresAtTime converts the millisecond timestamp to time.Time.
func (t *OAuthToken) ExpiresAtTime() time.Time {
	if t.ExpiresAt == 0 {
		return time.Time{}
	}
	return time.Unix(t.ExpiresAt/1000, 0)
}

// DaysUntilExpiry returns the number of days until the token expires.
// A negative value means the token has already expired.
func (t *OAuthToken) DaysUntilExpiry() int {
	exp := t.ExpiresAtTime()
	if exp.IsZero() {
		return 999
	}
	return int(time.Until(exp).Hours() / 24)
}

// OAuthAccount mirrors the oauthAccount field in ~/.claude.json.
type OAuthAccount struct {
	AccountUUID      string `json:"accountUuid"`
	EmailAddress     string `json:"emailAddress"`
	OrganizationUUID string `json:"organizationUuid"`
	OrganizationName string `json:"organizationName"`
	BillingType      string `json:"billingType,omitempty"`
	DisplayName      string `json:"displayName"`
}

const keychainService = "Claude Code-credentials"

// ReadCurrentCredentials reads the active Claude Code OAuth token.
// It tries the macOS Keychain first, then falls back to ~/.claude/.credentials.json.
func ReadCurrentCredentials() (*OAuthToken, error) {
	token, err := readFromKeychain()
	if err == nil {
		return token, nil
	}
	return readFromFile()
}

func readFromKeychain() (*OAuthToken, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-w").Output()
	if err != nil {
		return nil, err
	}
	var cf credentialsFile
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &cf); err != nil {
		return nil, err
	}
	return &cf.ClaudeAiOauth, nil
}

func readFromFile() (*OAuthToken, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, err
	}
	return &cf.ClaudeAiOauth, nil
}

// ReadCurrentAccount reads account metadata from ~/.claude.json.
func ReadCurrentAccount() (*OAuthAccount, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	oaRaw, ok := raw["oauthAccount"]
	if !ok {
		return nil, fmt.Errorf("oauthAccount not found in ~/.claude.json")
	}
	var account OAuthAccount
	if err := json.Unmarshal(oaRaw, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// ActiveEmail returns the email address of the currently active Claude Code account.
func ActiveEmail() string {
	account, err := ReadCurrentAccount()
	if err != nil {
		return ""
	}
	return account.EmailAddress
}

// WriteCredentials persists the token to both the macOS Keychain and the
// ~/.claude/.credentials.json fallback file.
func WriteCredentials(token *OAuthToken) error {
	cf := credentialsFile{ClaudeAiOauth: *token}
	data, err := json.Marshal(cf)
	if err != nil {
		return err
	}

	keychainErr := writeToKeychain(string(data))
	fileErr := writeCredentialsFile(data)

	if keychainErr != nil && fileErr != nil {
		return fmt.Errorf("keychain: %v; file: %v", keychainErr, fileErr)
	}
	return nil
}

func writeToKeychain(jsonStr string) error {
	username := currentUsername()
	cmd := exec.Command("security", "add-generic-password",
		"-U", "-s", keychainService, "-a", username, "-w", jsonStr)
	return cmd.Run()
}

func writeCredentialsFile(data []byte) error {
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	_ = os.MkdirAll(claudeDir, 0700)
	return os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), data, 0600)
}

// WriteAccountInfo updates the oauthAccount field in ~/.claude.json,
// preserving all other existing fields.
func WriteAccountInfo(account *OAuthAccount) error {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")

	existing := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	existing["oauthAccount"] = account

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func currentUsername() string {
	out, err := exec.Command("id", "-un").Output()
	if err != nil {
		return "user"
	}
	return strings.TrimSpace(string(out))
}
