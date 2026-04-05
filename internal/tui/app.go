package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/audio"
	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/git"
)

// tickMsg triggers periodic re-renders.
type tickMsg time.Time

// agentEventMsg wraps an agent manager event for the TUI.
type agentEventMsg struct {
	event    agent.Event
	repoPath string
}

// createResultMsg carries the result of async agent creation.
type createResultMsg struct {
	sessionID string
	agentID   string
	err       error
}

// splashResizeMsg is a delayed resize sent after agent creation to clear
// Claude Code's splash text once its TUI has had time to initialize.
type splashResizeMsg struct {
	agentID string
}

// diffStatsMsg carries the result of an async diff stats refresh.
type diffStatsMsg struct {
	sessionID string
	stats     *diffSummaryData
}

// initAppMsg carries the result of app initialization.
type initAppMsg struct {
	cfg *config.Config
	err error
}

// diffStatsEntry holds cached diff stats for a single session.
type diffStatsEntry struct {
	stats       *diffSummaryData
	lastRefresh time.Time
}

// App is the root Bubble Tea model.
type App struct {
	managers   map[string]*agent.Manager
	activeRepo string
	cfg        *config.Config
	repoBrowser fileBrowserModel

	view      ViewMode
	dashboard dashboardModel
	diff      diffModel
	merge     mergeModel

	width       int
	height      int
	err         string
	errTicks    int // ticks remaining to show error
	confirmQuit bool

	lastKnownStatus map[string]agent.Status
	audioPlayer     *audio.Player

	diffStatsCache    map[string]*diffStatsEntry // keyed by session ID
	diffRefreshInFlight bool
}

func NewApp() App {
	return App{
		view:            ViewDashboard,
		dashboard:       newDashboardModel(),
		managers:        make(map[string]*agent.Manager),
		lastKnownStatus: make(map[string]agent.Status),
		diffStatsCache:  make(map[string]*diffStatsEntry),
	}
}

func (a App) Init() tea.Cmd {
	return tea.Batch(tickCmd(), initAppCmd())
}

func initAppCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return initAppMsg{err: err}
		}

		if len(cfg.Repos) == 0 {
			// Auto-register the current working directory on first run.
			if err := config.AddRepo(cfg, "."); err != nil {
				return initAppMsg{err: err}
			}
			if err := config.Save(cfg); err != nil {
				return initAppMsg{err: err}
			}
		}

		return initAppMsg{cfg: cfg}
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
		return agentEventMsg{event: e, repoPath: mgr.RepoPath()}
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.dashboard.width = msg.Width
		a.dashboard.height = msg.Height - 1 // room for statusbar
		a.merge.width = msg.Width
		a.merge.height = msg.Height
		a.diff.width = msg.Width
		a.diff.height = msg.Height - 1
		a.repoBrowser.width = msg.Width
		a.repoBrowser.height = msg.Height - 1

		// Resize agent terminals to match their current display container.
		if a.view == ViewDashboard {
			a.resizeAllForDashboard()
		}

	case initAppMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
			return a, nil
		}
		a.cfg = msg.cfg
		// Initialize audio player (best-effort — nil on failure).
		if p, err := audio.NewPlayer(); err == nil {
			a.audioPlayer = p
		}
		// Create a manager for every registered repo and start event listeners.
		var cmds []tea.Cmd
		for _, repo := range msg.cfg.Repos {
			if a.managers[repo.Path] == nil {
				mgr := agent.NewManager(repo.Path)
				a.managers[repo.Path] = mgr
				ensureGitignore(repo.Path)
				cmds = append(cmds, listenEvents(mgr))
			}
		}
		// Always start on the dashboard — single repo or many.
		a.view = ViewDashboard
		a.refreshAgentList()
		return a, tea.Batch(cmds...)

	case tickMsg:
		a.refreshAgentList()
		// Auto-rename agents that just went idle for the first time,
		// and detect Active->Idle transitions for audio notification and diff refresh.
		idleTransition := false
		for _, item := range a.dashboard.items {
			if item.kind != listItemAgent || item.agent == nil {
				continue
			}
			ag := item.agent
			currentStatus := ag.Status()

			// Check for Active->Idle transition.
			if prev, ok := a.lastKnownStatus[ag.ID]; ok {
				if prev == agent.StatusActive && currentStatus == agent.StatusIdle {
					if a.audioPlayer != nil {
						a.audioPlayer.Play()
					}
					idleTransition = true
				}
			}
			a.lastKnownStatus[ag.ID] = currentStatus

			if currentStatus != agent.StatusIdle || ag.HasDisplayName() {
				continue
			}
			name := extractAgentName(ag.Render())
			if name == "" {
				name = ag.Name // fallback: use random name to prevent retrying
			}
			ag.SetDisplayName(name)
		}
		if a.errTicks > 0 {
			a.errTicks--
			if a.errTicks == 0 {
				a.err = ""
			}
		}
		// Clean stale diff cache entries periodically.
		a.cleanDiffStatsCache()
		// Refresh diff stats periodically or on idle transition.
		var diffCmd tea.Cmd
		if sess := a.dashboard.selectedSession(); sess != nil {
			entry := a.diffStatsCache[sess.ID]
			stale := entry == nil || time.Since(entry.lastRefresh) > 5*time.Second
			if (stale || idleTransition) && !a.diffRefreshInFlight {
				diffCmd = a.refreshDiffStatsCmd()
			}
		}
		return a, tea.Batch(tickCmd(), diffCmd)

	case agentEventMsg:
		// Refresh list on any agent event — all repos are visible in the dashboard.
		a.refreshAgentList()
		if mgr := a.managers[msg.repoPath]; mgr != nil {
			return a, listenEvents(mgr)
		}
		return a, nil

	case createResultMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
			return a, nil
		}
		a.refreshAgentList()
		// Find the new agent by ID, select it, and auto-focus its terminal.
		if msg.agentID != "" {
			for i, item := range a.dashboard.items {
				if item.kind == listItemAgent && item.agent != nil && item.agent.ID == msg.agentID {
					a.dashboard.selected = i
					a.dashboard.panelFocus = focusTerminal
					break
				}
			}
		}
		// Resize to force a clean redraw — Claude Code's initial splash output
		// gets baked into the VT before its TUI fully initializes, and a SIGWINCH clears it.
		a.resizeSelectedForDashboard()
		// Schedule a delayed follow-up resize: the immediate one above sets correct
		// dimensions but arrives before Claude Code's TUI is ready. The delayed
		// SIGWINCH hits after the TUI has initialized and triggers a clean redraw.
		delayedResize := tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return splashResizeMsg{agentID: msg.agentID}
		})
		return a, delayedResize

	case diffStatsMsg:
		a.diffRefreshInFlight = false
		// Always update cache timestamp to prevent tight retry loops on persistent errors.
		a.diffStatsCache[msg.sessionID] = &diffStatsEntry{
			stats:       msg.stats,
			lastRefresh: time.Now(),
		}
		// Update dashboard with current session's stats.
		if sess := a.dashboard.selectedSession(); sess != nil && sess.ID == msg.sessionID {
			a.dashboard.diffStats = msg.stats
		}
		return a, nil

	case splashResizeMsg:
		// Guard: only resize if we're still on the dashboard and the agent
		// that triggered this is still selected.
		if a.view != ViewDashboard {
			return a, nil
		}
		ag := a.dashboard.selectedAgent()
		if ag != nil && ag.ID == msg.agentID {
			a.resizeSelectedForDashboard()
		}
		return a, nil
	}

	// Route to the active view.
	switch a.view {
	case ViewDashboard:
		return a.updateDashboard(msg)
	case ViewDiff:
		return a.updateDiff(msg)
	case ViewMerge:
		return a.updateMerge(msg)
	case ViewFileBrowser:
		return a.updateFileBrowser(msg)
	}

	return a, nil
}

// addRepo adds a new repo to config, creates its manager, and starts listening.
// Returns a cmd if a new manager was created.
func (a *App) addRepo(path string) tea.Cmd {
	if a.cfg == nil {
		return nil
	}
	if err := config.AddRepo(a.cfg, path); err != nil {
		a.setError(err.Error())
		return nil
	}
	if err := config.Save(a.cfg); err != nil {
		a.setError(err.Error())
	}
	// Resolve to the absolute path that AddRepo stored.
	absPath := a.cfg.Repos[len(a.cfg.Repos)-1].Path
	if a.managers[absPath] == nil {
		mgr := agent.NewManager(absPath)
		a.managers[absPath] = mgr
		ensureGitignore(absPath)
		a.refreshAgentList()
		return listenEvents(mgr)
	}
	a.refreshAgentList()
	return nil
}

func (a App) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// When the terminal panel has focus, skip all app-level bindings.
		if a.dashboard.panelFocus == focusTerminal {
			a.confirmQuit = false
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			hasRunning := false
			for _, mgr := range a.managers {
				if mgr.AgentCount() > 0 {
					hasRunning = true
					break
				}
			}
			if hasRunning && !a.confirmQuit {
				a.confirmQuit = true
				return a, nil
			}
			for _, mgr := range a.managers {
				mgr.Shutdown()
			}
			if a.audioPlayer != nil {
				a.audioPlayer.Close()
			}
			return a, tea.Quit
		default:
			a.confirmQuit = false
		}

		switch msg.String() {
		case "n":
			// Create a new session in the repo of the currently selected item.
			repoPath := a.dashboard.selectedRepoPath()
			if repoPath == "" {
				repoPath = a.activeRepo
			}
			if repoPath == "" {
				return a, nil
			}
			a.activeRepo = repoPath
			previewW := a.dashboard.previewTermWidth()
			previewH := a.dashboard.previewTermHeight()
			if previewW <= 0 || previewH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			bypassPerms := true
			if a.cfg != nil {
				bypassPerms = a.cfg.GetBypassPermissions()
			}
			mgr := a.managers[repoPath]
			if mgr == nil {
				return a, nil
			}
			cfg := agent.Config{
				Rows:              previewH,
				Cols:              previewW,
				BypassPermissions: bypassPerms,
			}
			return a, func() tea.Msg {
				sess, ag, err := mgr.CreateSession(cfg)
				if err != nil {
					return createResultMsg{err: err}
				}
				return createResultMsg{sessionID: sess.ID, agentID: ag.ID}
			}

		case "c":
			// Add an agent to the selected session.
			sess := a.dashboard.selectedSession()
			if sess == nil {
				a.setError("No session selected")
				return a, nil
			}
			repoPath := a.dashboard.selectedRepoPath()
			if repoPath == "" {
				repoPath = a.activeRepo
			}
			mgr := a.managers[repoPath]
			if mgr == nil {
				return a, nil
			}
			previewW := a.dashboard.previewTermWidth()
			previewH := a.dashboard.previewTermHeight()
			if previewW <= 0 || previewH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			bypassPerms := true
			if a.cfg != nil {
				bypassPerms = a.cfg.GetBypassPermissions()
			}
			cfg := agent.Config{
				Rows:              previewH,
				Cols:              previewW,
				BypassPermissions: bypassPerms,
			}
			sessionID := sess.ID
			return a, func() tea.Msg {
				ag, err := mgr.AddAgent(sessionID, cfg)
				if err != nil {
					return createResultMsg{err: err}
				}
				return createResultMsg{sessionID: sessionID, agentID: ag.ID}
			}

		case "a":
			// Open file browser to add a new repo.
			a.repoBrowser = newFileBrowserModel()
			a.repoBrowser.width = a.width
			a.repoBrowser.height = a.height - 1
			a.view = ViewFileBrowser
			return a, nil

		case "d":
			item := a.dashboard.selectedItem()
			if item == nil {
				return a, nil
			}
			if item.kind == listItemSession || item.kind == listItemAgent {
				// Diff the session's worktree.
				sess := item.session
				if sess == nil {
					return a, nil
				}
				rawDiff, err := git.Diff(item.repoPath, sess.Worktree)
				if err != nil {
					a.setError(err.Error())
					return a, nil
				}
				if rawDiff == "" {
					a.setError("No changes yet")
					return a, nil
				}
				files := git.ParseDiffFiles(rawDiff)
				a.view = ViewDiff
				a.diff = newDiffModel(sess.Name, files, a.width, a.height-1)
				return a, nil
			}
			// Repo header selected — remove the repo.
			if a.cfg != nil {
				config.RemoveRepo(a.cfg, item.repoPath)
				if err := config.Save(a.cfg); err != nil {
					a.setError(err.Error())
				}
				a.refreshAgentList()
			}
			return a, nil

		case "x":
			// Kill the selected agent.
			item := a.dashboard.selectedItem()
			if item == nil || item.kind != listItemAgent || item.agent == nil || item.session == nil {
				return a, nil
			}
			mgr := a.managers[item.repoPath]
			if mgr == nil {
				return a, nil
			}
			if err := mgr.KillAgent(item.session.ID, item.agent.ID); err != nil {
				a.setError(err.Error())
			}
			delete(a.lastKnownStatus, item.agent.ID)
			a.refreshAgentList()
			return a, nil

		case "X":
			// Kill the entire parent session of the selected agent.
			item := a.dashboard.selectedItem()
			if item == nil || item.session == nil {
				return a, nil
			}
			mgr := a.managers[item.repoPath]
			if mgr == nil {
				return a, nil
			}
			sessID := item.session.ID
			for _, ag := range item.session.Agents() {
				delete(a.lastKnownStatus, ag.ID)
			}
			if err := mgr.KillSession(sessID); err != nil {
				a.setError(err.Error())
			}
			delete(a.diffStatsCache, sessID)
			a.refreshAgentList()
			a.updateDashboardDiffStats()
			return a, nil

		case "m":
			item := a.dashboard.selectedItem()
			if item == nil {
				return a, nil
			}
			sess := a.dashboard.selectedSession()
			if sess == nil {
				return a, nil
			}
			// Check that all agents in the session are done or idle.
			for _, ag := range sess.Agents() {
				st := ag.Status()
				if st != agent.StatusDone && st != agent.StatusIdle {
					a.setError("All agents in session must be done or idle to merge")
					return a, nil
				}
			}
			a.activeRepo = item.repoPath
			a.view = ViewMerge
			a.merge = newMergeModel(sess.Name, sess.Worktree.Branch, sess.Worktree.BaseBranch)
			a.merge.width = a.width
			a.merge.height = a.height
			return a, nil
		}
	}

	if msg, ok := msg.(tea.MouseClickMsg); ok {
		// Compute offset before clearing confirmQuit — reflects what was on screen.
		dashboardTopY := 0
		if a.err != "" {
			dashboardTopY++
		}
		if a.confirmQuit {
			dashboardTopY++
		}
		a.confirmQuit = false
		if msg.Button == tea.MouseLeft {
			if msg.X < 31 {
				// List panel click — map Y to item index.
				// Subtract 2 for the SESSIONS title row and separator row.
				itemIndex := msg.Y - dashboardTopY - 2
				if itemIndex >= 0 && itemIndex < len(a.dashboard.items) {
					a.dashboard.selected = itemIndex
					a.dashboard.clampToAgent()
					a.dashboard.panelFocus = focusList
					a.dashboard.scrollOffset = 0
					a.resizeSelectedForDashboard()
				}
			} else if msg.X >= 32 {
				// Preview panel click — enter focusTerminal if an agent is selected.
				// X==31 is the list panel's right border and is intentionally ignored.
				if a.dashboard.panelFocus == focusList && a.dashboard.selectedAgent() != nil {
					a.dashboard.panelFocus = focusTerminal
					a.resizeSelectedForDashboard()
				}
			}
		}
		return a, nil
	}

	if msg, ok := msg.(tea.MouseWheelMsg); ok {
		if a.dashboard.panelFocus == focusTerminal {
			ag := a.dashboard.selectedAgent()
			if ag != nil {
				switch msg.Button {
				case tea.MouseWheelUp:
					a.dashboard.scrollOffset += 3
					maxOffset := len(ag.ScrollbackLines())
					if a.dashboard.scrollOffset > maxOffset {
						a.dashboard.scrollOffset = maxOffset
					}
				case tea.MouseWheelDown:
					a.dashboard.scrollOffset -= 3
					if a.dashboard.scrollOffset < 0 {
						a.dashboard.scrollOffset = 0
					}
				}
			}
		}
		return a, nil
	}

	prevSelected := a.dashboard.selected
	prevPanelFocus := a.dashboard.panelFocus
	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.Update(msg)
	if a.dashboard.selected != prevSelected || a.dashboard.panelFocus != prevPanelFocus {
		a.resizeSelectedForDashboard()
	}
	// On selection change, update diff stats from cache (or trigger refresh).
	if a.dashboard.selected != prevSelected {
		a.updateDashboardDiffStats()
		if sess := a.dashboard.selectedSession(); sess != nil {
			entry := a.diffStatsCache[sess.ID]
			if (entry == nil || time.Since(entry.lastRefresh) > 5*time.Second) && !a.diffRefreshInFlight {
				diffCmd := a.refreshDiffStatsCmd()
				return a, tea.Batch(cmd, diffCmd)
			}
		}
	}
	return a, cmd
}

func (a App) updateFileBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fileBrowserSelectMsg:
		a.view = ViewDashboard
		cmd := a.addRepo(msg.path)
		return a, cmd
	case fileBrowserCancelMsg:
		a.view = ViewDashboard
		return a, nil
	}
	var cmd tea.Cmd
	a.repoBrowser, cmd = a.repoBrowser.Update(msg)
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


func (a App) updateMerge(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case mergeCancelMsg:
		a.view = ViewDashboard
		return a, nil
	case mergeConfirmMsg:
		sess := a.dashboard.selectedSession()
		if sess == nil {
			a.view = ViewDashboard
			return a, nil
		}
		activeRepo := a.activeRepo
		wt := sess.Worktree
		sessName := sess.Name
		return a, func() tea.Msg {
			message := "Merge baton/" + sessName + " into " + wt.BaseBranch
			err := git.MergeWorktree(activeRepo, wt, message)
			return mergeCompleteMsg{err: err}
		}
	case mergeCompleteMsg:
		if msg.err != nil {
			a.merge.errMsg = msg.err.Error()
			return a, nil
		}
		// Merge succeeded — clean up the entire session.
		// Collect agent IDs before KillSession clears the agents map.
		sess := a.dashboard.selectedSession()
		if sess != nil {
			sessID := sess.ID
			for _, ag := range sess.Agents() {
				delete(a.lastKnownStatus, ag.ID)
			}
			item := a.dashboard.selectedItem()
			if item != nil {
				mgr := a.managers[item.repoPath]
				if mgr != nil {
					_ = mgr.KillSession(sessID)
				}
			}
			delete(a.diffStatsCache, sessID)
			a.refreshAgentList()
			a.updateDashboardDiffStats()
		}
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.merge, cmd = a.merge.Update(msg)
	return a, cmd
}

// resizeSelectedForDashboard resizes the currently selected agent's VT and PTY
// to match the dashboard preview panel dimensions.
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
	for _, ag := range a.dashboard.agentItems() {
		ag.Resize(h, w)
	}
}

// setError sets an error message that displays for ~3 seconds (30 ticks at 100ms).
func (a *App) setError(msg string) {
	a.err = msg
	a.errTicks = 30
}

func (a *App) refreshAgentList() {
	if a.cfg == nil {
		// Fallback used in tests that set up managers directly without cfg.
		if mgr := a.managers[a.activeRepo]; mgr != nil {
			var items []listItem
			sessions := mgr.ListSessions()
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
			})
			for _, sess := range sessions {
				items = append(items, listItem{
					kind:     listItemSession,
					repoPath: a.activeRepo,
					session:  sess,
				})
				for _, ag := range sess.Agents() {
					items = append(items, listItem{
						kind:     listItemAgent,
						repoPath: a.activeRepo,
						session:  sess,
						agent:    ag,
					})
				}
			}
			a.dashboard.items = items
			a.dashboard.clampToAgent()
			}
		return
	}

	// Build hierarchical list: repo > session > agent.
	var items []listItem
	for _, repo := range a.cfg.Repos {
		items = append(items, listItem{
			kind:     listItemRepo,
			repoPath: repo.Path,
			repoName: repo.Name,
		})
		mgr := a.managers[repo.Path]
		if mgr != nil {
			sessions := mgr.ListSessions()
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
			})
			for _, sess := range sessions {
				items = append(items, listItem{
					kind:     listItemSession,
					repoPath: repo.Path,
					session:  sess,
				})
				for _, ag := range sess.Agents() {
					items = append(items, listItem{
						kind:     listItemAgent,
						repoPath: repo.Path,
						session:  sess,
						agent:    ag,
					})
				}
			}
		}
	}

	// Clamp selection to valid range.
	if len(items) > 0 && a.dashboard.selected >= len(items) {
		a.dashboard.selected = len(items) - 1
	}
	a.dashboard.items = items
	a.dashboard.clampToAgent()
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
	case ViewMerge:
		content = a.merge.View()
	case ViewFileBrowser:
		body := a.repoBrowser.View()
		statusbar := renderStatusBar(repoBrowsingHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
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
	if a.view == ViewDashboard {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

var (
	ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	nonAlnumRe   = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

// extractAgentName scans ANSI-rendered terminal output for the first Claude REPL
// user-input line ("> " prefix after stripping escape codes) and returns a slug.
// Returns "" if no suitable line is found.
func extractAgentName(rendered string) string {
	// Strip ANSI escape codes.
	plain := ansiEscapeRe.ReplaceAllString(rendered, "")

	// Find the first line starting with "> " (Claude REPL user input).
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "> ") {
			continue
		}
		input := strings.TrimPrefix(line, "> ")
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		slug := slugify(input)
		if slug != "" {
			return slug
		}
	}
	return ""
}

// slugify lowercases s, collapses runs of non-alphanumeric characters to "-",
// trims leading/trailing "-", and truncates to 40 characters.
// Returns "" if the result is empty or doesn't start with [a-zA-Z0-9].
func slugify(s string) string {
	slug := nonAlnumRe.ReplaceAllString(strings.ToLower(s), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "-")
	}
	if slug == "" {
		return ""
	}
	// Must start with alphanumeric (validName constraint).
	if slug[0] < 'a' || slug[0] > 'z' {
		if slug[0] < '0' || slug[0] > '9' {
			return ""
		}
	}
	return slug
}

// refreshDiffStatsCmd returns a Cmd that fetches diff stats for the currently selected session.
func (a *App) refreshDiffStatsCmd() tea.Cmd {
	sess := a.dashboard.selectedSession()
	if sess == nil {
		return nil
	}
	repoPath := a.dashboard.selectedRepoPath()
	if repoPath == "" {
		return nil
	}
	a.diffRefreshInFlight = true
	sessionID := sess.ID
	wt := sess.Worktree
	return func() tea.Msg {
		fileStats, agg, err := git.GetPerFileDiffStats(repoPath, wt)
		if err != nil {
			return diffStatsMsg{sessionID: sessionID, stats: nil}
		}
		// Convert git.FileStat to diffFileStat.
		var files []diffFileStat
		for _, fs := range fileStats {
			files = append(files, diffFileStat{
				Path:       fs.Path,
				Status:     fs.Status,
				Insertions: fs.Insertions,
				Deletions:  fs.Deletions,
			})
		}
		return diffStatsMsg{
			sessionID: sessionID,
			stats: &diffSummaryData{
				Files: files,
				Aggregate: diffAggregateStats{
					Files:      agg.Files,
					Insertions: agg.Insertions,
					Deletions:  agg.Deletions,
				},
			},
		}
	}
}

// updateDashboardDiffStats passes cached diff stats to the dashboard for the current selection.
func (a *App) updateDashboardDiffStats() {
	sess := a.dashboard.selectedSession()
	if sess == nil {
		a.dashboard.diffStats = nil
		return
	}
	if entry, ok := a.diffStatsCache[sess.ID]; ok {
		a.dashboard.diffStats = entry.stats
	} else {
		a.dashboard.diffStats = nil
	}
}

// cleanDiffStatsCache removes entries for sessions that no longer exist.
func (a *App) cleanDiffStatsCache() {
	activeSessions := make(map[string]bool)
	for _, mgr := range a.managers {
		for _, sess := range mgr.ListSessions() {
			activeSessions[sess.ID] = true
		}
	}
	for id := range a.diffStatsCache {
		if !activeSessions[id] {
			delete(a.diffStatsCache, id)
		}
	}
}

// ensureGitignore adds .baton/ to .gitignore in the given path if not already present.
func ensureGitignore(path string) {
	const entry = ".baton/"
	gitignorePath := filepath.Join(path, ".gitignore")

	// Check if .gitignore exists and already contains .baton/.
	data, _ := os.ReadFile(gitignorePath)
	if len(data) > 0 {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				return // already present
			}
		}
	}

	// Append .baton/ to .gitignore.
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // best-effort
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}
