package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
)

// dashboardModel shows the agent list and terminal preview.
type dashboardModel struct {
	agents   []*agent.Agent
	selected int
	width    int
	height   int
}

func newDashboardModel() dashboardModel {
	return dashboardModel{}
}

func (d dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if d.selected < len(d.agents)-1 {
				d.selected++
			}
		case "k", "up":
			if d.selected > 0 {
				d.selected--
			}
		}
	}
	return d, nil
}

func (d dashboardModel) View() string {
	if len(d.agents) == 0 {
		return d.emptyView()
	}

	listWidth := 30
	previewWidth := d.previewTermWidth()

	// Agent list panel.
	list := d.renderAgentList(listWidth)

	// Preview panel.
	preview := d.renderPreview(previewWidth)

	listStyle := lipgloss.NewStyle().
		Width(listWidth).
		Height(d.contentHeight()).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(ColorMuted)

	previewStyle := lipgloss.NewStyle().
		Width(previewWidth).
		Height(d.contentHeight())

	return lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(list),
		previewStyle.Render(preview),
	)
}

func (d dashboardModel) contentHeight() int {
	return d.height - 2 // statusbar + title
}

// previewTermWidth returns the terminal column count for the preview panel.
func (d dashboardModel) previewTermWidth() int {
	listWidth := 30
	return d.width - listWidth - 1 // 1 for the list panel's right border
}

// previewTermHeight returns the terminal row count for the preview panel,
// accounting for the title, task info, and blank line rendered above the terminal.
func (d dashboardModel) previewTermHeight() int {
	return d.contentHeight() - 3 // title + task info + blank line
}

func (d dashboardModel) emptyView() string {
	title := StyleTitle.Render("Baton")
	subtitle := StyleSubtle.Render("No agents running")
	hint := StyleSubtle.Render("Press n to create a new agent")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", subtitle, "", hint)
	return lipgloss.Place(d.width, d.contentHeight(), lipgloss.Center, lipgloss.Center, content)
}

func (d dashboardModel) renderAgentList(width int) string {
	title := StyleTitle.Render("AGENTS")
	separator := StyleSubtle.Render(strings.Repeat("─", width-2))

	var items []string
	items = append(items, title, separator)

	for i, a := range d.agents {
		status := a.Status()
		symbol := status.Symbol()
		elapsed := humanizeElapsed(a.Elapsed())

		var style lipgloss.Style
		switch status {
		case agent.StatusActive:
			style = lipgloss.NewStyle().Foreground(ColorSecondary)
		case agent.StatusDone:
			style = lipgloss.NewStyle().Foreground(ColorSuccess)
		case agent.StatusError:
			style = lipgloss.NewStyle().Foreground(ColorError)
		case agent.StatusIdle:
			style = lipgloss.NewStyle().Foreground(ColorMuted)
		default:
			style = lipgloss.NewStyle().Foreground(ColorWarning)
		}

		prefix := "  "
		if i == d.selected {
			prefix = StyleActive.Render("▸ ")
		}

		nameWidth := width - 14 // space for symbol, elapsed, padding
		name := a.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		line := fmt.Sprintf("%s%s %-*s %5s",
			prefix,
			style.Render(symbol),
			nameWidth,
			name,
			elapsed,
		)
		items = append(items, line)
	}

	return strings.Join(items, "\n")
}

func (d dashboardModel) renderPreview(width int) string {
	if d.selected >= len(d.agents) || len(d.agents) == 0 {
		return lipgloss.Place(width, d.contentHeight(), lipgloss.Center, lipgloss.Center,
			StyleSubtle.Render("No agent selected"))
	}

	a := d.agents[d.selected]
	title := StyleTitle.Render(" " + a.Name + " ")
	taskInfo := StyleSubtle.Render(" Task: " + a.Task)

	// Render the agent's terminal output.
	render := a.Render()

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		taskInfo,
		"",
		render,
	)
}

func (d dashboardModel) selectedAgent() *agent.Agent {
	if d.selected >= 0 && d.selected < len(d.agents) {
		return d.agents[d.selected]
	}
	return nil
}

// humanizeElapsed formats a duration for display.
func humanizeElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
