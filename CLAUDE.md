# CLAUDE.md

## Project

Baton is a terminal-native TUI for orchestrating multiple Claude Code agents in parallel. Written in Go 1.25.

## Build & Test

```bash
go build -o baton .         # build
go test ./...               # unit tests
go test -race ./...         # with race detector (required before commit)
go vet ./...                # static analysis
golangci-lint run           # lint (uses .golangci.yml)
gofumpt -w .                # format all Go files
./baton doctor              # validate environment + hook pipeline round-trip
```

End-to-end TUI tests live under `internal/e2e/` behind the `e2e` build tag and need `tu` v0.6.0+:

```bash
go test -tags e2e -timeout 300s -v ./internal/e2e/
```

Always run `go test -race ./...` before committing — concurrency bugs have been caught and fixed by the race detector.

## Architecture

- `cmd/root.go` — Cobra root, launches the TUI.
- `cmd/doctor.go` — Environment validation (git ≥ 2.20, `claude` on PATH with `--settings` support, hook-socket round-trip, git repo, GitHub auth).
- `cmd/hook.go` — Not for humans. Claude Code invokes `baton hook <event>` per the settings file baton writes; it forwards the JSON payload to the running baton over `BATON_HOOK_SOCKET`. Always exits 0 so hook failures never block Claude.
- `internal/pty/` — Raw PTY I/O around `creack/pty`. No goroutines — callers manage read loops.
- `internal/vt/` — Virtual terminal bridge around `x/vt.SafeEmulator`. Uses an `io.Pipe` for `Read()` so `Close()` is thread-safe without racing emulator internals.
- `internal/git/` — Worktree CRUD, diff, merge via `exec.Command("git", ...)`. Worktrees live at `.baton/worktrees/<name>` on branches `baton/<name>`.
- `internal/agent/` — Composes PTY + VT + git into managed agents. Each agent has 3 goroutines: readLoop (PTY→VT), writeLoop (VT→PTY), statusLoop (idle detection). Sessions group agents sharing a worktree. Manager handles lifecycle and events.
- `internal/tui/` — Bubble Tea v2 views: dashboard (list + preview), diff summary/detail, global/repo config forms, file browser (add repo), branch picker (session on existing branch/PR), statusbar.
- `internal/hook/` — Unix-socket server + client for Claude hook events (`session-start`, `stop`, `session-end`, `notification`, `user-prompt-submit`) plus the settings file generator that wires Claude's hooks to `baton hook`.
- `internal/github/` — GitHub API wrapper for PRs, checks, review status, and check-run log fetching (powers `f` fix-checks).
- `internal/config/` — Global settings (`~/.config/baton/settings.json`) and per-repo settings (`.baton/settings.json`) plus resolution logic.
- `internal/state/` — Session persistence so `q` detaches cleanly and a later `baton` invocation reattaches.
- `internal/editor/` — IDE launcher helpers; macOS app probing and quote-aware tokenizer for `open -a "Visual Studio Code"` style commands.
- `internal/audio/` — Optional chimes for status transitions (best-effort; nil on failure).
- `internal/e2e/` — End-to-end TUI tests driven by the `tu` headless virtual terminal (build tag `e2e`).

## Key Patterns

- **Bubble Tea v2**: `View()` returns `tea.View`, not string. Use `tea.NewView(content)` with `v.AltScreen = true`. No `tea.WithAltScreen()` option.
- **Charm imports**: Bubble Tea v2 is `charm.land/bubbletea/v2`. Lipgloss is still `github.com/charmbracelet/lipgloss`. x/vt is `github.com/charmbracelet/x/vt`.
- **Key forwarding**: `tea.KeyPressMsg` converts directly to `xvt.KeyPressEvent` — same underlying `ultraviolet.Key` struct.
- **Thread safety**: VT bridge uses an `io.Pipe` to decouple `Read()` from `SafeEmulator.Read()`, so `Close()` can unblock readers without racing emulator state.
- **Agent names**: Must match `[a-zA-Z0-9][a-zA-Z0-9_-]*`. Enforced in `Manager.Create()`.
- **Shutdown sequence**: close `m.done` → kill agents → wait for watcher goroutines → close events channel. Agent kill: close PTY → close terminal (unblocks writeLoop) → wait for writeLoopDone.
- **Hooks**: Baton writes a per-session settings JSON and launches `claude --settings <path>` so each hook invocation carries `BATON_HOOK_SOCKET` and `BATON_AGENT_ID` env, routing events back over a unix socket. Running `claude` outside baton exits the hook CLI silently.
- **Status detection**: visual-stability detection + hook-driven signals classify agents as idle / active / waiting / done / error. `StatusWaiting` is distinct (permission prompts, input blocks) and surfaces in a dashboard accent color.

## Testing

- `internal/pty/` — Echo, cat round-trip, close+done.
- `internal/vt/` — Write/render, ANSI preservation, resize, SendText/Read.
- `internal/git/` — Real git on temp repos.
- `internal/agent/` — Uses `bash -c` instead of `claude`.
- `internal/hook/` — Socket server unit tests, settings generator tests.
- `internal/config/` — Global + per-repo settings, resolution, migration.
- `internal/tui/` — Mostly manual; `app_test.go` and `idecommand_test.go` cover the testable pieces.
- `internal/e2e/` — End-to-end via `tu` CLI, `e2e` build tag.

## Conventions

- Mouse support: click-to-select (left panel) and click-to-focus (right panel) on the dashboard only.
- Agent program defaults to `claude` but is configurable via global/per-repo settings (`AgentProgram`).
- `.baton/` is gitignored (auto-added to the repo's `.gitignore` on first run).
- Errors display briefly in the TUI and clear on next tick; no modal error dialogs.
- Claude hook CLI (`baton hook`) is silent on stdout and always exits 0 — Claude interprets stdout as hook feedback.
- Changelog: every PR should add a line under `[Unreleased]` in `CHANGELOG.md`.
