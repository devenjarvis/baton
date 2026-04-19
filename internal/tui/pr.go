package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/github"
)

// prPollMsg carries the result of an async PR status poll.
type prPollMsg struct {
	sessionID string
	pr        *github.PRState
	checks    *github.CheckStatus
	reviews   *github.ReviewStatus
}

// prCacheEntry holds cached PR and check status for a session.
type prCacheEntry struct {
	pr      *github.PRState
	checks  *github.CheckStatus
	reviews *github.ReviewStatus
}

// prSessionState tracks per-session polling state for adaptive PR polling.
type prSessionState struct {
	lastPoll       time.Time
	lastSHACheck   time.Time
	lastCheckState string // "success", "failure", "pending", ""
	lastRemoteSHA  string
	flashUntil     time.Time
	flashColor     string // "success" or "error"
	inFlight       bool
}

// isMergeReady returns true when all conditions for merge readiness are met.
func isMergeReady(entry *prCacheEntry) bool {
	if entry == nil || entry.pr == nil {
		return false
	}
	// Require at least one check to prevent premature "Ready" display while CI
	// is still initializing (API may briefly return zero check runs).
	checksOK := entry.checks != nil && entry.checks.State == "success" && entry.checks.Total > 0
	reviewsOK := entry.reviews != nil && entry.reviews.State == "approved"
	mergeable := entry.pr.Mergeable
	return checksOK && reviewsOK && mergeable
}

// prIndicator returns a compact colored string for the session row.
// Returns empty string if no PR data exists.
func prIndicator(entry *prCacheEntry) string {
	if entry == nil || entry.pr == nil {
		return ""
	}
	pr := entry.pr
	checks := entry.checks

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

	result := prNum + " " + checkStyle.Render(checkSymbol)
	if isMergeReady(entry) {
		result += " " + lipgloss.NewStyle().Foreground(ColorSuccess).Render("Ready")
	}
	return result
}

// formatCheckDuration formats a duration for check run display.
func formatCheckDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}
