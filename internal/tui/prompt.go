package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// promptModel handles the new agent creation dialog.
type promptModel struct {
	name     string
	task     string
	focused  int // 0 = name, 1 = task
	width    int
	height   int
}

// promptResult is sent when the user submits the prompt.
type promptResult struct {
	name string
	task string
}

func newPromptModel() promptModel {
	return promptModel{}
}

func (p promptModel) Update(msg tea.Msg) (promptModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return p, func() tea.Msg { return promptCancelMsg{} }
		case "tab", "shift+tab":
			p.focused = (p.focused + 1) % 2
		case "enter":
			name := strings.TrimSpace(p.name)
			task := strings.TrimSpace(p.task)
			if name != "" && task != "" {
				return p, func() tea.Msg {
					return promptResult{name: name, task: task}
				}
			}
		case "backspace":
			if p.focused == 0 && len(p.name) > 0 {
				p.name = p.name[:len(p.name)-1]
			} else if p.focused == 1 && len(p.task) > 0 {
				p.task = p.task[:len(p.task)-1]
			}
		default:
			if msg.Text != "" {
				if p.focused == 0 {
					p.name += msg.Text
				} else {
					p.task += msg.Text
				}
			}
		}
	}
	return p, nil
}

type promptCancelMsg struct{}

func (p promptModel) View() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(60)

	title := StyleTitle.Render("New Agent")

	nameLabel := "Name: "
	taskLabel := "Task: "

	nameStyle := lipgloss.NewStyle()
	taskStyle := lipgloss.NewStyle()

	if p.focused == 0 {
		nameStyle = nameStyle.Foreground(ColorSecondary)
	} else {
		taskStyle = taskStyle.Foreground(ColorSecondary)
	}

	cursor := "▎"
	nameField := nameStyle.Render(nameLabel + p.name)
	taskField := taskStyle.Render(taskLabel + p.task)
	if p.focused == 0 {
		nameField = nameStyle.Render(nameLabel + p.name + cursor)
	} else {
		taskField = taskStyle.Render(taskLabel + p.task + cursor)
	}

	hint := StyleSubtle.Render("tab: switch field  enter: create  esc: cancel")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		nameField, "",
		taskField, "",
		hint,
	)

	box := boxStyle.Render(content)
	return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, box)
}
