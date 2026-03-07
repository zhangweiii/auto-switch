# auto-switch

> 自动切换 Claude Code 和 Codex 账号，每次都使用剩余用量最多的那个。

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)]()

[English](README.md)

---

## 为什么需要 auto-switch？

如果你有多个 Claude Code 或 Codex 订阅（个人、工作、团队……），一定遇到过 5 小时窗口用完、被迫等待重置的情况。**auto-switch 消除这种等待**——每次运行 `claude` / `codex` 时，它会自动选择用量最少的账号，让你的订阅配额利用率最大化。

| 特性 | auto-switch | 手动切换 | CCS（代理） |
|---|:---:|:---:|:---:|
| 自动选用量最少的账号 | ✅ | ❌ | ⚠️ 仅被动 failover |
| 零开销进程替换 | ✅ | ✅ | ❌ 有代理层 |
| 无守护进程 / 后台服务 | ✅ | ✅ | ❌ |
| 内置用量监控 | ✅ | ❌ | ❌ |
| Token 自动同步 | ✅ | ❌ | ❌ |
| 单二进制，无运行时依赖 | ✅ | ✅ | ❌ 需要 Node.js |

---

## 工作原理

```
auto-switch claude → 并发查询所有账号用量（结果缓存 5 分钟）
                   → score = 5小时用量 × 0.7 + 7天用量 × 0.3
                   → 选择得分最低的账号
                   → 写入凭证（Keychain + 文件）
                   → syscall.Exec("claude", args...)   ← 替换为 claude 进程，零包装
```

最后一步使用 `syscall.Exec` **替换**当前进程为 `claude`，没有包装进程——信号处理、stdin、stdout、TTY 行为与直接运行 `claude` 完全一致。

---

## 安装

### 从源码构建

```bash
git clone https://github.com/zhangweiii/auto-switch
cd auto-switch
go build -o auto-switch .
cp auto-switch ~/.local/bin/   # 或任意 $PATH 目录
```

### Homebrew（macOS / Linux）

```bash
brew tap zhangweiii/tap
brew install zhangweiii/tap/auto-switch
```

升级到最新版本：

```bash
brew update && brew upgrade zhangweiii/tap/auto-switch
```

如果你之前安装的是旧的 cask，请迁移到 formula：

```bash
brew uninstall --cask zhangweiii/tap/auto-switch
brew install zhangweiii/tap/auto-switch
```

账号数据和缓存保存在 `~/.config/auto-switch` 以及系统凭据存储中，迁移安装方式通常不会删掉这些数据。

---

## 快速开始

### 1. 保存第一个账号

确保已经在目标 CLI 中登录，然后运行：

```bash
auto-switch login
# 或通过 flag 跳过交互：
auto-switch login --alias personal
auto-switch login --provider codex --alias personal
```

### 2. 添加第二个账号

先从对应 CLI 退出登录，再用第二个账号登录，然后保存：

```bash
auto-switch login --alias work
auto-switch login --provider codex --alias work
```

### 3. 自动切换并启动

```bash
auto-switch claude
auto-switch codex
```

auto-switch 会查询所有账号的用量，选择剩余配额最多的账号启动 `claude` / `codex`。

### 4. 透传参数

所有参数都会原样转发给目标 CLI：

```bash
auto-switch claude --continue
auto-switch claude -p "explain this file"
auto-switch claude --model claude-opus-4-6
auto-switch --account work codex
auto-switch codex exec "review this repo"
```

### 5. 强制指定账号

```bash
auto-switch --account work claude
auto-switch --account work codex
```

---

## 最佳体验——将 `claude` 设为别名

最推荐的方式是**直接替换 `claude` 命令**。在 shell 配置文件中添加一行：

**zsh**（`~/.zshrc`）：
```zsh
alias claude='auto-switch claude'
```

**bash**（`~/.bashrc` 或 `~/.bash_profile`）：
```bash
alias claude='auto-switch claude'
```

**fish**（`~/.config/fish/config.fish`）：
```fish
alias claude 'auto-switch claude'
```

重新加载 shell 使其立即生效：

```bash
source ~/.zshrc   # bash 用户执行 source ~/.bashrc
```

之后像往常一样输入 `claude`，auto-switch 会静默在后台选择最优账号：

```bash
claude                    # 自动选用量最少的账号
claude --continue         # 所有参数原样透传
claude -p "review PR"     # 非交互模式同样正常工作
```

---

## 命令说明

| 命令 | 说明 |
|---|---|
| `auto-switch login [--provider <名称>] [--alias <名称>]` | 保存当前已登录的 Claude Code 或 Codex 账号 |
| `auto-switch claude [参数...]` | 切换到用量最少的账号并启动 claude |
| `auto-switch codex [参数...]` | 切换到用量最少的账号并启动 codex |
| `auto-switch list` | 显示所有账号及实时用量进度条 |
| `auto-switch status` | 详细用量视图，含重置倒计时 |
| `auto-switch remove <别名>` | 删除已保存的账号 |

按 provider 管理：
- `auto-switch login --provider codex --alias work`
- `auto-switch list --provider codex`
- `auto-switch status --provider codex`
- `auto-switch remove --provider codex work`

### `auto-switch list`

```
Claude Code accounts (2)

  Alias          Email                         5h window                 7d window
  ───────────────────────────────────────────────────────────────────────────────────────────
* personal       user1@example.com             ████████░░  67% ↺1h23m   ███░░░░░  30% ↺5d12h
  work           user2@example.com             ░░░░░░░░░░   5% ↺3h10m   █░░░░░░░  10% ↺5d12h

* 当前活跃账号  数据更新于 14:32:05
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

## 配置文件

| 路径 | 说明 |
|---|---|
| `~/.config/auto-switch/accounts.json` | 账号元数据（权限 0600） |
| `~/.config/auto-switch/usage-cache.json` | 用量缓存（5 分钟 TTL） |
| macOS Keychain（`Claude Code-credentials`） | OAuth token |
| `~/.claude/.credentials.json` | OAuth token 文件 fallback（Linux） |

缓存策略：
- 成功响应缓存 **5 分钟**
- 错误**不缓存**——下次立即重试

---

## Token 自动同步

Claude Code 会不定期静默刷新 OAuth token。auto-switch 每次运行时会自动对比 Keychain 中的最新 token 与配置中存储的值，如果不一致则自动更新 `accounts.json`。你不需要因为 token 被轮换而重新运行 `login`。

对于 Codex，auto-switch 会把每个账号隔离到 `~/.config/auto-switch/codex/<alias>` 下独立的 `CODEX_HOME`。用量来自该账号最近一次会话日志里的 `rate_limits` 字段，因此 `auto-switch codex` 可以在不覆盖主 `~/.codex` 的前提下自动挑选当前用量最低的账号。

---

## Roadmap

- [x] 一期 — Claude Code 多账号切换
- [x] 二期 — OpenAI Codex 支持
- [ ] Shell 自动补全（zsh、bash、fish）
- [x] Homebrew formula (via zhangweiii/homebrew-tap)

---

## 许可证

MIT
