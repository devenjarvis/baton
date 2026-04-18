# Changelog

All notable changes to Baton will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Every PR should update the `[Unreleased]` section with a short entry describing the change.

## [Unreleased]

### Added

- Meaningful branch names derived from the user's first prompt. Sessions still start on a random adjective-noun branch so Claude can launch immediately, but on the first real `user-prompt-submit` hook the branch is renamed in place (via `git branch -m`, which atomically updates the worktree's HEAD symref) to a slug of the prompt — e.g. `baton/warm-ibis` becomes `baton/add-dark-mode-to-dashboard`. Slash commands like `/clear` are skipped so the next real prompt still triggers the rename. Attached sessions and resumed sessions are exempt, since their branch name is already meaningful. The preview header now surfaces `Branch:` alongside the session display name so the rename is visible.
- `{user}` and `{date}` template variables in `BranchPrefix`. `{user}` resolves from `git config user.name` (falling back to `$USER`), slugified; `{date}` resolves to today's `YYYY-MM-DD`. Unknown `{tokens}` are left literal so existing prefixes with braces are unaffected. Example: `BranchPrefix: "{user}/"` produces `dj/add-dark-mode` for a first-prompt rename.
- GoReleaser config and release workflow: pushing a `v*` tag builds darwin/linux amd64+arm64 archives, publishes a GitHub release, and updates the `devenjarvis/homebrew-tap` formula.

### Changed

- `x` (kill agent) and `X` (kill session) now tear down off the UI thread and mark the affected row as `closing…` until the worktree teardown completes, so the dashboard stays interactive while Claude exits and `git worktree remove --force` runs.
- Reverted "Release mouse capture in focus terminal view": the focus terminal view now re-captures the mouse. Text selection via the host terminal's drag was unreliable (copied text included the surrounding TUI frame), and capturing the mouse restores consistent keybinding/scroll behavior in focus mode.

### Fixed

- Permission-prompt approvals now clear the waiting indicator immediately via Claude's `PreToolUse` hook, instead of lingering yellow until the turn ends.
- GoReleaser now publishes the Homebrew formula into the tap's `Formula/` directory (via `directory: Formula`). v0.1.0 landed `baton.rb` at the tap repo root, where newer Homebrew versions don't discover it — installs with `brew install devenjarvis/tap/baton` would fail with "No available formula."
- Restore auto-naming of sessions and agents from the first Claude prompt (was broken in 0.1.0 hook refactor). The dashboard now relabels a fresh agent's random `adjective-noun` placeholder to a slugified version of the user's first prompt as soon as the `UserPromptSubmit` hook fires; sessions that already have a display name (branch-derived or restored from state) are preserved.
- Mouse-wheel scrolling now works inside Claude Code's `/tui fullscreen` mode. When the focused agent is in alt-screen, wheel events are forwarded to the agent (so Claude's internal scrollback handles them) instead of being consumed by baton's own scrollback buffer, which is inert for alt-screen apps. Exiting fullscreen restores baton's scrollback. Entering fullscreen while scrolled back snaps the preview back to live.

## [0.1.0] — 2026-04-18

Initial public release.

### Added

- Dashboard view listing all managed repos, sessions, and agents with live status (idle/active/waiting/done/error) and visual-stability detection.
- Focus mode: interactive preview of a selected agent with keys forwarded to the PTY, keyboard scrollback (`pgup` / `pgdn` / `home`), and native click-and-drag text selection in the host terminal.
- Diff view: summary list sorted by change magnitude, plus side-by-side (≥120 cols) or unified (below) detail view.
- Merge: `git merge --no-ff` from a worktree branch into the base branch with cleanup of the worktree.
- Prompt and merge overlays for creating and landing agent work without leaving the TUI.
- `baton doctor` for validating git, `claude` on PATH (with `--settings` support), the baton binary, hook-pipeline round-trip, git repo, and GitHub auth.
- Hook pipeline: per-session settings file wires Claude Code's hooks (`session-start`, `stop`, `session-end`, `notification`, `user-prompt-submit`) to `baton hook <event>`, routed back to the running TUI over a unix socket for hook-driven status detection.
- GitHub integration: PR creation, checks/review polling, and a "fix failing checks" flow (`f`) that fetches failed check logs and dispatches them to an idle agent.
- IDE editor dropdown in global and per-repo settings. On macOS, probes `/Applications` and `~/Applications` for a curated list of editors (Zed, VS Code, Cursor, JetBrains IDEs) and generates `open -a "<App>"` invocations that take focus and support opening additional worktrees alongside an already-running editor window. Custom Command option preserves free-text entry.
- Shell agent: open a shell in the selected session's worktree without leaving the TUI (`t`).
- Branch picker: start a session on an existing branch or PR (`o`).
- Global and per-repo settings persisted on disk (`AgentProgram`, `BypassPermissions`, `IDECommand`, etc.).
- Session persistence: `q` detaches cleanly; a later `baton` invocation reattaches to preserved worktrees.
- Mouse support on the dashboard (click-to-select, click-to-focus).
- Isolated git worktrees under `.baton/worktrees/<name>` on branches `baton/<name>`, with `.baton/` auto-added to the repo's `.gitignore` on first run.
- Virtual terminal bridge built on `charmbracelet/x/vt` for thread-safe rendering of agent output.
- Optional audio chimes for status transitions.

[Unreleased]: https://github.com/devenjarvis/baton/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/devenjarvis/baton/releases/tag/v0.1.0
