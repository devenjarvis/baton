### Fixed

- PR tracking is now robust across branch renames, closed/merged PRs, and transient GitHub API failures. The session PR panel now appears reliably within ~2s after a push, even when the PR was opened while the smart-branch-name Haiku rename was still in flight.

### Changed

- GitHub PR lookups now resolve by commit SHA first (with branch-name fallback), so a `git branch -m` or async rename no longer hides an open PR.
- The GitHub client retries transient 5xx and rate-limit responses (max 2 retries with server-honored `Retry-After`); transient failures no longer blank the PR panel.
- The PR poller arms a 60s burst of 2s-interval polls after any branch rename or remote-SHA change — force-pushes and new commits on branches with an open PR now refresh promptly instead of waiting the full 30s adaptive interval.
- Closed or merged PRs are cleared from the panel on the next successful poll; they previously lingered forever because of a cache-update gate.
- The PR checks panel in the left sidebar now reserves a minimum height even when the agent list is long, truncating the list with a "+N more" marker so the panel remains visible.
