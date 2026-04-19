### Fixed

- Side-by-side diff no longer "line dances" on tab-indented code. Tabs are now expanded to 4 spaces before width measurement so `ansi.StringWidth` returns the correct display width. Side-by-side mode also switches from wrapping to truncation, so each diff row always occupies exactly one physical terminal line and the `│` separator stays stable while scrolling.
