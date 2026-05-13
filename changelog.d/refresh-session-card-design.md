### Changed

- Building session cards: branch is now rendered as a muted `⎇ branch` inline label (no background chip), placing it in the same muted-metadata tier as elapsed and idle tokens; branch and detail join with ` · ` when both are present
- Progress bar suffix now reads `N/M tasks` instead of `N/M`, matching the Planning badge's wording
- Active task line no longer has a leading `▸ ` glyph; next-task line changes from `↳ next: …` to `next: …`; typographic hierarchy (bold vs muted italic) carries the visual distinction
- Active-state status glyph changes from `⚡` to `●` (calm filled circle); attention-demanding glyphs (`⏸`, `?`, `✗`) are now reserved for states that require action
