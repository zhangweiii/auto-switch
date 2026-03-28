package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhangweiii/auto-switch/internal/store"
)

// AccountHome returns the isolated home directory path for a Claude account alias.
// Each account gets its own home under ~/.config/auto-switch/claude/<alias>/.
func AccountHome(alias string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(alias)
	return filepath.Join(store.ConfigDir(), "claude", safe)
}

// EnsureAccountHome creates (or updates) the isolated home directory for a Claude account.
// It writes account-specific credentials and account info, and symlinks shared config files
// from the real home so tools like git, npm, etc. continue to work.
func EnsureAccountHome(alias string, token *OAuthToken, account *OAuthAccount) (string, error) {
	home := AccountHome(alias)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		return "", err
	}

	// Write account-specific credentials
	if err := writeCredentialsToDir(claudeDir, token); err != nil {
		return "", err
	}

	// Write account-specific claude.json
	if err := writeAccountJSONToHome(home, account); err != nil {
		return "", err
	}

	realHome, _ := os.UserHomeDir()

	// Symlink shared .claude subdirectories and files
	realClaudeDir := filepath.Join(realHome, ".claude")
	for _, name := range []string{
		"settings.json", "settings.local.json",
		"projects", "statsig", "ide", "todos",
	} {
		symlinkIfExists(filepath.Join(realClaudeDir, name), filepath.Join(claudeDir, name))
	}

	// Symlink home-level dotfiles needed by tools spawned by Claude Code
	for _, name := range []string{
		".gitconfig", ".ssh", ".config",
		".zshrc", ".zprofile", ".bashrc", ".bash_profile", ".profile",
		".npmrc",
	} {
		symlinkIfExists(filepath.Join(realHome, name), filepath.Join(home, name))
	}

	return home, nil
}

// ReadCredentialsFromHome reads the credentials from an isolated account home directory.
func ReadCredentialsFromHome(home string) (*OAuthToken, error) {
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

// WriteCredentialsToHome writes updated credentials into an existing isolated home.
// Used to keep the isolated home in sync after a token refresh.
func WriteCredentialsToHome(home string, token *OAuthToken) error {
	return writeCredentialsToDir(filepath.Join(home, ".claude"), token)
}

// writeCredentialsToDir writes credentials JSON into the given .claude directory.
func writeCredentialsToDir(claudeDir string, token *OAuthToken) error {
	cf := credentialsFile{ClaudeAiOauth: *token}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(claudeDir, ".credentials.json"), data, 0600)
}

// writeAccountJSONToHome writes the oauthAccount info into the isolated home's .claude.json.
func writeAccountJSONToHome(home string, account *OAuthAccount) error {
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

// symlinkIfExists creates a symlink dst -> src if src exists and dst doesn't.
func symlinkIfExists(src, dst string) {
	if _, err := os.Stat(src); err != nil {
		return
	}
	if _, err := os.Lstat(dst); err == nil {
		return // already exists (file or symlink)
	}
	_ = os.Symlink(src, dst)
}
