package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/agent"

	xvt "github.com/charmbracelet/x/vt"
)

// focusModel handles full-screen interactive terminal mode.
type focusModel struct {
	agent  *agent.Agent
	width  int
	height int
}

type focusExitMsg struct{}

func newFocusModel(a *agent.Agent) focusModel {
	return focusModel{agent: a}
}

func (f focusModel) Update(msg tea.Msg) (focusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// ctrl+b always returns to dashboard — never forwarded.
		if msg.String() == "ctrl+b" {
			return f, func() tea.Msg { return focusExitMsg{} }
		}

		// Forward all other keys to the agent.
		if f.agent != nil {
			f.agent.SendKey(xvt.KeyPressEvent(msg))
		}
	}
	return f, nil
}

func (f focusModel) View() string {
	if f.agent == nil {
		return "No agent"
	}
	return f.agent.Render()
}
