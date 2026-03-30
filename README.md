# Baton

A terminal-native tool for orchestrating multiple [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agents in parallel. Each agent runs in an isolated git worktree with its own PTY, rendered via a virtual terminal emulator. Monitor agents from a dashboard, interact in focus mode, review diffs, and merge results — all without leaving the terminal.

No tmux dependency.

## Requirements

- Go 1.25+
- Git 2.20+
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH)

Verify with:

```bash
baton doctor
```

## Install

```bash
go install github.com/devenjarvis/baton@latest
```

Or build from source:

```bash
git clone https://github.com/devenjarvis/baton.git
cd baton
go build -o baton .
```

## Usage

Run `baton` inside a git repository:

```bash
baton
```

### Keybindings

**Dashboard:**

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate agent list |
| `enter` | Focus selected agent (full-screen interactive) |
| `n` | Create new agent |
| `d` | View diff for selected agent |
| `x` | Kill selected agent |
| `m` | Merge selected agent's changes |
| `q` | Quit (confirms if agents running) |

**Focus mode:**

| Key | Action |
|-----|--------|
| `ctrl+b` | Return to dashboard |
| *all other keys* | Forwarded to the agent |

**Diff view:**

| Key | Action |
|-----|--------|
| `j` / `k` | Scroll |
| `d` / `u` | Page down / up |
| `q` / `esc` | Back to dashboard |

## How It Works

When you create an agent, Baton:

1. Creates an isolated git worktree at `.baton/worktrees/<name>` with branch `baton/<name>`
2. Spawns `claude "<task>"` in a PTY inside that worktree
3. Feeds PTY output through a virtual terminal emulator ([charmbracelet/x/vt](https://github.com/charmbracelet/x/vt))
4. Renders the emulator's ANSI output in the TUI via [Bubble Tea v2](https://github.com/charmbracelet/bubbletea)

When you merge, Baton runs `git merge --no-ff` from the worktree branch into your base branch, then cleans up the worktree.

## Architecture

```
main.go              Entry point
cmd/
  root.go            Cobra root command, launches TUI
  doctor.go          Environment validation
internal/
  pty/               PTY wrapper (creack/pty)
  vt/                Virtual terminal bridge (x/vt SafeEmulator)
  git/               Worktree CRUD, diff, merge
  agent/             Agent lifecycle (PTY + VT + git composed)
  tui/               Bubble Tea views (dashboard, focus, diff, prompt, merge)
```

## License

MIT
