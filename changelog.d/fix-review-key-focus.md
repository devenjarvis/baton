### Fixed

- Pressing `r` in focus mode now opens the review panel as expected. The review-panel branch of `App.View()` returned a view without `AltScreen=true`, causing Bubble Tea to drop out of the alternate screen each frame so the panel never became visible — making `r` look like it did nothing.
