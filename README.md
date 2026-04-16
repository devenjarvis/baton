# Baton

**A terminal-native orchestrator for parallel [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agents.**

[![CI](https://img.shields.io/github/actions/workflow/status/devenjarvis/baton/ci.yml?branch=main&label=ci)](https://github.com/devenjarvis/baton/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/devenjarvis/baton?include_prereleases&sort=semver)](https://github.com/devenjarvis/baton/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/devenjarvis/baton)](https://goreportcard.com/report/github.com/devenjarvis/baton)
[![License](https://img.shields.io/github/license/devenjarvis/baton)](./LICENSE)

![Baton demo](./demo/baton.gif)

## Why Baton?

Running one Claude Code agent is easy. Running several at once — each on its own branch, without stomping on each other's working trees — is not. Baton gives each agent an isolated git worktree, wires its PTY into a virtual terminal emulator, and surfaces them all from a single dashboard. Switch focus, review diffs, and merge results without leaving the terminal and without a tmux dependency.

## Install

### Homebrew (macOS & Linux)

```bash
brew install devenjarvis/tap/baton
```

### Download a release binary

Grab a tarball for your platform from the [latest release](https://github.com/devenjarvis/baton/releases/latest) and drop the `baton` binary on your `PATH`.

### Go

```bash
go install github.com/devenjarvis/baton@latest
```

### Build from source

```bash
git clone https://github.com/devenjarvis/baton.git
cd baton
go build -o baton .
```

## Quick start

1. Check your environment:

   ```bash
   baton doctor
   ```

2. Run it inside any git repo:

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

## How it works

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
  version.go         Version metadata, --version flag, `baton version`
  doctor.go          Environment validation
internal/
  pty/               PTY wrapper (creack/pty)
  vt/                Virtual terminal bridge (x/vt SafeEmulator)
  git/               Worktree CRUD, diff, merge
  agent/             Agent lifecycle (PTY + VT + git composed)
  tui/               Bubble Tea views (dashboard, focus, diff, prompt, merge)
```

## Status

Pre-1.0. Expect breaking changes between minor versions. See [CHANGELOG.md](./CHANGELOG.md).

## Platform support

- macOS (Apple Silicon, Intel)
- Linux (arm64, amd64)

Windows and FreeBSD are not supported.

## Troubleshooting

**`baton doctor` reports `claude: not found`** — install the [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) and make sure `claude` is on your `PATH`.

**`baton` exits with "not a git repository"** — Baton must run inside a git working tree. Run `git init` first or `cd` into an existing repo.

**"worktree already exists"** — a previous `.baton/worktrees/<name>` is still on disk. Remove the stale entry with `git worktree remove .baton/worktrees/<name>` (add `--force` if the worktree is dirty).

## Contributing

Bug reports, fixes, and small features are welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md).

Security issues: see [SECURITY.md](./SECURITY.md) — please do not open a public issue.

## License

[MIT](./LICENSE)
