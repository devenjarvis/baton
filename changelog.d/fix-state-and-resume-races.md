### Fixed

- Serialize `state.Save`/`Load`/`Remove` with a package-level mutex so concurrent writers can't race on the final `os.Rename` and silently roll back session-recovery data.
- `ResumeSession` no longer drops failures on the floor: when an agent fails to spawn, the manager now emits `EventError` so the TUI can surface the failure (the on-disk worktree is still preserved in case it holds uncommitted user work).
- Closed a TOCTOU race in session creation: `createSessionWorktree` and `createSessionOnBranchWorktree` now reserve the chosen name in a `pendingNames` set under `Manager.mu`, so two concurrent `CreateSession` calls can't pick the same slug and collide on the same worktree path.
- `applyHookEnv` now returns an error (instead of silently writing a useless env) when an agent ID is missing while hooks are otherwise wired up — preventing zombie agents that the hook server can't route events back to. Adds direct unit tests for `applyHookEnv`, `buildHookArgs`, `agentProgram`, and `supportsHooks` (previously only exercised via integration tests).
- Config forms now respond to the spacebar: Bubble Tea v2's `KeyPressMsg.String()` returns `"space"`, so the form handler now matches `"space"` alongside `" "` and `"enter"` for toggling and select cycling.
