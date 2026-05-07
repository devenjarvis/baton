### Fixed

- Focus-mode review panel no longer orphans sessions. A session whose phase has progressed to `LifecycleInReview` now stays visible in REVIEW QUEUE (with a subtle `(reviewing)` tag), so pressing `ESC` after `p` with no PR cached can no longer leave the session unreachable.

### Added

- Two new keys in the focus-mode review panel:
  - `t` — open the session's most-active agent in the fullscreen terminal (useful for sessions with no PR yet, e.g. running `gh pr create` manually, or for inspecting individual agents in a multi-agent session via the tab bar).
  - `c` — mark the session complete without opening a PR, for design-doc / exploratory branches.
- Footer hint in the review panel now advertises all available actions (`p / t / c / e / d / ESC`).
