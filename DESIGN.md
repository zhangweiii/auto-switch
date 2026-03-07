# auto-switch Technical Design

## 1. Overview

`auto-switch` is a CLI tool for managing and automatically switching between AI coding assistant accounts (Claude Code, Codex, etc.). The core strategy is to **always select the account with the lowest current usage**, maximising subscription quota utilisation.

---

## 2. Core Problem Analysis

### 2.1 Claude Code Authentication (Research Findings)

Claude Code stores credentials in multiple layers:

| Location | Platform | Content |
|---|---|---|
| macOS Keychain | macOS | OAuth token (primary), service: `Claude Code-credentials` |
| `~/.claude/.credentials.json` | All platforms | OAuth token (Keychain fallback) |
| `~/.claude.json` | All platforms | Account metadata (email, uuid, org, etc.) |
| `CLAUDE_CODE_OAUTH_TOKEN` env var | All platforms | Highest priority, overrides all other storage |

**Token structure** (JSON stored in Keychain):
```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat-...",
    "refreshToken": "...",
    "expiresAt": 1234567890
  }
}
```

**Account metadata structure** (`oauthAccount` field in `~/.claude.json`):
```json
{
  "accountUuid": "xxx",
  "emailAddress": "user@example.com",
  "organizationUuid": "yyy",
  "organizationName": "...",
  "billingType": "apple_subscription",
  "displayName": "wei"
}
```

### 2.2 Usage Monitoring (Research Findings)

**Method 1: Anthropic OAuth API (recommended, real-time)**
```
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <access_token>
anthropic-beta: oauth-2025-04-20
```
Returns `five_hour` and `seven_day` window utilisation as percentages plus reset timestamps.

**Method 2: Claude.ai Web API (requires sessionKey cookie)**
```
GET https://claude.ai/api/organizations/{orgId}/usage
```

**Method 3: Local JSONL file parsing (offline, for statistics)**
- `~/.claude/stats-cache.json`: pre-aggregated usage stats (daily, by model)
- `~/.claude/transcripts/*.jsonl`: raw session records with token counts

**Claude Code limits (2025)**:
| Plan | 5h window tokens | 7d window |
|---|---|---|
| Pro | ~44,000 | limited |
| Max5 | ~88,000 | limited |
| Max20 | ~220,000 | limited |

### 2.3 Account Switching Approaches

| Approach | Mechanism | Pros | Cons |
|---|---|---|---|
| A. Env var injection | Set `CLAUDE_CODE_OAUTH_TOKEN` at launch | Clean, no system config changes | Requires going through auto-switch each time |
| B. Keychain replacement | Write directly to macOS Keychain | Transparent to claude | Poor cross-platform support |
| C. Config dir switching | `--config-dir` flag or symlink to profile dir | Full isolation | Claude Code support unverified |
| D. Credentials file replacement | Replace `~/.claude/.credentials.json` + `~/.claude.json` | Cross-platform, no root needed | File operations, race condition risk |

**Recommended: A (env var injection) + D (file replacement)**
- Inject `CLAUDE_CODE_OAUTH_TOKEN` when launching claude (cleanest approach)
- Also update `~/.claude/.credentials.json` and `~/.claude.json` for persistence
- On macOS, additionally update the Keychain entry for native compatibility

---

## 3. Architecture

### 3.1 Directory Layout

```
auto-switch/
├── cmd/                    # CLI entry points (cobra commands)
│   ├── root.go
│   ├── common.go           # shared helpers (loadAndSync)
│   ├── login.go            # auto-switch login
│   ├── claude.go           # auto-switch claude [args...]
│   ├── list.go             # auto-switch list
│   ├── status.go           # auto-switch status
│   └── remove.go           # auto-switch remove <alias>
├── internal/
│   ├── store/              # config persistence
│   │   ├── store.go        # read/write ~/.config/auto-switch/accounts.json
│   │   └── sync.go         # token auto-sync logic
│   └── claude/             # Claude Code implementation (phase 1)
│       ├── auth.go         # credential read/write (Keychain + file)
│       └── usage.go        # usage API + caching
├── main.go
├── go.mod
└── DESIGN.md
```

### 3.2 Core Interface Design

```go
// Provider interface — supports future extension to Codex, etc.
type Provider interface {
    Name() string
    Login(ctx context.Context) (*Account, error)
    GetUsage(ctx context.Context, account *Account) (*Usage, error)
    Switch(ctx context.Context, account *Account) error
    Launch(ctx context.Context, account *Account, args []string) error
}

// Account holds per-account metadata and credentials.
type Account struct {
    ID          string    // internal UUID
    Alias       string    // user-assigned name, e.g. "work", "personal"
    Email       string
    Provider    string    // "claude" | "codex"
    Credentials Credentials
    OrgUUID     string
    CreatedAt   time.Time
}

// Usage holds the current window utilisation for one account.
type Usage struct {
    FiveHourUtilization float64
    FiveHourResetsAt    time.Time
    SevenDayUtilization float64
    SevenDayResetsAt    time.Time
    FetchedAt           time.Time
    Cached              bool
    Error               string
}
```

### 3.3 Config File

Path: `~/.config/auto-switch/accounts.json`

```json
{
  "version": 1,
  "accounts": [
    {
      "id": "uuid-1",
      "alias": "personal",
      "email": "user1@example.com",
      "provider": "claude",
      "org_uuid": "xxx",
      "created_at": "2026-01-01T00:00:00Z"
    },
    {
      "id": "uuid-2",
      "alias": "work",
      "email": "user2@example.com",
      "provider": "claude",
      "org_uuid": "yyy",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

**OAuth token storage**:
- macOS: Keychain, service=`auto-switch`, account=`<provider>:<account-id>`
- Linux/Windows: `~/.config/auto-switch/credentials` (mode 0600)

---

## 4. CLI Command Design

### 4.1 Command Reference

```
auto-switch <command> [options]

Commands:
  login                Save the currently logged-in account
  claude [args...]     Switch to least-used Claude account and launch claude
  list                 List all accounts with current usage
  status               Show detailed real-time usage
  remove <alias>       Delete a saved account
  help                 Show help

Flags (on the claude subcommand):
  --account <alias>    Force a specific account (overrides auto-selection)
```

### 4.2 Example Output

**`auto-switch login`**:
```
Reading current Claude Code credentials...
Detected account: user@example.com (My Org)
Enter an alias for this account (e.g. personal, work): personal

✓ Account "personal" (user@example.com) saved

Tip: run /logout in Claude Code → log in with next account → run auto-switch login again
```

**`auto-switch claude`**:
```
Checking usage for 2 accounts...

  personal    ████████░░  67% ↺1h23m    7d: ███░░░░░  30%
  work        ░░░░░░░░░░   5% ↺3h10m    7d: █░░░░░░░  10%

→ switching to "work" (lowest usage)
```

**`auto-switch list`**:
```
Claude Code accounts (2)

  Alias          Email                         5h window                 7d window                 Expires
  ─────────────────────────────────────────────────────────────────────────────────────────────────────────
* personal       user1@example.com             ████████░░  67% ↺1h23m   ███░░░░░  30% ↺5d12h
  work           user2@example.com             ░░░░░░░░░░   5% ↺3h10m   █░░░░░░░  10% ↺5d12h

* active account  refreshed at 14:32:05
```

**`auto-switch status`**:
```
Claude Code usage  (2026-03-07 14:32:05)
────────────────────────────────────────────────────────────

personal (user1@example.com) [active]
  5h window: ████████████████░░░░  67.0%  resets in 1h23m
  7d window: ██████░░░░░░░░░░░░░░  30.0%  resets in 5d12h

work (user2@example.com)
  5h window: █░░░░░░░░░░░░░░░░░░░   5.0%  resets in 3h10m
  7d window: ██░░░░░░░░░░░░░░░░░░  10.0%  resets in 5d12h
```

---

## 5. Account Selection Strategy

### 5.1 Least-Used Strategy (default)

```
score = five_hour_utilization * 0.7 + seven_day_utilization * 0.3
select the account with the lowest score
```

- 5-hour window weight: 70% (short-term capacity matters most)
- 7-day window weight: 30% (long-term balance)
- Skip any account where the 5-hour window is ≥ 95%

### 5.2 Usage Cache

- Successful responses cached for **5 minutes** to avoid hammering the API
- Errors are **never cached** so the next call retries immediately
- Cache file: `~/.config/auto-switch/usage-cache.json`

---

## 6. Claude Code Switching — Implementation Details

### 6.1 Switch Steps

```
1. Read target account's OAuth token from our config (auto-synced from Keychain)
2. Check token expiry; warn if < 30 days remaining
3. Write token to ~/.claude/.credentials.json
4. Update oauthAccount field in ~/.claude.json
5. Update macOS Keychain entry "Claude Code-credentials"
6. Set CLAUDE_CODE_OAUTH_TOKEN env var (highest priority)
7. syscall.Exec → replace current process with claude [args...]
```

### 6.2 Token Auto-Sync

Claude Code silently refreshes the OAuth token. On every `list`, `status`, and `claude` invocation, auto-switch:
1. Reads the current active email from `~/.claude.json`
2. Reads the latest token from Keychain / credentials file
3. If the token differs from what is stored, updates `accounts.json` automatically

### 6.3 Credential File Formats

`~/.claude/.credentials.json`:
```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat-...",
    "refreshToken": "...",
    "expiresAt": 1234567890000
  }
}
```

`~/.claude.json` (`oauthAccount` field only; all other fields preserved):
```json
{
  "oauthAccount": {
    "accountUuid": "...",
    "emailAddress": "user@example.com",
    "organizationUuid": "...",
    "organizationName": "...",
    "billingType": "apple_subscription",
    "displayName": "..."
  }
}
```

---

## 7. Dependencies

| Purpose | Library | Rationale |
|---|---|---|
| CLI framework | `github.com/spf13/cobra` | Industry standard (kubectl, gh) |
| Keychain (future) | `github.com/zalando/go-keyring` | Cross-platform (macOS/Linux/Windows) |
| UUID generation | `github.com/google/uuid` | Stable, widely used |
| HTTP client | stdlib `net/http` | No extra dependency needed |
| JSON config | stdlib `encoding/json` | Sufficient for this use case |

---

## 8. Phase 2 — Codex Extension

### 8.1 Codex Authentication (Pre-research)

OpenAI Codex CLI stores credentials at:
- `~/.codex/auth.json` (session token)
- Or env var `OPENAI_API_KEY`

### 8.2 Extension Approach

```go
// Phase 2: implement the Provider interface for Codex
type CodexProvider struct{}

func (p *CodexProvider) Name() string { return "codex" }
func (p *CodexProvider) Login(ctx context.Context) (*Account, error) { ... }
func (p *CodexProvider) GetUsage(ctx context.Context, a *Account) (*Usage, error) { ... }
func (p *CodexProvider) Switch(ctx context.Context, a *Account) error { ... }
func (p *CodexProvider) Launch(ctx context.Context, a *Account, args []string) error { ... }
```

Registration (uncomment to enable):
```go
var providers = map[string]provider.Provider{
    "claude": claude.New(),
    // "codex": codex.New(),  // uncomment in phase 2
}
```

---

## 9. Development Plan (Phase 1)

**M1 — Foundation**
- [x] cobra command structure
- [x] Config file read/write (`~/.config/auto-switch/accounts.json`)
- [x] Keychain read via `security` CLI

**M2 — Claude Login**
- [x] Read existing Claude Code Keychain / credentials file
- [x] `auto-switch login` interactive flow
- [x] Save account to config
- [x] `auto-switch list` command

**M3 — Usage Monitoring**
- [x] Anthropic OAuth usage API integration
- [x] 5-minute success cache, no error caching
- [x] `auto-switch status` command
- [x] Token auto-sync on every invocation

**M4 — Auto Switching**
- [x] Least-used account selection strategy
- [x] Credentials write (env var + file + Keychain)
- [x] `auto-switch claude [args...]` command
- [x] `--account` flag for forced selection

**M5 — Polish**
- [ ] README
- [ ] Shell completion
- [ ] Homebrew formula / install script

---

## 10. Key Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Anthropic changes OAuth token format | Usage query fails | Graceful error with user-friendly message |
| Claude Code changes credentials file format | Switch fails | Version detection + clear error output |
| Token expiry causes launch failure | Cannot start claude | Expiry warning + prompt to re-run login |
| macOS Keychain unavailable (SSH/tmux) | Cannot read token | Auto-fallback to credentials file |
| Concurrent token writes from multiple processes | Config corruption | File permissions (0600) limit exposure |
