### Fixed

- Stale-text artifacts overlaying the agent preview (most visible on Claude plan-approval prompts and between normal Q&A turns). Fixed by padding every VT render line to the full viewport width with a terminating style reset (`RenderPadded`) so previous-frame trailing cells are overwritten, invalidating the `StableRender` cache on resize and alt-screen entry, taking atomic scrollback+viewport snapshots under a single lock, and aligning the preview box's inner height to the VT dimensions.
