### Fixed

- Focused preview panel no longer renders a doubled or misaligned host-terminal cursor. The host cursor used `previewColOffset = 32` instead of `31`, drawing it one column right of the agent's VT cursor; the host cursor was also unconditionally shown even after the inner program emitted DECRST 25 (`\e[?25l`), so full-screen TUIs like Claude Code drew an extra blinking block on top of their own cursor. `vt.Terminal` now tracks DECTCEM via the `CursorVisibility` callback and `App.View()` only places `tea.NewCursor` when the agent reports the cursor visible.
