### Fixed

- Text artifacts (e.g. `> Add`) no longer bleed below the agent preview panel. Three structural layout bugs were repaired: (1) the preview panel was 1 row too tall in `focusTerminal` mode when a PR row was visible, causing Bubble Tea's differential renderer to leave stale cells; (2) in `focusList` mode the preview container was 2 columns narrower than `focusTerminal`'s outer width, leaving the rightmost 2 columns unwritten between focus switches; (3) scrollback lines were not padded to the full viewport width before rendering, allowing short history lines to leave stale content.
