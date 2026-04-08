package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/audio"
	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/git"
	"github.com/devenjarvis/baton/internal/state"
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

	// Settings
	globalSettings *config.GlobalSettings
	repoSettings   map[string]*config.RepoSettings   // keyed by repo path
	resolvedCache  map[string]config.ResolvedSettings // keyed by repo path

	view         ViewMode
	dashboard    dashboardModel
	diff         diffModel
	merge        mergeModel
	globalConfig globalConfigModel

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
		repoSettings:    make(map[string]*config.RepoSettings),
		resolvedCache:   make(map[string]config.ResolvedSettings),
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

		// Load global settings and run one-time migration.
		globalSettings, err := config.LoadGlobalSettings()
		if err != nil {
			a.setError(err.Error())
		} else {
			a.globalSettings = globalSettings
			_ = config.MigrateBypassPermissions(a.cfg)
		}

		// Load per-repo settings and build resolved cache.
		for _, repo := range msg.cfg.Repos {
			rs, _ := config.LoadRepoSettings(repo.Path)
			a.repoSettings[repo.Path] = rs
			a.resolvedCache[repo.Path] = config.Resolve(a.globalSettings, rs)
		}

		// Initialize audio player (best-effort — nil on failure).
		if p, err := audio.NewPlayer(); err == nil {
			a.audioPlayer = p
		}
		// Create a manager for every registered repo and start event listeners.
		var cmds []tea.Cmd
		for _, repo := range msg.cfg.Repos {
			if a.managers[repo.Path] == nil {
				mgr := agent.NewManager(repo.Path, a.resolvedCache[repo.Path])
				a.managers[repo.Path] = mgr
				ensureGitignore(repo.Path)
				cmds = append(cmds, listenEvents(mgr))
			}
		}
		// Auto-resume saved sessions for each repo.
		for _, repo := range msg.cfg.Repos {
			bs, err := state.Load(repo.Path)
			if err != nil || bs == nil {
				continue
			}
			mgr := a.managers[repo.Path]
			if mgr == nil {
				continue
			}
			resolved := a.resolvedCache[repo.Path]
			resumeCfg := agent.Config{
				Rows:              24,
				Cols:              80,
				BypassPermissions: resolved.BypassPermissions,
				AgentProgram:      resolved.AgentProgram,
			}
			for _, ss := range bs.Sessions {
				if err := mgr.ResumeSession(ss, resumeCfg); err != nil {
					// Skip sessions whose worktrees are missing — don't crash.
					continue
				}
			}
			// Clean up state file after successful resume.
			_ = state.Remove(repo.Path)
		}
		// Always start on the dashboard — single repo or many.
		a.view = ViewDashboard
		a.refreshAgentList()
		return a, tea.Batch(cmds...)

	case tickMsg:
		a.refreshAgentList()
		// Poll for Claude session names and detect Active->Idle transitions.
		idleTransition := false
		for _, item := range a.dashboard.items {
			if item.kind != listItemAgent || item.agent == nil {
				continue
			}
			ag := item.agent
			currentStatus := ag.Status()

			// Check for Active->Idle transition.
			if prev, ok := a.lastKnownStatus[ag.ID]; ok {
				if prev == agent.StatusActive && currentStatus == agent.StatusIdle && ag.HasReceivedInput() && !ag.IsShell {
					resolved := a.resolvedCache[item.repoPath]
					if resolved.AudioEnabled && a.audioPlayer != nil {
						a.audioPlayer.Play()
					}
					idleTransition = true
				}
			}
			a.lastKnownStatus[ag.ID] = currentStatus

			// Poll Claude session file for auto-generated name.
			if !ag.IsShell && !ag.HasClaudeName() {
				if name := ag.PollClaudeSessionName(); name != "" {
					ag.SetDisplayName(name)
					ag.SetClaudeName(true)
					if item.session != nil && !item.session.HasDisplayName() {
						item.session.SetDisplayName(name)
					}
				}
			}
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
		// Clean up stale lastKnownStatus entries when a session auto-closes.
		if msg.event.Type == agent.EventSessionClosed && msg.event.SessionID != "" {
			prefix := msg.event.SessionID + "-agent-"
			for id := range a.lastKnownStatus {
				if strings.HasPrefix(id, prefix) {
					delete(a.lastKnownStatus, id)
				}
			}
		}
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
	case ViewGlobalConfig:
		return a.updateGlobalConfig(msg)
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

	// Load repo settings and build resolved cache for new repo.
	rs, _ := config.LoadRepoSettings(absPath)
	a.repoSettings[absPath] = rs
	a.resolvedCache[absPath] = config.Resolve(a.globalSettings, rs)

	if a.managers[absPath] == nil {
		mgr := agent.NewManager(absPath, a.resolvedCache[absPath])
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
	case configFormSaveMsg:
		// Repo config form saved.
		if a.dashboard.repoConfigForm != nil && a.dashboard.configRepoPath != "" {
			repoPath := a.dashboard.configRepoPath
			settings := a.extractRepoSettings()
			if err := config.SaveRepoSettings(repoPath, settings); err != nil {
				a.setError(err.Error())
			} else {
				a.repoSettings[repoPath] = settings
				a.resolvedCache[repoPath] = config.Resolve(a.globalSettings, settings)
				if mgr := a.managers[repoPath]; mgr != nil {
					mgr.UpdateSettings(a.resolvedCache[repoPath])
				}
			}
		}
		a.dashboard.panelFocus = focusList
		a.dashboard.repoConfigForm = nil
		return a, nil

	case configFormCancelMsg:
		// Repo config form cancelled.
		a.dashboard.panelFocus = focusList
		a.dashboard.repoConfigForm = nil
		return a, nil

	case tea.KeyPressMsg:
		// When the terminal or config panel has focus, skip all app-level bindings.
		if a.dashboard.panelFocus == focusTerminal || a.dashboard.panelFocus == focusConfig {
			a.confirmQuit = false
			break
		}

		// Enter/right on a repo header: open repo config in right panel.
		if (msg.String() == "enter" || msg.String() == "right") && a.dashboard.panelFocus == focusList {
			item := a.dashboard.selectedItem()
			if item != nil && item.kind == listItemRepo {
				a.initRepoConfigForm(item.repoPath)
				return a, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			// Detach path: save state and exit, preserving worktrees.
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
			// Detach all managers and save state.
			for repoPath, mgr := range a.managers {
				bs := mgr.Detach()
				if bs != nil {
					_ = state.Save(repoPath, bs)
				} else {
					_ = state.Remove(repoPath)
				}
			}
			if a.audioPlayer != nil {
				a.audioPlayer.Close()
			}
			return a, tea.Quit
		case "Q":
			// Force quit: full cleanup, remove state.
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
			for repoPath, mgr := range a.managers {
				mgr.Shutdown()
				_ = state.Remove(repoPath)
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
			resolved := a.resolvedCache[repoPath]
			mgr := a.managers[repoPath]
			if mgr == nil {
				return a, nil
			}
			cfg := agent.Config{
				Rows:              previewH,
				Cols:              previewW,
				BypassPermissions: resolved.BypassPermissions,
				AgentProgram:      resolved.AgentProgram,
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
			resolved := a.resolvedCache[repoPath]
			cfg := agent.Config{
				Rows:              previewH,
				Cols:              previewW,
				BypassPermissions: resolved.BypassPermissions,
				AgentProgram:      resolved.AgentProgram,
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

		case "t":
			// Open or focus a shell terminal in the selected session.
			sess := a.dashboard.selectedSession()
			if sess == nil {
				a.setError("No session selected")
				return a, nil
			}
			if sess.HasShell() {
				// Shell exists — select it and enter focusTerminal.
				for i, item := range a.dashboard.items {
					if item.kind == listItemAgent && item.agent != nil && item.agent.IsShell && item.session == sess {
						a.dashboard.selected = i
						a.dashboard.panelFocus = focusTerminal
						a.resizeSelectedForDashboard()
						break
					}
				}
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
			cfg := agent.Config{
				Rows: previewH,
				Cols: previewW,
			}
			sessionID := sess.ID
			return a, func() tea.Msg {
				ag, err := mgr.AddShell(sessionID, cfg)
				if err != nil {
					return createResultMsg{err: err}
				}
				return createResultMsg{sessionID: sessionID, agentID: ag.ID}
			}

		case "s":
			// Open global settings overlay.
			a.globalConfig = newGlobalConfigModel(a.globalSettings, a.width, a.height)
			a.view = ViewGlobalConfig
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
				a.diff = newDiffModel(sess.GetDisplayName(), files, a.width, a.height-1)
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
			// Check that all non-shell agents in the session are done or idle.
			for _, ag := range sess.Agents() {
				if ag.IsShell {
					continue
				}
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
		return a, func() tea.Msg {
			message := "Merge " + wt.Branch + " into " + wt.BaseBranch
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

// initRepoConfigForm creates a config form for the given repo and enters config focus.
func (a *App) initRepoConfigForm(repoPath string) {
	rs := a.repoSettings[repoPath]
	if rs == nil {
		rs = &config.RepoSettings{}
	}

	bypassPerms := config.DefaultBypassPermissions
	if rs.BypassPermissions != nil {
		bypassPerms = *rs.BypassPermissions
	}
	defaultBranch := ""
	if rs.DefaultBranch != nil {
		defaultBranch = *rs.DefaultBranch
	}
	branchPrefix := ""
	if rs.BranchPrefix != nil {
		branchPrefix = *rs.BranchPrefix
	}
	agentProgram := ""
	if rs.AgentProgram != nil {
		agentProgram = *rs.AgentProgram
	}
	worktreeDir := ""
	if rs.WorktreeDir != nil {
		worktreeDir = *rs.WorktreeDir
	}

	inputWidth := 30
	var fields []formField
	fields = addToggle(fields, "Bypass Permissions", bypassPerms)
	fields = addTextInput(fields, "Default Branch", defaultBranch, "auto-detect", inputWidth)
	fields = addTextInput(fields, "Branch Prefix", branchPrefix, config.DefaultBranchPrefix, inputWidth)
	fields = addTextInput(fields, "Agent Program", agentProgram, config.DefaultAgentProgram, inputWidth)
	fields = addTextInput(fields, "Worktree Directory", worktreeDir, config.DefaultWorktreeDir, inputWidth)

	form := newConfigForm(fields, a.dashboard.previewTermWidth())
	a.dashboard.repoConfigForm = &form
	a.dashboard.configRepoPath = repoPath
	a.dashboard.panelFocus = focusConfig
}

// extractRepoSettings reads form values and creates a RepoSettings struct.
func (a App) extractRepoSettings() *config.RepoSettings {
	form := a.dashboard.repoConfigForm
	if form == nil {
		return &config.RepoSettings{}
	}
	s := &config.RepoSettings{}

	bypassPerms := form.toggleValue("Bypass Permissions")
	s.BypassPermissions = &bypassPerms

	if v := form.textValue("Default Branch"); v != "" {
		s.DefaultBranch = &v
	}
	if v := form.textValue("Branch Prefix"); v != "" {
		s.BranchPrefix = &v
	}
	if v := form.textValue("Agent Program"); v != "" {
		s.AgentProgram = &v
	}
	if v := form.textValue("Worktree Directory"); v != "" {
		s.WorktreeDir = &v
	}
	return s
}

func (a App) updateGlobalConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case globalConfigSaveMsg:
		// Persist global settings.
		if err := config.SaveGlobalSettings(msg.settings); err != nil {
			a.setError(err.Error())
			a.view = ViewDashboard
			return a, nil
		}
		a.globalSettings = msg.settings
		// Rebuild resolved cache and push to all managers.
		for repoPath, rs := range a.repoSettings {
			a.resolvedCache[repoPath] = config.Resolve(a.globalSettings, rs)
			if mgr := a.managers[repoPath]; mgr != nil {
				mgr.UpdateSettings(a.resolvedCache[repoPath])
			}
		}
		a.view = ViewDashboard
		return a, nil
	case globalConfigCancelMsg:
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.globalConfig, cmd = a.globalConfig.Update(msg)
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

	// Remember which repo the cursor is in before rebuilding.
	var prevRepo string
	if a.dashboard.selected >= 0 && a.dashboard.selected < len(a.dashboard.items) {
		prevRepo = a.dashboard.items[a.dashboard.selected].repoPath
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

	// If the selection landed in a different repo, search backward for the
	// nearest item in the original repo (an agent or the repo header).
	if prevRepo != "" && len(items) > 0 && items[a.dashboard.selected].repoPath != prevRepo {
		for i := a.dashboard.selected; i >= 0; i-- {
			if items[i].repoPath == prevRepo && items[i].kind != listItemSession {
				a.dashboard.selected = i
				break
			}
		}
	}
}

func (a App) View() tea.View {
	var content string

	switch a.view {
	case ViewDashboard:
		body := a.dashboard.View()
		hints := dashboardHints
		if a.dashboard.panelFocus == focusTerminal {
			hints = focusTerminalHints
		} else if a.dashboard.panelFocus == focusConfig {
			hints = repoConfigHints
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
	case ViewGlobalConfig:
		content = a.globalConfig.View()
	}

	// Show quit confirmation.
	if a.confirmQuit {
		confirmLine := StyleWarning.Render("Agents are running. q to detach (resume later), Q to quit and clean up, any key to cancel.")
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
