### Fixed

- Mouse-wheel scrolling now works inside Claude Code's `/tui fullscreen` mode. When the focused agent is in alt-screen, wheel events are forwarded to the agent (so Claude's internal scrollback handles them) instead of being consumed by baton's own scrollback buffer, which is inert for alt-screen apps. Exiting fullscreen restores baton's scrollback. Entering fullscreen while scrolled back snaps the preview back to live.
