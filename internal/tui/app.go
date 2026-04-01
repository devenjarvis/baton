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

// createResultMsg carries the result of async agent creation.
type createResultMsg struct {
	err error
}

// App is the root Bubble Tea model.
type App struct {
	manager  *agent.Manager
	repoPath string

	view      ViewMode
	dashboard dashboardModel
	diff      diffModel
	prompt    promptModel
	merge     mergeModel

	width       int
	height      int
	err         string
	errTicks    int // ticks remaining to show error
	confirmQuit bool
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
		e, ok := <-mgr.Events()
		if !ok {
			return nil // channel closed (shutdown)
		}
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

		// Resize agent terminals to match their current display container.
		if a.view == ViewDashboard {
			a.resizeAllForDashboard()
		}

	case initManagerMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
			return a, nil
		}
		a.manager = msg.manager
		a.repoPath = msg.repoPath
		return a, listenEvents(a.manager)

	case tickMsg:
		a.refreshAgentList()
		if a.errTicks > 0 {
			a.errTicks--
			if a.errTicks == 0 {
				a.err = ""
			}
		}
		return a, tickCmd()

	case agentEventMsg:
		a.refreshAgentList()
		if a.manager != nil {
			return a, listenEvents(a.manager)
		}
		return a, nil

	case createResultMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
		}
		a.refreshAgentList()
		// Resize the new agent to force a clean redraw — Claude Code's
		// initial splash output gets baked into the VT before its TUI
		// fully initializes, and a SIGWINCH clears it.
		a.resizeSelectedForDashboard()
		return a, nil
	}

	// Route to the active view.
	switch a.view {
	case ViewDashboard:
		return a.updateDashboard(msg)
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
		// When the terminal panel has focus, skip all app-level bindings.
		// dashboard.Update handles key forwarding to the agent.
		if a.dashboard.panelFocus == focusTerminal {
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			if a.manager != nil && a.manager.AgentCount() > 0 && !a.confirmQuit {
				a.confirmQuit = true
				return a, nil
			}
			if a.manager != nil {
				a.manager.Shutdown()
			}
			return a, tea.Quit
		default:
			a.confirmQuit = false
		}

		switch msg.String() {
		case "n":
			a.view = ViewPrompt
			a.prompt = newPromptModel()
			a.prompt.width = a.width
			a.prompt.height = a.height
			return a, nil
		case "d":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				rawDiff, err := git.Diff(a.repoPath, ag.Worktree)
				if err != nil {
					a.setError(err.Error())
					return a, nil
				}
				if rawDiff == "" {
					a.setError("No changes yet")
					return a, nil
				}
				a.view = ViewDiff
				a.diff = newDiffModel(rawDiff, a.width, a.height-1)
				return a, nil
			}
		case "x":
			if ag := a.dashboard.selectedAgent(); ag != nil {
				if err := a.manager.Kill(ag.ID); err != nil {
					a.setError(err.Error())
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
					a.setError("Agent must be done or idle to merge")
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

	prevSelected := a.dashboard.selected
	prevPanelFocus := a.dashboard.panelFocus
	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.Update(msg)
	if a.dashboard.selected != prevSelected || a.dashboard.panelFocus != prevPanelFocus {
		a.resizeSelectedForDashboard()
	}
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
			mgr := a.manager
			// Size the agent to the dashboard preview panel so it renders
			// correctly before the user enters focus view.
			previewW := a.dashboard.previewTermWidth()
			previewH := a.dashboard.previewTermHeight()
			if previewW <= 0 || previewH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			cfg := agent.Config{
				Name: msg.name,
				Task: msg.task,
				Rows: previewH,
				Cols: previewW,
			}
			return a, func() tea.Msg {
				_, err := mgr.Create(cfg)
				return createResultMsg{err: err}
			}
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

// resizeSelectedForDashboard resizes the currently selected agent's VT and PTY
// to match the dashboard preview panel dimensions, so rendered output fits
// without wrapping or overflow.
func (a *App) resizeSelectedForDashboard() {
	ag := a.dashboard.selectedAgent()
	if ag == nil {
		return
	}
	w := a.dashboard.previewTermWidth()
	h := a.dashboard.previewTermHeight()
	if w > 0 && h > 0 {
		ag.Resize(h, w)
	}
}

// resizeAllForDashboard resizes every agent to the dashboard preview dimensions.
// Called on WindowSizeMsg so all agents match the new terminal size.
func (a *App) resizeAllForDashboard() {
	w := a.dashboard.previewTermWidth()
	h := a.dashboard.previewTermHeight()
	if w <= 0 || h <= 0 {
		return
	}
	for _, ag := range a.dashboard.agents {
		ag.Resize(h, w)
	}
}

// setError sets an error message that displays for ~3 seconds (30 ticks at 100ms).
func (a *App) setError(msg string) {
	a.err = msg
	a.errTicks = 30
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
		hints := dashboardHints
		if a.dashboard.panelFocus == focusTerminal {
			hints = focusTerminalHints
		}
		statusbar := renderStatusBar(hints, a.width)
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

	// Show quit confirmation.
	if a.confirmQuit {
		confirmLine := StyleWarning.Render("Agents are running. Press q again to quit, or any key to cancel.")
		content = lipgloss.JoinVertical(lipgloss.Left, confirmLine, content)
	}

	// Show error (auto-cleared after ~3 seconds).
	if a.err != "" {
		errLine := StyleError.Render("Error: " + a.err)
		content = lipgloss.JoinVertical(lipgloss.Left, errLine, content)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
