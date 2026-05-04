### Fixed

- Pipeline cards no longer overflow terminal width at 80 columns; width formula corrected and minimum cell width raised to fit all labels.

### Changed

- Pressing `N` in focus mode now updates both the header label and the pipeline counts to reflect only the active repo's sessions.
- Focus header shows a compact per-repo agent summary (e.g. `my-app(2⚡) | backend(1⏸)`) when multiple repos are configured.
- Attention rows now display `session-name › Track N` so the session context is visible alongside the agent name.
- Review queue rows now show the PR number and CI check status badge when a PR exists.
- Pressing `n` in focus mode with 2+ repos opens a repo picker overlay (`< my-app >`) before creating the session; single-repo setups are unchanged.
