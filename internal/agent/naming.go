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
	// For slash commands like "/plan-it add dark mode", use only the argument
	// text after the command. Pure commands with no arguments (e.g. "/clear",
	// "/help") have no meaningful work description, so return "".
	if strings.HasPrefix(trimmed, "/") {
		idx := strings.IndexAny(trimmed, " \t")
		if idx < 0 {
			return ""
		}
		trimmed = strings.TrimSpace(trimmed[idx+1:])
		if trimmed == "" {
			return ""
		}
	}
	if len(trimmed) > maxPromptBytesForSuffix {
		trimmed = trimmed[:maxPromptBytesForSuffix]
	}
	return slugify(trimmed)
}
