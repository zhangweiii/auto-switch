package claude

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type cacheEntry struct {
	Usage    Usage     `json:"usage"`
	CachedAt time.Time `json:"cached_at"`
}

// successCacheTTL is how long a successful usage response is cached.
// Errors are never cached so the next call retries immediately.
const successCacheTTL = 5 * time.Minute

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "auto-switch")
}

func cachePath() string {
	return filepath.Join(cacheDir(), "usage-cache.json")
}

func loadCache() map[string]cacheEntry {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return map[string]cacheEntry{}
	}
	var m map[string]cacheEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]cacheEntry{}
	}
	return m
}

func saveCache(m map[string]cacheEntry) {
	_ = os.MkdirAll(cacheDir(), 0700)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(), data, 0600)
}

// Usage holds the current utilisation for one account.
type Usage struct {
	FiveHourUtilization float64
	FiveHourResetsAt    time.Time
	SevenDayUtilization float64
	SevenDayResetsAt    time.Time
	FetchedAt           time.Time
	Cached              bool   // true when returned from local cache
	Error               string // non-empty when fetch failed
}

// Score returns a weighted score used for account selection (lower = preferred).
func (u *Usage) Score() float64 {
	if u.Error != "" {
		return 0 // unknown usage: still usable
	}
	return u.FiveHourUtilization*0.7 + u.SevenDayUtilization*0.3
}

// IsMaxed returns true when the 5-hour window is at or near the limit.
func (u *Usage) IsMaxed() bool {
	return u.FiveHourUtilization >= 95 && u.Error == ""
}

// CacheAge returns a human-friendly description of how old the cached data is.
func (u *Usage) CacheAge() string {
	if !u.Cached || u.FetchedAt.IsZero() {
		return ""
	}
	d := time.Since(u.FetchedAt)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// FetchUsageWithCache returns cached usage if still fresh, otherwise fetches live.
// Only successful responses are cached; errors are not cached.
func FetchUsageWithCache(accessToken, cacheKey string) *Usage {
	cache := loadCache()
	if entry, ok := cache[cacheKey]; ok {
		if time.Since(entry.CachedAt) < successCacheTTL {
			u := entry.Usage
			u.Cached = true
			return &u
		}
	}

	u := FetchUsage(accessToken)

	if u.Error == "" {
		cache[cacheKey] = cacheEntry{Usage: *u, CachedAt: time.Now()}
		saveCache(cache)
	}
	return u
}

// FetchUsage queries the Anthropic OAuth usage endpoint directly.
func FetchUsage(accessToken string) *Usage {
	u := &Usage{FetchedAt: time.Now()}

	client := &http.Client{Timeout: 10 * time.Second}

	doRequest := func() (*http.Response, error) {
		req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		return client.Do(req)
	}

	resp, err := doRequest()
	if err != nil {
		u.Error = err.Error()
		return u
	}

	// Retry once on 429 (retry-after: 0 means immediately)
	if resp.StatusCode == 429 {
		resp.Body.Close()
		time.Sleep(1500 * time.Millisecond)
		resp, err = doRequest()
		if err != nil {
			u.Error = err.Error()
			return u
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		u.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return u
	}

	var raw struct {
		FiveHour *struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		u.Error = err.Error()
		return u
	}

	if raw.FiveHour != nil {
		u.FiveHourUtilization = raw.FiveHour.Utilization
		u.FiveHourResetsAt, _ = time.Parse(time.RFC3339Nano, raw.FiveHour.ResetsAt)
	}
	if raw.SevenDay != nil {
		u.SevenDayUtilization = raw.SevenDay.Utilization
		u.SevenDayResetsAt, _ = time.Parse(time.RFC3339Nano, raw.SevenDay.ResetsAt)
	}

	return u
}

// FormatResetIn returns a human-friendly "resets in X" string.
func FormatResetIn(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Until(t)
	if d <= 0 {
		return "reset"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// ProgressBar returns a fixed-width ASCII progress bar.
func ProgressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return bar
}
