package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/git"
)

// reposSelectMsg is emitted when the user selects a repo to open.
type reposSelectMsg struct{ path string }

// reposAddMsg is emitted when the user adds a new repo via the file browser.
type reposAddMsg struct{ path string }

// reposRemoveMsg is emitted when the user removes the selected repo.
type reposRemoveMsg struct{ path string }

// reposCancelMsg is emitted when the user presses r/esc to return to the dashboard.
type reposCancelMsg struct{}

// reposModel shows the list of registered repos and integrates the file browser.
type reposModel struct {
	repos    []config.Repo
	managers map[string]*agent.Manager // read-only reference for agent counts
	selected int
	browsing bool // when true, delegate to fileBrowserModel
	browser  fileBrowserModel

	width  int
	height int
}

// newReposModel creates a reposModel with the given repos and managers.
func newReposModel(repos []config.Repo, managers map[string]*agent.Manager) reposModel {
	return reposModel{
		repos:    repos,
		managers: managers,
	}
}

// Update handles key events for the repos view.
func (m reposModel) Update(msg tea.Msg) (reposModel, tea.Cmd) {
	if m.browsing {
		var cmd tea.Cmd
		m.browser, cmd = m.browser.Update(msg)
		if cmd != nil {
			// Wrap the command so we can intercept file browser messages.
			return m, func() tea.Msg {
				result := cmd()
				switch result := result.(type) {
				case fileBrowserSelectMsg:
					m.browsing = false
					return reposAddMsg{path: result.path}
				case fileBrowserCancelMsg:
					m.browsing = false
					return result
				}
				return result
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case fileBrowserSelectMsg:
		m.browsing = false
		return m, func() tea.Msg { return reposAddMsg{path: msg.path} }
	case fileBrowserCancelMsg:
		m.browsing = false
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.selected < len(m.repos)-1 {
				m.selected++
			}
		case "k", "up":
			if m.selected > 0 {
				m.selected--
			}
		case "enter", "right":
			if len(m.repos) > 0 {
				path := m.repos[m.selected].Path
				return m, func() tea.Msg { return reposSelectMsg{path: path} }
			}
		case "a":
			m.browsing = true
			m.browser = newFileBrowserModel()
			m.browser.width = m.width
			m.browser.height = m.contentHeight()
		case "d":
			if len(m.repos) > 0 {
				path := m.repos[m.selected].Path
				return m, func() tea.Msg { return reposRemoveMsg{path: path} }
			}
		case "r", "esc":
			if len(m.managers) > 0 {
				return m, func() tea.Msg { return reposCancelMsg{} }
			}
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the repos list view.
func (m reposModel) View() string {
	if m.browsing {
		return m.browser.View()
	}

	if len(m.repos) == 0 {
		return m.emptyView()
	}

	listWidth := 40
	detailWidth := m.width - listWidth - 1 // 1 for the border

	list := m.renderRepoList(listWidth)
	detail := m.renderRepoDetail(detailWidth)

	listStyle := lipgloss.NewStyle().
		Width(listWidth).
		Height(m.contentHeight()).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(ColorMuted)

	detailStyle := lipgloss.NewStyle().
		Width(detailWidth).
		Height(m.contentHeight())

	return lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(list),
		detailStyle.Render(detail),
	)
}

func (m reposModel) contentHeight() int {
	return m.height - 2 // statusbar + title
}

func (m reposModel) emptyView() string {
	title := StyleTitle.Render("Baton")
	hint := StyleSubtle.Render("No repos added yet — press 'a' to add a git repo.")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", hint)
	return lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, content)
}

func (m reposModel) renderRepoList(width int) string {
	title := StyleTitle.Render("REPOS")
	separator := StyleSubtle.Render(strings.Repeat("─", width-2))

	var items []string
	items = append(items, title, separator)

	for i, repo := range m.repos {
		prefix := "  "
		if i == m.selected {
			prefix = StyleActive.Render("▸ ")
		}

		// Agent count for this repo.
		agentCount := 0
		if mgr, ok := m.managers[repo.Path]; ok {
			agentCount = len(mgr.List())
		}

		// Name column.
		nameWidth := width - 16 // space for agent count, path truncation, padding
		name := repo.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		// Truncated path.
		pathWidth := width - len(name) - 10
		path := repo.Path
		if len(path) > pathWidth && pathWidth > 3 {
			path = "…" + path[len(path)-pathWidth+1:]
		}

		agentStr := fmt.Sprintf("%d", agentCount)
		if agentCount > 0 {
			agentStr = StyleSuccess.Render(agentStr)
		} else {
			agentStr = StyleSubtle.Render(agentStr)
		}

		line := fmt.Sprintf("%s%-*s  %s agents",
			prefix,
			nameWidth,
			name,
			agentStr,
		)
		items = append(items, line)
		items = append(items, "  "+StyleSubtle.Render(path))
		items = append(items, "")
	}

	return strings.Join(items, "\n")
}

func (m reposModel) renderRepoDetail(width int) string {
	if len(m.repos) == 0 {
		return ""
	}

	repo := m.repos[m.selected]

	title := StyleTitle.Render("DETAILS")
	separator := StyleSubtle.Render(strings.Repeat("─", width-1))

	var lines []string
	lines = append(lines, title, separator)
	lines = append(lines, "")
	lines = append(lines, StyleTitle.Render(repo.Name))
	lines = append(lines, StyleSubtle.Render(repo.Path))
	lines = append(lines, "")

	branch, err := git.BaseBranch(repo.Path)
	if err != nil {
		branch = "(unknown)"
	}
	lines = append(lines, StyleSubtle.Render("branch: ")+branch)

	agentCount := 0
	if mgr, ok := m.managers[repo.Path]; ok {
		agentCount = len(mgr.List())
	}
	lines = append(lines, "")
	lines = append(lines, StyleSubtle.Render(fmt.Sprintf("agents: %d", agentCount)))

	return strings.Join(lines, "\n")
}
