### Added
- Pomodoro cycle tracking in focus mode: pressing `b` enters a fullscreen break screen with a live countdown timer and breathing animation; completing or skipping a break increments the 🍅 pomodoro counter shown in the work-mode header.
- Fullscreen break overlay (`b` key in focus mode): centered break screen with calming 12-frame expanding/contracting animation, countdown to break end, and soft double-press override to return early.
- Auto-exit from break mode when the configured break duration elapses, resetting the work timer and incrementing the cycle counter.
- `Break (min)` field in the global config form (`g` → Global Settings), defaulting to 15 minutes.
- Bubbles block-character progress bar replaces the ASCII `[===]` timer bar in focus mode work view.
- `[b] take a break` hint in the focus mode status bar; `⌛` hint now surfaces the keybinding when the session limit is exceeded.
- `"pomodoros_completed"` field in `wellness.log` entries, recording completed Pomodoro cycles per session.
