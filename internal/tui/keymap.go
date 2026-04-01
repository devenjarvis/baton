package tui

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
