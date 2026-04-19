### Fixed

- GoReleaser now publishes the Homebrew formula into the tap's `Formula/` directory (via `directory: Formula`). v0.1.0 landed `baton.rb` at the tap repo root, where newer Homebrew versions don't discover it — installs with `brew install devenjarvis/tap/baton` would fail with "No available formula."
