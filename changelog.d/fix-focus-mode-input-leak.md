### Fixed

- Focus mode shortcuts (j/k, m, r) now work correctly after entering focus mode from a non-list panel state (e.g. review panel); `panelFocus` is reset to `focusList` on entry
- Pressing `d` in focus mode no longer falls through to the repo-delete handler
