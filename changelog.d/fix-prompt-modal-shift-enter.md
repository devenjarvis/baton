### Fixed

- Shift+enter now inserts a newline in the new-session prompt modal, matching the footer hint. Bubbles' `InsertNewline` binding only listed `enter`/`ctrl+m`, and the modal intercepts plain `enter` as submit, so shift+enter previously fell through to the textarea's text-input path. The textarea's `InsertNewline` binding is extended in `newPromptModal` to also accept `shift+enter`.
