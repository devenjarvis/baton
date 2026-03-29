package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

type App struct {
	width  int
	height int
}

func NewApp() App {
	return App{}
}

func (a App) Init() tea.Cmd {
	return nil
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a App) View() tea.View {
	title := StyleTitle.Render("Welcome to Baton")
	subtitle := StyleSubtle.Render("Terminal-native orchestration for Claude Code agents")
	hint := StyleSubtle.Render("Press q to quit")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", subtitle, "", hint)

	v := tea.NewView(lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, content))
	v.AltScreen = true
	return v
}
