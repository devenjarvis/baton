### Changed

- Split the monolithic `updateDashboard` dispatcher (`internal/tui/update_keys.go`, 521 lines) into per-handler methods: `handlePipelineKeys` (pipeline navigation + cursor-action keys), `handlePipelineMarkReady` / `handlePipelineOpenReview` (m / r workflow paths), `handleQuitKey` (q / ctrl+c with confirm-quit gesture), `handleConfigFormSave`, `handleDashboardPaste`, `handleMouseClick` / `handleMouseMotion` / `handleMouseRelease` / `handleMouseWheel`. `updateDashboard` is now a 139-line router. No behavior change.
