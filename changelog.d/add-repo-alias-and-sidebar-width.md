### Added

- Per-repo `Alias` override so long repo names (e.g. ones sharing a long common prefix) can be rendered as a short nickname in the dashboard sidebar. Set it from the repo config form ("Alias"); leave blank to keep using the registered name.
- Global `SidebarWidth` setting (default 30, clamped to 20..60) to widen or narrow the dashboard's left panel. Set it from the global config form ("Sidebar Width").

### Fixed

- Dashboard repo/session/agent name truncation now uses display-width (ANSI/UTF-8 aware) instead of byte length, so multi-byte names no longer get cut mid-rune.
