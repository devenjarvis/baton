### Changed

- Review panel now renders a two-pane layout: a compact task list on the left and a full detail view on the right, so AI verdict rationales are never truncated
- Header collapses to a single title row plus at most two wrapped prompt lines (replaces the six-line accent block)
- Left pane shows one row per plan task with a verdict icon (⋯ pending, spinner running, ✓ pass, ! concerns, ✗ fail, ⊘ no diff) and `+X -Y` stats; clicking a row moves the cursor
- Right pane shows the full untruncated rationale, changed files with line stats, and commit subjects for the selected task
- No-plan sessions show an "Overview" row with aggregate file stats instead of the legacy "REVIEW SHAPE" bar chart
- All body colors now use the dashboard theme (StyleSuccess, StyleWarning, StyleError, ColorPrimary) instead of inline hex literals
- Narrow terminals (<80 cols) stack the list above the detail with a divider rather than going side-by-side
