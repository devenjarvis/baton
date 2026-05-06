### Fixed

- In focus mode's fullscreen agent view (`focusLaunch`), the `m` and `r` keys are no longer intercepted as focus-pipeline shortcuts. Single-letter focus-mode bindings were ripping the user out of their Claude session whenever they typed those characters; all keystrokes other than explicit exit chords (`esc`, `ctrl+e`, `shift+esc`, `ctrl+t/n/w`, `alt+]/[`, scrollback) now forward to the agent terminal.
