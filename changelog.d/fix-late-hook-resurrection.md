### Fixed

- Late `Stop` and `SessionStart` hook events arriving after a Claude process exits no longer resurrect `Done`/`Error` agents back to `Idle`/`Active`. This eliminates spurious status indicators and potential phantom chimes caused by the race between the PTY close path and in-flight unix-socket hook events.
