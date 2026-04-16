# Contributing to Baton

Baton is an early, single-maintainer project. Focused bug reports, fixes, and small features are the easiest kind of contribution to review.

## Filing issues

Search existing issues (open and closed) before opening a new one.

- **Bugs** — use the "Bug report" form. Include `baton doctor` output, steps to reproduce, and expected vs. actual behavior.
- **Feature requests** — use the "Feature request" form. Describe the underlying problem, not just the proposal.
- **Security vulnerabilities** — do not file a public issue. See [SECURITY.md](./SECURITY.md).

## Development setup

Requirements:

- Go 1.25+
- Git 2.20+
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) (`claude` on PATH) — runtime-only

```bash
git clone https://github.com/devenjarvis/baton.git
cd baton
go build -o baton .
./baton doctor
```

## Build, test, lint

```bash
go build -o baton .         # build
go test ./...               # run all tests
go test -race ./...         # run with race detector
go vet ./...                # static analysis
golangci-lint run           # lint (config in .golangci.yml)
gofumpt -w .                # format all Go files
```

Always run `go test -race ./...` before opening a PR. Baton runs several goroutines per agent (PTY/VT/git) and the race detector has caught real bugs here.

End-to-end TUI tests live in `internal/e2e/` and require the `tu` CLI v0.6.0+:

```bash
go test -tags e2e -timeout 300s -v ./internal/e2e/
```

## PR workflow

1. Fork the repo and branch from `main`.
2. Keep PRs focused — one concern per PR.
3. Add or update tests. Bug fixes should include a regression test where practical.
4. Update `CHANGELOG.md` under `[Unreleased]`.
5. Run locally before pushing: `go test -race ./... && go vet ./... && golangci-lint run && gofumpt -l .` (the last should print nothing).
6. Open a PR. The template prompts for summary and test plan.

## Commit messages

Match the existing style (see `git log`):

- `feat: <what>` — new user-facing functionality
- `fix: <what>` — bug fix
- `test: <what>` — test-only changes
- `refactor: <what>` — no behavior change
- `docs: <what>` — documentation
- `chore: <what>` — tooling, dependencies, release plumbing

One-liners are fine. A body is welcome when the *why* isn't obvious from the diff.

## Architecture

Read the Architecture section in [README.md](./README.md) and the key patterns in [`CLAUDE.md`](./CLAUDE.md) before making non-trivial changes.
