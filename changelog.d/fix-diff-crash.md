### Fixed

- Diff view no longer crashes with "fragment header miscounts lines: -1 old, -1 new" when a hunk ends with an empty context line. `git.Diff` now uses `runGitRaw` instead of `runGit` so trailing whitespace (space-prefixed empty lines) is preserved for go-gitdiff's line-count validation.
