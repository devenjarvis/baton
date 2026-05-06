### Changed

- Focus mode session list now shows 3-line cards: name + status badge on line 1, task summary (Haiku-generated) on line 2, waiting reason / idle time + elapsed on line 3
- Sessions with errors or waiting agents sort to the top of the SESSIONS list; active sessions sort above idle
- Review queue entries now show 2-line cards: name + PR indicator on line 1, task summary + age on line 2
- Task summaries are generated asynchronously by Haiku on the first actionable prompt and update in place without blocking
