package tui

import tea "charm.land/bubbletea/v2"

// ViewMode represents the current TUI view.
type ViewMode int

const (
	ViewDashboard ViewMode = iota
	ViewDiff
	ViewPrompt  // overlay
	ViewMerge   // overlay
)

// isQuit checks if a key press is a quit key (only on dashboard).
func isQuit(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == "q" || s == "ctrl+c"
}
