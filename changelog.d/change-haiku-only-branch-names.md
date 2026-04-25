### Changed

- Branch and session naming now always go through Haiku — the legacy slugify-from-prompt path and the `smart_branch_names` toggle have been removed. When Haiku errors, times out, or returns nothing, the session keeps its random adjective-noun branch and the next prompt retries.
- The instruction sent to Haiku is configurable via the new `branch_name_prompt` key in global or per-repo config (JSON only). The string is treated as a template: any occurrence of `{prompt}` is replaced with the user's prompt. Templates that omit the placeholder still get the prompt appended.
