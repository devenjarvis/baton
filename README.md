# Baton

A terminal-native tool for orchestrating multiple [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agents in parallel. Each agent runs in an isolated git worktree with its own PTY, rendered via a virtual terminal emulator. Monitor agents from a single dashboard, interact in focus mode, review diffs, open PRs, fix failing CI checks, and merge results — all without leaving the terminal.

No tmux dependency.

> ⚠️ **Alpha software — here be dragons.** Baton is on its very first public release (v0.1.0). APIs, on-disk state layout, config schema, and keybindings may change without notice between versions. The TUI, hook pipeline, and session-resume layers are still stabilizing; expect rough edges and the occasional crash. Git operations are conservative — Baton only writes to `baton/*` branches inside `.baton/worktrees/` and uses `git merge --no-ff` with explicit confirmation — but please keep your work committed and file issues when things break.

## Requirements

- Go 1.25+
- Git 2.20+
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH, with `--settings` support for hook integration)
- Optional: `gh` CLI or `GITHUB_TOKEN` for PR creation, checks polling, and the "fix failing checks" flow

Verify your environment with:

```bash
baton doctor
```

`doctor` validates git, Claude Code, the baton binary, hook-pipeline round-trip, the current directory is a git repo, and GitHub auth.

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

The first run auto-registers the current directory as a repo and adds `.baton/` to `.gitignore`. Additional repos can be added from the TUI (`a`).

### Keybindings

**Dashboard** (list focused):

| Key       | Action                                                     |
|-----------|------------------------------------------------------------|
| `j` / `k` | Navigate repos, sessions, and agents                       |
| `⏎` / `→` | Focus terminal preview (on agent) or repo config (on repo) |
| `n`       | Create a new session                                       |
| `c`       | Add another agent to the selected session                  |
| `t`       | Open or focus a shell in the selected session's worktree   |
| `e`       | Open selected session's worktree in the configured IDE     |
| `o`       | Create session on an existing branch or PR                 |
| `a`       | Add a repo (file browser)                                  |
| `s`       | Global settings                                            |
| `d`       | Diff the session's worktree, or remove selected repo       |
| `x`       | Kill the selected agent                                    |
| `X`       | Kill the selected agent's entire session                   |
| `f`       | Fix failing CI checks on the session's PR                  |
| `q`       | Detach and exit (prompts if agents are running)            |

**Focus mode** (terminal preview focused):

| Key              | Action                                        |
|------------------|-----------------------------------------------|
| `ctrl+e` / `esc` | Return to list                                |
| `shift+esc`      | Send ESC to the agent (e.g. Claude interrupt) |
| `pgup` / `pgdn`  | Scroll backward / forward                     |
| `home`           | Jump back to live output                      |
| *drag*           | Native terminal text selection                |
| *other keys*     | Forwarded to the agent                        |

**Diff summary:**

| Key       | Action             |
|-----------|--------------------|
| `j` / `k` | Navigate files     |
| `⏎`       | Open file detail   |
| `g` / `G` | Top / bottom       |
| `q`       | Back to dashboard  |

**Diff detail:**

| Key       | Action              |
|-----------|---------------------|
| `j` / `k` | Scroll              |
| `d` / `u` | Page down / up      |
| `n` / `p` | Next / prev file    |
| `esc`     | Back to summary     |
| `q`       | Back to dashboard   |

Click support on the dashboard: click a row in the list to select it; click in the preview panel to enter focus mode.

### Branch naming

New sessions start on a random adjective-noun branch (e.g. `baton/warm-ibis`) so Claude can launch immediately. On the first real `user-prompt-submit`, the branch is renamed in place — `git branch -m` atomically updates the worktree's HEAD symref — to a slug of the prompt, e.g. `baton/add-dark-mode-to-dashboard`. Slash commands (`/clear`, `/help`) are skipped, so the next real prompt still triggers the rename. Sessions started on an existing branch (`o`) keep that branch as-is.

The prefix is configurable via `BranchPrefix` in global or per-repo settings, and supports two template variables:

- `{user}` — slugified `git config user.name` (falls back to `$USER`)
- `{date}` — today's date in `YYYY-MM-DD`

Unknown `{tokens}` are left literal. Example: `BranchPrefix: "{user}/"` produces `dj/add-dark-mode` after the first-prompt rename.

## How It Works

When you create a session, Baton:

1. Creates an isolated git worktree at `.baton/worktrees/<name>` on branch `baton/<name>`.
2. Writes a settings file wiring Claude Code's hooks (`session-start`, `stop`, `notification`, `user-prompt-submit`, `session-end`) to `baton hook <event>` and points Claude at it with `claude --settings`.
3. Spawns `claude "<task>"` in a PTY inside the worktree.
4. Feeds PTY output through a virtual terminal emulator ([charmbracelet/x/vt](https://github.com/charmbracelet/x/vt)) and renders it in the dashboard via [Bubble Tea v2](https://github.com/charmbracelet/bubbletea).
5. Listens on a per-process unix socket for hook events so the TUI can distinguish idle / active / waiting / done states without screen-scraping.

When you merge, Baton runs `git merge --no-ff` from the worktree branch into the session's base branch and cleans up the worktree.

## Architecture

```
main.go              Entry point
cmd/
  root.go            Cobra root, launches TUI
  doctor.go          Environment validation (git, claude, hook round-trip)
  hook.go            Forwards Claude hook payloads to the running baton over a unix socket
internal/
  pty/               Raw PTY wrapper (creack/pty)
  vt/                Virtual terminal bridge (x/vt SafeEmulator + io.Pipe)
  git/               Worktree CRUD, diff, merge via exec.Command("git", ...)
  agent/             Agent + Session + Manager (composes PTY + VT + git, runs read/write/status loops)
  tui/               Bubble Tea v2 views (dashboard, diff, repo/global config, file/branch pickers)
  hook/              Unix-socket server + client for Claude Code hook events
  github/            GitHub API wrapper for PRs, checks, review status
  config/            Global and per-repo settings (JSON on disk, resolved at runtime)
  state/             Session persistence across baton restarts
  editor/            IDE launcher helpers (macOS app probing, quote-aware tokenizer)
  audio/             Optional chimes for status transitions
  e2e/               End-to-end TUI tests (behind the `e2e` build tag)
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Bug reports and focused PRs are welcome; because Baton is a single-maintainer alpha, larger feature proposals should start as an issue.

## Security

See [SECURITY.md](./SECURITY.md) for how to report vulnerabilities.

## License

MIT — see [LICENSE](./LICENSE).
