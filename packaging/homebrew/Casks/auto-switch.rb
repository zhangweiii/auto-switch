# typed: false
# frozen_string_literal: true

cask "auto-switch" do
  version :latest
  sha256 :no_check

  url "https://github.com/zhangweiii/auto-switch/releases/latest"
  name "auto-switch"
  desc "Automatically switch between Claude Code accounts by lowest usage"
  homepage "https://github.com/zhangweiii/auto-switch"

  deprecate! date: "2026-03-07", because: "is now distributed as a formula", replacement_formula: "zhangweiii/tap/auto-switch"

  caveats <<~EOS
    auto-switch is now distributed as a Homebrew formula instead of a cask.

      brew uninstall --cask zhangweiii/tap/auto-switch
      brew install zhangweiii/tap/auto-switch

    Your saved accounts are stored outside Homebrew's install directory and should remain intact.
  EOS
end
