### Changed

- Focus mode SESSIONS cards now distinguish idle-but-ready agents from actively-working ones: an all-idle session renders a mint `▎` stripe and a `✓ N ready for input` badge, while a mixed session shows `N active · M ready` with the ready count called out in mint
- This is separate from the existing purple `waiting` state (Claude blocked on a permission prompt) and the yellow `may need input` heuristic, preserving the urgency hierarchy
