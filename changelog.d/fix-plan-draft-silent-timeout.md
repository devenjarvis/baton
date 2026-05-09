### Fixed

- Plan drafting and revising no longer fail silently with `claude planner: signal: killed (stderr="")` after ~60s. The wall-clock `PlanDraftTimeout` that wrapped the Sonnet subprocess has been removed: drafting can legitimately take a couple of minutes on complex prompts, and the user is actively waiting for the editor — there is no other concurrent work the budget was protecting. Cancellation is still wired up via `KillSession`, manager shutdown, and explicit `CancelDraft` / `CancelRevise`, so the user remains in control of when to bail.
