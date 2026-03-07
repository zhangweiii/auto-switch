# Homebrew tap migration

This repository now publishes `auto-switch` as a Homebrew formula via GoReleaser.

## Tap repository changes

Apply these changes in `zhangweiii/homebrew-tap`:

1. Keep the generated formula at `Formula/auto-switch.rb`.
2. Replace the old cask with `packaging/homebrew/Casks/auto-switch.rb` from this repository for one or two release cycles.
3. After most users have migrated, remove the cask entirely.

## Existing user migration

Users who previously installed the cask should run:

```bash
brew uninstall --cask zhangweiii/tap/auto-switch
brew install zhangweiii/tap/auto-switch
```

Runtime data is stored under `~/.config/auto-switch` and in system credential storage, so this migration should not remove saved accounts.
