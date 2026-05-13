### Fixed

- Building session cards now reflect checkbox toggles made directly by the build agent in `.claude/plan.md` (via Claude's Edit tool). `CachedPlan` is keyed on the file's mtime and re-reads the file whenever the mtime changes, so the progress bar advances within one 100ms tick instead of staying frozen until `WritePlan` was called.

### Changed

- Building session cards have a new layout: a real progress bar (colored `ColorSuccess` at 100%, `ColorPrimary` otherwise) with a muted `done/total` count now appears on line 1's right side instead of the plain `▸ N/M` text badge.
- Line 1 leads with a single status glyph that mirrors the stripe color: `✗` error, `⏸` waiting, `?` idle-asking, `✓` reviewable/finished, `⚡` active, `○` otherwise.
- Lines 2–3 show the active task with a `▸` bold prefix and the next pending task with a `↳ next:` muted-italic prefix, sourced from the session's in-progress TodoItem (falling back to the first uncompleted plan checkbox when no todos are active).
- Line 4 renders the branch as a dark-background chip with a `🌿` prefix and elapsed time with a `⏱` prefix.
- All cards are now a uniform 4 lines; the previous 5-line variant for plan-backed Building sessions has been removed.
