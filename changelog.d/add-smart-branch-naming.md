### Added

- Branch renaming now uses `claude -p --model claude-haiku-4-5` to summarize the first prompt into a 3-5 word slug; falls back to the old slugify on failure. Toggle via `smart_branch_names`. Session display name now updates to match the Haiku-generated slug after the async rename completes.
