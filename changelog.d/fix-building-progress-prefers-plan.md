### Fixed

- Building session progress bar now advances whenever the build agent toggles checkboxes in `.claude/plan.md`, even if Claude has issued a TodoWrite that subsequently goes stale. Plan checkboxes are the agent's authoritative task contract (mapped 1:1 to `[task N]` commits) and now take precedence over TodoWrite snapshots whenever a plan exists. Sessions without a plan continue to use TodoWrite-driven progress unchanged.
