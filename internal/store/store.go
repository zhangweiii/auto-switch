package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// DaysUntilExpiry returns days until token expiry. Negative means already expired.
func (c Credentials) DaysUntilExpiry() int {
	if c.ExpiresAt == 0 {
		return 999
	}
	exp := time.Unix(c.ExpiresAt/1000, 0)
	return int(time.Until(exp).Hours() / 24)
}

// FormatExpiry returns a human-readable expiry string.
// Shows days when >= 1 day, hours when < 1 day, "expired!" when past.
func (c Credentials) FormatExpiry() string {
	if c.ExpiresAt == 0 {
		return ""
	}
	exp := time.Unix(c.ExpiresAt/1000, 0)
	d := time.Until(exp)
	if d < 0 {
		return "expired!"
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 0 {
			return fmt.Sprintf("%dm", int(d.Minutes()))
		}
		return fmt.Sprintf("%dh", h)
	}
	days := int(d.Hours() / 24)
	if days < 30 {
		return fmt.Sprintf("%dd", days)
	}
	return ""
}

type Credentials struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresAt    int64    `json:"expires_at"` // unix ms
	IDToken      string   `json:"id_token,omitempty"`
	AccountID    string   `json:"account_id,omitempty"`
	AuthMode     string   `json:"auth_mode,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

type Account struct {
	ID          string      `json:"id"`
	Alias       string      `json:"alias"`
	Email       string      `json:"email"`
	Provider    string      `json:"provider"` // "claude" | "codex"
	Credentials Credentials `json:"credentials"`
	OrgUUID     string      `json:"org_uuid,omitempty"`
	AccountUUID string      `json:"account_uuid,omitempty"`
	OrgName     string      `json:"org_name,omitempty"`
	DisplayName string      `json:"display_name,omitempty"`
	RawAuth     string      `json:"raw_auth,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Config struct {
	Version  int       `json:"version"`
	Accounts []Account `json:"accounts"`
}

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "auto-switch")
}

func configPath() string {
	return filepath.Join(ConfigDir(), "accounts.json")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return &Config{Version: 1, Accounts: []Account{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(c *Config) error {
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func (c *Config) AddAccount(a Account) error {
	// check duplicate alias
	for _, existing := range c.Accounts {
		if existing.Alias == a.Alias && existing.Provider == a.Provider {
			return fmt.Errorf("alias %q already exists for provider %s", a.Alias, a.Provider)
		}
	}
	// check duplicate email
	for i, existing := range c.Accounts {
		if existing.Email == a.Email && existing.Provider == a.Provider {
			// update credentials of existing account
			c.Accounts[i].Credentials = a.Credentials
			c.Accounts[i].Alias = a.Alias
			c.Accounts[i].OrgUUID = a.OrgUUID
			c.Accounts[i].AccountUUID = a.AccountUUID
			c.Accounts[i].OrgName = a.OrgName
			c.Accounts[i].DisplayName = a.DisplayName
			c.Accounts[i].RawAuth = a.RawAuth
			return nil
		}
	}
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	c.Accounts = append(c.Accounts, a)
	return nil
}

func (c *Config) FindByAlias(alias, provider string) *Account {
	for i := range c.Accounts {
		if c.Accounts[i].Alias == alias && c.Accounts[i].Provider == provider {
			return &c.Accounts[i]
		}
	}
	return nil
}

func (c *Config) AccountsByProvider(provider string) []Account {
	var result []Account
	for _, a := range c.Accounts {
		if a.Provider == provider {
			result = append(result, a)
		}
	}
	return result
}

func (c *Config) RemoveByAlias(alias, provider string) bool {
	for i, a := range c.Accounts {
		if a.Alias == alias && a.Provider == provider {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return true
		}
	}
	return false
}
