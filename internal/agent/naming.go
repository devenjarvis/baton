package agent

import "strings"

// maxPromptBytesForSuffix caps how much of the user's first prompt is fed
// into the slugifier. Slugs are then further truncated to 40 chars by slugify.
const maxPromptBytesForSuffix = 80

// suffixFromPrompt derives a branch-safe slug from the user's prompt text.
// Returns "" for empty input, slash-command-only input (e.g. "/clear"), or
// anything that slugifies to an empty string.
func suffixFromPrompt(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return ""
	}
	// Skip pure slash commands like "/clear" or "/help <args>". Claude's
	// UserPromptSubmit hook fires for those, but they don't describe work
	// worth renaming the branch for.
	if strings.HasPrefix(trimmed, "/") {
		return ""
	}
	if len(trimmed) > maxPromptBytesForSuffix {
		trimmed = trimmed[:maxPromptBytesForSuffix]
	}
	return slugify(trimmed)
}
