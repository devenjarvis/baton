### Fixed

- Restore auto-naming of sessions and agents from the first Claude prompt (was broken in 0.1.0 hook refactor). The dashboard now relabels a fresh agent's random `adjective-noun` placeholder to a slugified version of the user's first prompt as soon as the `UserPromptSubmit` hook fires; sessions that already have a display name (branch-derived or restored from state) are preserved.
