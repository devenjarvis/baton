### Changed

- Reverted "Release mouse capture in focus terminal view": the focus terminal view now re-captures the mouse. Text selection via the host terminal's drag was unreliable (copied text included the surrounding TUI frame), and capturing the mouse restores consistent keybinding/scroll behavior in focus mode.
