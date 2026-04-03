# CLAUDE.md

## Project

Baton is a terminal-native TUI for orchestrating multiple Claude Code agents in parallel. Written in Go 1.25.

## Build & Test

```bash
go build -o baton .         # build
go test ./...               # run all tests
go test -race ./...         # run with race detector
go vet ./...                # static analysis
./baton doctor              # validate environment
```

## Architecture

- `cmd/` — Cobra CLI commands (root launches TUI, doctor validates env)
- `internal/pty/` — Raw PTY I/O wrapper around `creack/pty`. No goroutines — callers manage read loops.
- `internal/vt/` — Virtual terminal bridge around `x/vt.SafeEmulator`. Uses an `io.Pipe` bridge for `Read()` to allow thread-safe `Close()`.
- `internal/git/` — Git worktree CRUD, diff, merge via `exec.Command("git", ...)`. Worktrees live at `.baton/worktrees/<name>` with branches `baton/<name>`.
- `internal/agent/` — Composes PTY + VT + git into managed agents. Each agent has 3 goroutines: readLoop (PTY->VT), writeLoop (VT->PTY), statusLoop (idle detection). Manager handles lifecycle and events.
- `internal/tui/` — Bubble Tea v2 views: dashboard, focus, diff, prompt overlay, merge overlay, statusbar.

## Key Patterns

- **Bubble Tea v2**: `View()` returns `tea.View` (not string). Use `tea.NewView(content)` with `v.AltScreen = true`. No `tea.WithAltScreen()` option.
- **Charm imports**: Bubble Tea v2 is `charm.land/bubbletea/v2`. Lipgloss is still `github.com/charmbracelet/lipgloss`. x/vt is `github.com/charmbracelet/x/vt`.
- **Key forwarding**: `tea.KeyPressMsg` converts directly to `xvt.KeyPressEvent` — same underlying `ultraviolet.Key` struct.
- **Thread safety**: VT bridge uses an `io.Pipe` to decouple `Read()` from `SafeEmulator.Read()`, so `Close()` can unblock readers without racing on internal emulator state.
- **Agent names**: Must match `[a-zA-Z0-9][a-zA-Z0-9_-]*`. Enforced in `Manager.Create()`.
- **Shutdown sequence**: Close `m.done` -> kill agents -> wait for watcher goroutines -> close events channel. Agent kill: close PTY -> close terminal (unblocks writeLoop) -> wait for writeLoopDone.

## Testing

- `internal/pty/` — Tests echo, cat round-trip, close+done
- `internal/vt/` — Tests write/render, ANSI preservation, resize, SendText/Read
- `internal/git/` — Tests use temp repos with real git commands
- `internal/agent/` — Tests use `bash -c` commands instead of claude
- `internal/tui/` — Manual testing only (TUI views)

Always run `go test -race ./...` before committing — concurrency bugs have been caught and fixed by the race detector.

## Conventions

- Mouse support: click-to-select (left panel) and click-to-focus (right panel) on dashboard only. No config files, no scrollback in preview
- Agent program is hardcoded to `claude` (no program selector)
- `.baton/` is gitignored (auto-added on first run)
- Errors display briefly in the TUI and clear on next tick
