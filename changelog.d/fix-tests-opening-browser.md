### Fixed

- `internal/tui` tests no longer launch a real browser. `openURL` is now a package-level `var` so PR-click and Shipping-activate tests can swap in a no-op override for the duration of the test.
