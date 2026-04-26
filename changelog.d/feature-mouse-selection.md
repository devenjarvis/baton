### Added

- Drag to select text in the focused agent panel; releases copy to the clipboard via OSC 52. Selection is performed in VT-cell coordinates so the lipgloss border can never appear inside a multi-line span. The highlight persists after release (matching iTerm2/Alacritty) and clears when you switch agents, lose terminal focus, click without dragging, or resize the window.

### Fixed

- Mouse-wheel coordinate translation no longer drifts by one row when the selected session has an open PR (the metadata-row count is computed from the dashboard rather than hardcoded).
