package codex

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/zhangweiii/auto-switch/internal/store"
)

type Usage struct {
	PrimaryUtilization   float64
	PrimaryResetsAt      time.Time
	SecondaryUtilization float64
	SecondaryResetsAt    time.Time
	PlanType             string
	FetchedAt            time.Time
	Cached               bool
	Error                string
}

type cacheEntry struct {
	Usage    Usage     `json:"usage"`
	CachedAt time.Time `json:"cached_at"`
}

const (
	successCacheTTL     = 1 * time.Minute
	fallbackCacheMaxAge = 24 * time.Hour
)

var ansiRegexp = regexp.MustCompile(`\x1b(?:\][^\x07]*(?:\x07|\x1b\\)|\[[0-9;?]*[ -/]*[@-~])`)

func (u *Usage) Score() float64 {
	if u.Error != "" {
		return 0
	}
	return u.PrimaryUtilization*0.7 + u.SecondaryUtilization*0.3
}

func (u *Usage) IsMaxed() bool {
	return u.PrimaryUtilization >= 95 && u.Error == ""
}

func (u *Usage) CacheAge() string {
	if u.FetchedAt.IsZero() {
		return ""
	}
	d := time.Since(u.FetchedAt)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func cachePath() string {
	return filepath.Join(store.ConfigDir(), "codex-usage-cache.json")
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
	if err := os.MkdirAll(store.ConfigDir(), 0700); err != nil {
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(), data, 0600)
}

func FetchUsageWithCache(home, cacheKey string) *Usage {
	cache := loadCache()
	if entry, ok := cache[cacheKey]; ok && time.Since(entry.CachedAt) < successCacheTTL {
		u := entry.Usage
		u.Cached = true
		return &u
	}

	u := FetchUsageFromHome(home)
	if u.Error == "" {
		cache[cacheKey] = cacheEntry{Usage: *u, CachedAt: time.Now()}
		saveCache(cache)
		return u
	}

	if entry, ok := cache[cacheKey]; ok && time.Since(entry.CachedAt) < fallbackCacheMaxAge {
		cached := entry.Usage
		cached.Cached = true
		return &cached
	}

	return u
}

func FetchUsageFromHome(home string) *Usage {
	if usage, err := fetchUsageViaStatus(home); err == nil {
		return usage
	}

	if usage, err := fetchUsageViaAppServer(home); err == nil {
		return usage
	}

	expectedPlan := expectedPlanType(home)

	files, err := sessionFiles(home)
	if err != nil {
		return &Usage{Error: err.Error()}
	}
	if stateFiles, err := stateSessionFiles(home); err == nil && len(stateFiles) > 0 {
		files = stateFiles
	}
	for _, file := range files {
		if usage := usageFromFile(file, expectedPlan); usage != nil {
			return usage
		}
	}

	if sharedHome := SharedHome(); sharedHome != "" && sharedHome != home {
		if files, err := sessionFiles(sharedHome); err == nil {
			for _, file := range files {
				if usage := usageFromFile(file, expectedPlan); usage != nil {
					return usage
				}
			}
		}
	}
	return &Usage{Error: "no Codex rate limit data found yet"}
}

func fetchUsageViaStatus(home string) (*Usage, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(codexPath, "-s", "read-only", "-a", "untrusted")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 60, Cols: 200})
	if err != nil {
		return nil, err
	}
	defer func() { _ = ptmx.Close() }()
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	var (
		buf bytes.Buffer
		mu  sync.Mutex
	)
	go func() {
		tmp := make([]byte, 8192)
		for {
			n, err := ptmx.Read(tmp)
			if n > 0 {
				chunk := tmp[:n]
				mu.Lock()
				_, _ = buf.Write(chunk)
				mu.Unlock()
				if bytes.Contains(chunk, []byte{0x1b, '[', '6', 'n'}) {
					_, _ = io.WriteString(ptmx, "\x1b[1;1R")
				}
			}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(8 * time.Second)
	time.Sleep(400 * time.Millisecond)

	sendStatus := func() error {
		_, err := io.WriteString(ptmx, "/status\n")
		return err
	}

	if err := sendStatus(); err != nil {
		return nil, err
	}
	lastEnter := time.Now()
	scriptSentAt := time.Now()
	enterRetries := 0
	statusRetries := 0

	for time.Now().Before(deadline) {
		text := cleanedOutput(&buf, &mu)
		if strings.Contains(text, "5h limit:") && strings.Contains(text, "Weekly limit:") {
			return parseStatusUsage(text)
		}
		if time.Since(lastEnter) >= 1200*time.Millisecond && enterRetries < 6 {
			_, _ = io.WriteString(ptmx, "\r")
			lastEnter = time.Now()
			enterRetries++
		}
		if time.Since(scriptSentAt) >= 3*time.Second && statusRetries < 2 {
			if err := sendStatus(); err == nil {
				scriptSentAt = time.Now()
				lastEnter = time.Now()
				statusRetries++
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = "timed out waiting for Codex /status output"
	}
	return nil, fmt.Errorf(msg)
}

func cleanedOutput(buf *bytes.Buffer, mu *sync.Mutex) string {
	mu.Lock()
	raw := buf.String()
	mu.Unlock()

	text := ansiRegexp.ReplaceAllString(raw, "")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case r < 32:
			return -1
		default:
			return r
		}
	}, text)
	return text
}

func parseStatusUsage(text string) (*Usage, error) {
	accountRe := regexp.MustCompile(`Account:\s+([^\s]+)\s+\(([^)]+)\)`)
	fiveHourRe := regexp.MustCompile(`5h limit:\s*\[[^\]]+\]\s*(\d+)% left\s*\(resets ([^)]+)\)`)
	weeklyRe := regexp.MustCompile(`Weekly limit:\s*\[[^\]]+\]\s*(\d+)% left\s*\(resets ([^)]+)\)`)

	accountMatch := accountRe.FindStringSubmatch(text)
	fiveHourMatch := fiveHourRe.FindStringSubmatch(text)
	weeklyMatch := weeklyRe.FindStringSubmatch(text)
	if len(fiveHourMatch) != 3 || len(weeklyMatch) != 3 {
		return nil, fmt.Errorf("Codex /status output missing limit details")
	}

	fiveLeft, err := strconv.Atoi(fiveHourMatch[1])
	if err != nil {
		return nil, err
	}
	weekLeft, err := strconv.Atoi(weeklyMatch[1])
	if err != nil {
		return nil, err
	}

	usage := &Usage{
		PrimaryUtilization:   float64(100 - fiveLeft),
		SecondaryUtilization: float64(100 - weekLeft),
		FetchedAt:            time.Now(),
	}
	if len(accountMatch) == 3 {
		usage.PlanType = strings.ToLower(strings.TrimSpace(accountMatch[2]))
	}

	if usage.PrimaryResetsAt, err = parseStatusReset(fiveHourMatch[2]); err != nil {
		return nil, err
	}
	if usage.SecondaryResetsAt, err = parseStatusReset(weeklyMatch[2]); err != nil {
		return nil, err
	}
	return usage, nil
}

func parseStatusReset(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	now := time.Now()
	loc := now.Location()

	if !strings.Contains(raw, " on ") {
		t, err := time.ParseInLocation("15:04", raw, loc)
		if err != nil {
			return time.Time{}, err
		}
		reset := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		if !reset.After(now) {
			reset = reset.Add(24 * time.Hour)
		}
		return reset, nil
	}

	parts := strings.SplitN(raw, " on ", 2)
	tm, err := time.ParseInLocation("15:04 2 Jan 2006", parts[0]+" "+parts[1]+" "+strconv.Itoa(now.Year()), loc)
	if err != nil {
		return time.Time{}, err
	}
	if !tm.After(now) {
		tm = tm.AddDate(1, 0, 0)
	}
	return tm, nil
}

func sessionFiles(home string) ([]string, error) {
	root := filepath.Join(home, "sessions")
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}

func stateSessionFiles(home string) ([]string, error) {
	sqlitePath, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, err
	}

	stateDB := filepath.Join(home, "state_5.sqlite")
	cmd := exec.Command(sqlitePath, stateDB, "select rollout_path from threads order by updated_at desc;")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("read state db: %s", msg)
	}

	var files []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	return files, nil
}

func usageFromFile(path, expectedPlan string) *Usage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var latest *Usage
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"rate_limits":`) {
			continue
		}
		usage := parseUsageLine(line)
		if usage != nil && matchesExpectedPlan(usage.PlanType, expectedPlan) {
			latest = usage
		}
	}
	return latest
}

func matchesExpectedPlan(actual, expected string) bool {
	if expected == "" || actual == "" {
		return true
	}
	return strings.EqualFold(actual, expected)
}

func expectedPlanType(home string) string {
	raw, err := os.ReadFile(authPath(home))
	if err != nil {
		return ""
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &auth); err != nil {
		return ""
	}
	return accessTokenPlanType(auth.Tokens.AccessToken)
}

func accessTokenPlanType(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			PlanType string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Auth.PlanType
}

func base64URLDecode(s string) ([]byte, error) {
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return base64.URLEncoding.DecodeString(s)
}

type appServerResponse struct {
	ID     any             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type appServerRateLimitsResponse struct {
	RateLimits struct {
		PlanType string `json:"planType"`
		Primary  *struct {
			UsedPercent        float64 `json:"usedPercent"`
			ResetsAt           int64   `json:"resetsAt"`
			WindowDurationMins int64   `json:"windowDurationMins"`
		} `json:"primary"`
		Secondary *struct {
			UsedPercent        float64 `json:"usedPercent"`
			ResetsAt           int64   `json:"resetsAt"`
			WindowDurationMins int64   `json:"windowDurationMins"`
		} `json:"secondary"`
	} `json:"rateLimits"`
}

func fetchUsageViaAppServer(home string) (*Usage, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(codexPath, "app-server")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	send := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = stdin.Write(append(data, '\n'))
		return err
	}

	if err := send(map[string]any{
		"id":     1,
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]string{
				"name":    "auto-switch",
				"version": "dev",
			},
		},
	}); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"id": 2, "method": "account/rateLimits/read"}); err != nil {
		return nil, err
	}

	timeout := time.After(1500 * time.Millisecond)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = "app-server exited before returning rate limits"
				}
				return nil, fmt.Errorf(msg)
			}
			usage, done, err := parseAppServerLine(line)
			if err == nil && done {
				return usage, nil
			}
		case <-timeout:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "timed out waiting for app-server rate limits"
			}
			return nil, fmt.Errorf(msg)
		}
	}
}

func parseAppServerLine(line string) (*Usage, bool, error) {
	var resp appServerResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, false, err
	}
	if resp.ID == nil {
		return nil, false, nil
	}
	switch fmt.Sprint(resp.ID) {
	case "1":
		return nil, false, nil
	case "2":
		if resp.Error != nil {
			return nil, true, fmt.Errorf(resp.Error.Message)
		}
		var result appServerRateLimitsResponse
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, true, err
		}
		usage := &Usage{
			FetchedAt: time.Now(),
			PlanType:  result.RateLimits.PlanType,
		}
		if p := result.RateLimits.Primary; p != nil {
			usage.PrimaryUtilization = p.UsedPercent
			usage.PrimaryResetsAt = parseResetTime(time.Now(), p.ResetsAt, 0)
		}
		if s := result.RateLimits.Secondary; s != nil {
			usage.SecondaryUtilization = s.UsedPercent
			usage.SecondaryResetsAt = parseResetTime(time.Now(), s.ResetsAt, 0)
		}
		return usage, true, nil
	default:
		return nil, false, nil
	}
}

func parseUsageLine(line []byte) *Usage {
	var event struct {
		Timestamp string `json:"timestamp"`
		Payload   struct {
			Type       string `json:"type"`
			RateLimits *struct {
				PlanType string `json:"plan_type"`
				Primary  *struct {
					UsedPercent    float64 `json:"used_percent"`
					ResetsAt       int64   `json:"resets_at"`
					ResetsInSecond int64   `json:"resets_in_seconds"`
				} `json:"primary"`
				Secondary *struct {
					UsedPercent    float64 `json:"used_percent"`
					ResetsAt       int64   `json:"resets_at"`
					ResetsInSecond int64   `json:"resets_in_seconds"`
				} `json:"secondary"`
			} `json:"rate_limits"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return nil
	}
	if event.Payload.Type != "token_count" || event.Payload.RateLimits == nil {
		return nil
	}

	sampleAt, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
	usage := &Usage{
		FetchedAt: sampleAt,
		PlanType:  event.Payload.RateLimits.PlanType,
	}
	if p := event.Payload.RateLimits.Primary; p != nil {
		usage.PrimaryUtilization = p.UsedPercent
		usage.PrimaryResetsAt = parseResetTime(sampleAt, p.ResetsAt, p.ResetsInSecond)
	}
	if s := event.Payload.RateLimits.Secondary; s != nil {
		usage.SecondaryUtilization = s.UsedPercent
		usage.SecondaryResetsAt = parseResetTime(sampleAt, s.ResetsAt, s.ResetsInSecond)
	}
	return usage
}

func parseResetTime(sampleAt time.Time, resetsAt, resetsInSeconds int64) time.Time {
	if resetsAt > 0 {
		return time.Unix(resetsAt, 0)
	}
	if resetsInSeconds > 0 {
		if sampleAt.IsZero() {
			sampleAt = time.Now()
		}
		return sampleAt.Add(time.Duration(resetsInSeconds) * time.Second)
	}
	return time.Time{}
}

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

func ProgressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if pct > 0 && filled == 0 {
		filled = 1
	}
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
