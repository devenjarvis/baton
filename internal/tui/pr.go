package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/github"
)

// prPollMsg carries the result of an async PR status poll.
type prPollMsg struct {
	sessionID string
	pr        *github.PRState
	checks    *github.CheckStatus
}

// prCreateMsg carries the result of async PR creation.
type prCreateMsg struct {
	sessionID string
	pr        *github.PRState
	err       error
}

// fixChecksMsg carries the result of dispatching fix-checks to an agent.
type fixChecksMsg struct {
	sessionID string
	err       error
}

// prCacheEntry holds cached PR and check status for a session.
type prCacheEntry struct {
	pr     *github.PRState
	checks *github.CheckStatus
}

// prIndicator returns a compact colored string for the session row.
// Returns empty string if no PR data exists.
func prIndicator(pr *github.PRState, checks *github.CheckStatus) string {
	if pr == nil {
		return ""
	}

	prNum := fmt.Sprintf("#%d", pr.Number)

	if checks == nil {
		return prNum
	}

	var checkSymbol string
	var checkStyle lipgloss.Style
	switch checks.State {
	case "success":
		checkSymbol = "\u2713" // checkmark
		checkStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	case "failure":
		checkSymbol = "\u2717" // x mark
		checkStyle = lipgloss.NewStyle().Foreground(ColorError)
	case "pending":
		checkSymbol = "\u25cb" // circle
		checkStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	default:
		checkSymbol = "?"
		checkStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	}

	return prNum + " " + checkStyle.Render(checkSymbol)
}
