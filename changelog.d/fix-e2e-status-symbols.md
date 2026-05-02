### Fixed

- Five e2e tests (`TestHookPipeline`, `TestSessionCreation`, `TestAgentAddition`, `TestAgentKill`, `TestSessionKill`) were failing because their helpers still looked for the pre-PR-#89 status glyphs (`◎ ● ◐ ○ ✓ ✗`) instead of the music-themed playback controls now produced by `agent.Status.Symbol()` (`▷ ▶ ⏺ ⏸ ⏭ ⏹`). The agent rows were rendered correctly all along; only the assertion side was stale. Updated `countAgentRows` and the `TestHookPipeline` symbol expectations to match the current symbols.
