### Changed

- `x` (kill agent) and `X` (kill session) now tear down off the UI thread and mark the affected row as `closing…` until the worktree teardown completes, so the dashboard stays interactive while Claude exits and `git worktree remove --force` runs.
