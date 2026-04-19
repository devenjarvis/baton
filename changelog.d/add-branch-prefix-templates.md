### Added

- `{user}` and `{date}` template variables in `BranchPrefix`. `{user}` resolves from `git config user.name` (falling back to `$USER`), slugified; `{date}` resolves to today's `YYYY-MM-DD`. Unknown `{tokens}` are left literal so existing prefixes with braces are unaffected. Example: `BranchPrefix: "{user}/"` produces `dj/add-dark-mode` for a first-prompt rename.
