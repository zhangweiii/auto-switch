package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhangweiii/auto-switch/internal/store"
)

type AuthFile struct {
	AuthMode     string      `json:"auth_mode"`
	OpenAIAPIKey *string     `json:"OPENAI_API_KEY"`
	Tokens       AuthTokens  `json:"tokens"`
	LastRefresh  string      `json:"last_refresh"`
	Extra        interface{} `json:"-"`
}

type AuthTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type AccountInfo struct {
	Email     string
	Subject   string
	AccountID string
	AuthMode  string
}

func BaseHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	userHome, _ := os.UserHomeDir()
	return filepath.Join(userHome, ".codex")
}

func SharedHome() string {
	userHome, _ := os.UserHomeDir()
	return filepath.Join(userHome, ".codex")
}

func authPath(home string) string {
	return filepath.Join(home, "auth.json")
}

func AccountHome(alias string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(alias)
	return filepath.Join(store.ConfigDir(), "codex", safe)
}

func ReadCurrentAuthRaw() ([]byte, error) {
	return os.ReadFile(authPath(BaseHome()))
}

func ReadCurrentAuth() (*AuthFile, []byte, error) {
	raw, err := ReadCurrentAuthRaw()
	if err != nil {
		return nil, nil, err
	}
	var auth AuthFile
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, nil, err
	}
	return &auth, raw, nil
}

func ReadCurrentAccount() (*AccountInfo, error) {
	auth, _, err := ReadCurrentAuth()
	if err != nil {
		return nil, err
	}
	info := &AccountInfo{
		AccountID: auth.Tokens.AccountID,
		AuthMode:  auth.AuthMode,
	}
	if auth.AuthMode == "chatgpt" {
		email, sub, err := decodeIDToken(auth.Tokens.IDToken)
		if err != nil {
			return nil, err
		}
		info.Email = email
		info.Subject = sub
		return info, nil
	}
	if auth.OpenAIAPIKey != nil && *auth.OpenAIAPIKey != "" {
		key := *auth.OpenAIAPIKey
		suffix := key
		if len(key) > 4 {
			suffix = key[len(key)-4:]
		}
		info.Email = "api-key-" + suffix
		return info, nil
	}
	return nil, fmt.Errorf("unsupported Codex auth mode: %q", auth.AuthMode)
}

func ActiveAccountID() string {
	auth, _, err := ReadCurrentAuth()
	if err != nil {
		return ""
	}
	return auth.Tokens.AccountID
}

func EnsureAccountHome(alias string, rawAuth []byte) (string, error) {
	home := AccountHome(alias)
	if err := os.MkdirAll(home, 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(authPath(home), rawAuth, 0600); err != nil {
		return "", err
	}

	shared := SharedHome()
	for _, name := range []string{"config.toml", "version.json", "models_cache.json", "skills", "vendor_imports", "memories"} {
		src := filepath.Join(shared, name)
		dst := filepath.Join(home, name)

		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		_ = os.Symlink(src, dst)
	}

	return home, nil
}

func decodeIDToken(token string) (email, sub string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid Codex id_token")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", err
	}

	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", err
	}
	if claims.Email == "" {
		return "", "", fmt.Errorf("email not found in Codex id_token")
	}
	return claims.Email, claims.Sub, nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
