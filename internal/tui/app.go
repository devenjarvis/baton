package tui

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/git"
)

// tickMsg triggers periodic re-renders.
type tickMsg time.Time

// agentEventMsg wraps an agent manager event for the TUI.
type agentEventMsg agent.Event

// App is the root Bubble Tea model.
type App struct {
	manager   *agent.Manager
	repoPath  string

	view      ViewMode
	dashboard dashboardModel
	focus     focusModel
	diff      diffModel
	prompt    promptModel
	merge     mergeModel

	width  int
	height int
	err    string
}

func NewApp() App {
	return App{
		view:      ViewDashboard,
		dashboard: newDashboardModel(),
	}
}

func (a App) Init() tea.Cmd {
	return tea.Batch(tickCmd(), initManagerCmd())
}

// initManagerMsg carries the initialized manager.
type initManagerMsg struct {
	manager  *agent.Manager
	repoPath string
	err      error
}

func initManagerCmd() tea.Cmd {
	return func() tea.Msg {
		// Detect repo path from current directory.
		repoPath := "."
		return initManagerMsg{
			manager:  agent.NewManager(repoPath),
			repoPath: repoPath,
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func listenEvents(mgr *agent.Manager) tea.Cmd {
	return func() tea.Msg {
		e := <-mgr.Events()
		return agentEventMsg(e)
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.dashboard.width = msg.Width
		a.dashboard.height = msg.Height - 1 // room for statusbar
		a.prompt.width = msg.Width
		a.prompt.height = msg.Height
		a.merge.width = msg.Width
		a.merge.height = msg.Height
		a.diff.width = msg.Width
		a.diff.height = msg.Height - 1

		// Resize focused agent terminal.
		if a.view == ViewFocus && a.focus.agent != nil {
			a.focus.agent.Resize(msg.Height, msg.Width)
		}

	case initManagerMsg:
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.manager = msg.manager
		a.repoPath = msg.repoPath
		return a, listenEvents(a.manager)

	case tickMsg:
		a.refreshAgentList()
		return a, tickCmd()

	case agentEventMsg:
		a.refreshAgentList()
		if a.manager != nil {
			return a, listenEvents(a.manager)
		}
		return a, nil
	}

	// Route to the active view.
	switch a.view {
	case ViewDashboard:
		return a.updateDashboard(msg)
	case ViewFocus:
		return a.updateFocus(msg)
	case ViewDiff:
		return a.updateDiff(msg)
	case ViewPrompt:
		return a.updatePrompt(msg)
	case ViewMerge:
		return a.updateMerge(msg)
	}

	return a, nil
}

func (a App) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if a.manager != nil {
				a.manager.Shutdown()
			}
			return a, tea.Quit
		case "n":
			a.view = ViewPrompt
			a.prompt = newPromptModel()
			a.prompt.width = a.width
			a.prompt.height = a.height
			return a, nil
		case "enter":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				a.view = ViewFocus
				a.focus = newFocusModel(ag)
				a.focus.width = a.width
				a.focus.height = a.height
				ag.Resize(a.height, a.width)
				return a, nil
			}
		case "d":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				rawDiff, err := git.Diff(a.repoPath, ag.Worktree)
				if err != nil {
					a.err = err.Error()
					return a, nil
				}
				if rawDiff == "" {
					a.err = "No changes yet"
					return a, nil
				}
				a.view = ViewDiff
				a.diff = newDiffModel(rawDiff, a.width, a.height-1)
				return a, nil
			}
		case "x":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				if err := a.manager.Kill(ag.ID); err != nil {
					a.err = err.Error()
				}
				a.refreshAgentList()
				if a.dashboard.selected >= len(a.dashboard.agents) && a.dashboard.selected > 0 {
					a.dashboard.selected--
				}
				return a, nil
			}
		case "m":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				if ag.Status() != agent.StatusDone && ag.Status() != agent.StatusIdle {
					a.err = "Agent must be done or idle to merge"
					return a, nil
				}
				a.view = ViewMerge
				a.merge = newMergeModel(ag.Name, ag.Worktree.Branch, ag.Worktree.BaseBranch)
				a.merge.width = a.width
				a.merge.height = a.height
				return a, nil
			}
		}
	}

	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.Update(msg)
	return a, cmd
}

func (a App) updateFocus(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case focusExitMsg:
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.focus, cmd = a.focus.Update(msg)
	return a, cmd
}

func (a App) updateDiff(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case diffCloseMsg:
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.diff, cmd = a.diff.Update(msg)
	return a, cmd
}

func (a App) updatePrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case promptCancelMsg:
		a.view = ViewDashboard
		return a, nil
	case promptResult:
		a.view = ViewDashboard
		if a.manager != nil {
			cfg := agent.Config{
				Name: msg.name,
				Task: msg.task,
				Rows: a.height,
				Cols: a.width,
			}
			if _, err := a.manager.Create(cfg); err != nil {
				a.err = err.Error()
			}
			a.refreshAgentList()
		}
		return a, nil
	}

	var cmd tea.Cmd
	a.prompt, cmd = a.prompt.Update(msg)
	return a, cmd
}

func (a App) updateMerge(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case mergeCancelMsg:
		a.view = ViewDashboard
		return a, nil
	case mergeConfirmMsg:
		ag := a.dashboard.selectedAgent()
		if ag == nil {
			a.view = ViewDashboard
			return a, nil
		}
		return a, func() tea.Msg {
			message := "Merge baton/" + ag.Name + " into " + ag.Worktree.BaseBranch
			err := git.MergeWorktree(a.repoPath, ag.Worktree, message)
			return mergeCompleteMsg{err: err}
		}
	case mergeCompleteMsg:
		if msg.err != nil {
			a.merge.errMsg = msg.err.Error()
			return a, nil
		}
		// Merge succeeded — clean up the agent.
		if ag := a.dashboard.selectedAgent(); ag != nil {
			_ = a.manager.Kill(ag.ID)
			a.refreshAgentList()
			if a.dashboard.selected >= len(a.dashboard.agents) && a.dashboard.selected > 0 {
				a.dashboard.selected--
			}
		}
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.merge, cmd = a.merge.Update(msg)
	return a, cmd
}

func (a *App) refreshAgentList() {
	if a.manager == nil {
		return
	}
	agents := a.manager.List()
	// Sort by creation time.
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].CreatedAt.Before(agents[j].CreatedAt)
	})
	a.dashboard.agents = agents
}

func (a App) View() tea.View {
	var content string

	switch a.view {
	case ViewDashboard:
		body := a.dashboard.View()
		statusbar := renderStatusBar(dashboardHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewFocus:
		body := a.focus.View()
		statusbar := renderStatusBar(focusHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewDiff:
		body := a.diff.View()
		statusbar := renderStatusBar(diffHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewPrompt:
		content = a.prompt.View()
	case ViewMerge:
		content = a.merge.View()
	}

	// Show error overlay briefly.
	if a.err != "" {
		errLine := StyleError.Render("Error: " + a.err)
		// Clear error on next render.
		a.err = ""
		content = lipgloss.JoinVertical(lipgloss.Left, errLine, content)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
