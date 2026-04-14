package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// mergeModel shows a merge confirmation overlay.
type mergeModel struct {
	agentName  string
	branchName string
	baseBranch string
	width      int
	height     int
	errMsg     string
	viaPR      bool // merge via GitHub API instead of local git
	prNumber   int  // GitHub PR number when viaPR is true
}

func newMergeModel(agentName, branchName, baseBranch string) mergeModel {
	return mergeModel{
		agentName:  agentName,
		branchName: branchName,
		baseBranch: baseBranch,
	}
}

type (
	mergeConfirmMsg  struct{}
	mergeCancelMsg   struct{}
	mergeCompleteMsg struct{ err error }
)

func (m mergeModel) Update(msg tea.Msg) (mergeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y":
			return m, func() tea.Msg { return mergeConfirmMsg{} }
		case "n", "esc":
			return m, func() tea.Msg { return mergeCancelMsg{} }
		}
	}
	return m, nil
}

func (m mergeModel) View() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(1, 2).
		Width(50)

	title := StyleWarning.Render("Merge Confirmation")

	var question string
	if m.viaPR {
		question = fmt.Sprintf("Merge PR %s via GitHub?",
			StyleActive.Render(fmt.Sprintf("#%d", m.prNumber)))
	} else {
		question = "Merge " + StyleActive.Render(m.branchName) +
			" into " + StyleActive.Render(m.baseBranch) + "?"
	}

	hint := StyleSubtle.Render("y: confirm  n/esc: cancel")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		question, "",
	)

	if m.errMsg != "" {
		content += StyleError.Render("Error: "+m.errMsg) + "\n\n"
	}

	content += hint

	box := boxStyle.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
