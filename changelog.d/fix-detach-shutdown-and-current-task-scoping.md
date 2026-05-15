### Fixed

`Manager.Detach()` is now idempotent and safe to follow with `Shutdown()` (or vice versa) — previously the second call panicked with "close of closed channel" because `Detach` closed `m.done` outside of `shutdownOnce`. The two teardown methods now share the same once-gate so a deferred `Shutdown()` after an explicit `Detach()` is a no-op rather than a crash.

The dashboard's "current task" indicator now scopes its scan to the `## Tasks` section the same way `planTaskCounts` and `ParsePlanTasks` do. A stray `- [ ]` in `## Spec` or `## Verification` no longer surfaces as the active task while the X/Y count ignores it — the two values are now derived from the same set of checkboxes.
