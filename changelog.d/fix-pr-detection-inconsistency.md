### Fixed

- PR indicator no longer blanks on a single nil poll result during branch-rename or rapid force-push windows; cache is now evicted only after 2 consecutive nil polls.
- After a Haiku branch rename, PR tracking continues by falling back to the local worktree HEAD SHA when the remote branch hasn't been pushed under the new name yet.
