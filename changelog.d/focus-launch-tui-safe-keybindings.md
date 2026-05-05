### Changed

- Focus-launch key bindings now use terminal-safe `alt+`/`ctrl+` modifiers instead of `super+` (cmd) keys that macOS intercepts: `alt+[`/`alt+]` switch tabs, `ctrl+t` opens a shell, `ctrl+n` spawns a new agent.

### Added

- `ctrl+w` closes the current focus-launch tab: switches to an adjacent tab and kills the agent asynchronously; on the last tab, returns to the dashboard.
