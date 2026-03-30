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

// panelFocus tracks which dashboard panel has keyboard focus.
type panelFocus int

const (
	focusList     panelFocus = iota // sidebar: j/k navigate agents
	focusTerminal                    // preview: keys forwarded to agent
)

// isQuit checks if a key press is a quit key (only on dashboard).
func isQuit(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == "q" || s == "ctrl+c"
}
