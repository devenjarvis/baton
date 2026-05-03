### Fixed

- Focus mode: pressing `n` to create a new agent now opens a "focus paused" terminal so you can give the initial prompt, then press `f` to return to the pipeline view.
- Focus mode: attention rows (waiting agents) now show the notification reason text (e.g. "Claude needs your permission to use Bash") and can be navigated with `j`/`k`. Press `space`/`enter` to open that agent's terminal.
- Focus mode: header now shows the active repo name. Press `N` to cycle through repos without leaving focus mode.
- Focus mode: status bar shows context-sensitive hints (`[f/esc] back to focus`) when a launch terminal is open.
