### Changed

- Approving a plan no longer drops the user into the agent terminal — focus stays on the dashboard and the pipeline cursor moves to the new BUILDING row, so the developer can keep monitoring all sessions while the build agent runs.
- Building sessions now auto-advance to REVIEWING when all agents go idle, eliminating the manual `m` press for the common single-turn case. The `m` key remains as a fallback for sessions restored from disk whose agents had already idled before the current process started.
