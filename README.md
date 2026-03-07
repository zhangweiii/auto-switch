# auto-switch

> Automatically switch between Claude Code accounts — always use the one with the most quota remaining.

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)]()

[中文文档](README.zh.md)

---

## Why auto-switch?

If you use multiple Claude Code subscriptions (personal, work, team…), you've probably hit the 5-hour rate limit mid-session and waited for it to reset. **auto-switch eliminates that wait** by transparently routing each `claude` invocation to the account with the most headroom.

| Feature | auto-switch | Manual switching | CCS (proxy) |
|---|:---:|:---:|:---:|
| Auto-selects least-used account | ✅ | ❌ | ⚠️ reactive only |
| Zero-overhead process replacement | ✅ | ✅ | ❌ proxy layer |
| No daemon / background process | ✅ | ✅ | ❌ |
| Built-in usage monitor | ✅ | ❌ | ❌ |
| Token auto-sync | ✅ | ❌ | ❌ |
| Single binary, no runtime deps | ✅ | ✅ | ❌ Node.js |

---

## How it works

```
auto-switch claude → fetch usage for all accounts (parallel, cached 5 min)
                   → score = 5h_util × 0.7 + 7d_util × 0.3
                   → pick lowest score
                   → write credentials (Keychain + file)
                   → syscall.Exec("claude", args...)   ← becomes claude, zero wrapper
```

The final step uses `syscall.Exec` to **replace** the current process with `claude`. There is no wrapper process — signals, stdin, stdout, and TTY all behave exactly as if you ran `claude` directly.

---

## Installation

### Build from source

```bash
git clone https://github.com/zhangweiii/auto-switch
cd auto-switch
go build -o auto-switch .
cp auto-switch ~/.local/bin/   # or any directory in $PATH
```

### Homebrew (macOS / Linux)

```bash
brew tap zhangweiii/tap
brew install auto-switch
```

---

## Quick start

### 1. Save your first account

Make sure you are already logged in to Claude Code, then run:

```bash
auto-switch login
# or skip the prompt with a flag:
auto-switch login --alias personal
```

### 2. Add a second account

Inside Claude Code, run `/logout`, then log in with your second account. Then save it:

```bash
auto-switch login --alias work
```

### 3. Let auto-switch decide

```bash
auto-switch claude
```

auto-switch checks the usage of every saved account and launches `claude` as the one with the most quota left.

### 4. Pass arguments through

All arguments are forwarded verbatim to `claude`:

```bash
auto-switch claude --continue
auto-switch claude -p "explain this file"
auto-switch claude --model claude-opus-4-6
```

### 5. Force a specific account

```bash
auto-switch claude --account work
```

---

## Seamless experience — alias `claude`

The best way to use auto-switch is to **replace your `claude` command** entirely. Add one line to your shell config:

**zsh** (`~/.zshrc`):
```zsh
alias claude='auto-switch claude'
```

**bash** (`~/.bashrc` or `~/.bash_profile`):
```bash
alias claude='auto-switch claude'
```

**fish** (`~/.config/fish/config.fish`):
```fish
alias claude 'auto-switch claude'
```

Reload your shell to activate it:

```bash
source ~/.zshrc   # or ~/.bashrc for bash
```

From now on just type `claude` as always — auto-switch silently picks the best account and gets out of the way:

```bash
claude                    # auto-selects least-used account
claude --continue         # args pass through unchanged
claude -p "review PR"     # non-interactive mode works too
```

---

## Commands

| Command | Description |
|---|---|
| `auto-switch login [--alias <name>]` | Save the currently logged-in Claude account |
| `auto-switch claude [args...]` | Switch to least-used account and launch claude |
| `auto-switch list` | Show all accounts with live usage bars |
| `auto-switch status` | Detailed usage view with reset countdowns |
| `auto-switch remove <alias>` | Delete a saved account |

### `auto-switch list`

```
Claude Code accounts (2)

  Alias          Email                         5h window                 7d window                 Expires
  ─────────────────────────────────────────────────────────────────────────────────────────────────────────
* personal       user1@example.com             ████████░░  67% ↺1h23m   ███░░░░░  30% ↺5d12h
  work           user2@example.com             ░░░░░░░░░░   5% ↺3h10m   █░░░░░░░  10% ↺5d12h

* active account  refreshed at 14:32:05
```

### `auto-switch status`

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

## Configuration

| Path | Description |
|---|---|
| `~/.config/auto-switch/accounts.json` | Account metadata (mode 0600) |
| `~/.config/auto-switch/usage-cache.json` | Usage cache (5-min TTL) |
| macOS Keychain (`Claude Code-credentials`) | OAuth tokens |
| `~/.claude/.credentials.json` | OAuth token fallback (Linux) |

Cache behaviour:
- Successful responses cached for **5 minutes**
- Errors **never cached** — next call retries immediately

---

## Token auto-sync

Claude Code silently rotates its OAuth token from time to time. On every invocation, auto-switch compares the token in the system Keychain against the stored value and updates `accounts.json` automatically if they differ. You never need to re-run `login` just because a token was rotated.

---

## Roadmap

- [x] Phase 1 — Claude Code multi-account switching
- [ ] Phase 2 — OpenAI Codex support
- [ ] Shell completion (zsh, bash, fish)
- [x] Homebrew formula (via zhangweiii/homebrew-tap)

---

## License

MIT
