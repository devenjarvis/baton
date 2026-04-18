# Changelog

All notable changes to Baton will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Every PR should update the `[Unreleased]` section with a short entry describing the change.

## [Unreleased]

### Added

- IDE editor dropdown in global and per-repo settings. On macOS, probes `/Applications` and `~/Applications` for a curated list of editors (Zed, VS Code, Cursor, JetBrains IDEs) and generates `open -a "<App>"` invocations that take focus and support opening additional worktrees alongside an already-running editor window. Custom Command option preserves free-text entry; legacy stored values load unchanged.

### Fixed

- Startup splash text artifacts when Claude Code enters its TUI (alt-screen transition detection replaces 500ms delayed resize heuristic).
- Mid-repaint flicker in dashboard preview (StableRender with 16ms quiescence threshold).
- VT emulator / lipgloss container sizing mismatch causing displaced content in focusList mode.
- IDE launch path now uses a quote-aware tokenizer so multi-word app names in `open -a "Visual Studio Code"` style commands launch correctly instead of fragmenting on whitespace.

## [0.1.0] — 2026-04-15

Initial public release.

### Added

- Dashboard view listing all managed agents with live status (idle/active/error) and visual-stability detection.
- Focus mode: full-screen interactive session with a selected agent; all keys forwarded to the PTY.
- Diff view: two-mode experience — summary list sorted by change magnitude, and side-by-side (≥120 cols) or unified (below) detail view.
- Merge: `git merge --no-ff` from a worktree branch into the base branch with cleanup of the worktree.
- Prompt and merge overlays for creating and landing agent work without leaving the TUI.
- `baton doctor` command for validating the environment (Go, git, `claude` on PATH).
- Mouse support on the dashboard (click-to-select, click-to-focus).
- Isolated git worktrees under `.baton/worktrees/<name>` on branches `baton/<name>`.
- Virtual terminal bridge built on `charmbracelet/x/vt` for thread-safe rendering of agent output.

[Unreleased]: https://github.com/devenjarvis/baton/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/devenjarvis/baton/releases/tag/v0.1.0
