package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	xvt "github.com/charmbracelet/x/vt"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/audio"
	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/git"
	"github.com/devenjarvis/baton/internal/github"
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

// resumeDoneMsg signals that background session resume has completed.
type resumeDoneMsg struct {
	repoPaths []string // repos whose state files should be cleaned up
}

// diffStatsEntry holds cached diff stats for a single session.
type diffStatsEntry struct {
	stats       *diffSummaryData
	lastRefresh time.Time
}

// App is the root Bubble Tea model.
type App struct {
	managers     map[string]*agent.Manager
	activeRepo   string
	cfg          *config.Config
	repoBrowser  fileBrowserModel
	branchPicker branchPickerModel

	// Settings
	globalSettings *config.GlobalSettings
	repoSettings   map[string]*config.RepoSettings    // keyed by repo path
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

	diffStatsCache      map[string]*diffStatsEntry // keyed by session ID
	diffRefreshInFlight bool

	ghClient        *github.Client
	prCache         map[string]*prCacheEntry   // keyed by session ID
	prPollStates    map[string]*prSessionState // keyed by session ID
	prPollsInFlight int                        // count of concurrent in-flight polls
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
		prCache:         make(map[string]*prCacheEntry),
		prPollStates:    make(map[string]*prSessionState),
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
		a.branchPicker.width = msg.Width
		a.branchPicker.height = msg.Height - 1

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
		// Initialize GitHub client (best-effort — nil on failure).
		if ghc, err := github.NewClient(); err == nil {
			a.ghClient = ghc
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
		// Build resume work to run in the background so the TUI renders immediately.
		type resumeItem struct {
			repoPath  string
			mgr       *agent.Manager
			resumeCfg agent.Config
			sessions  []state.SessionState
		}
		var resumeItems []resumeItem
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
			fixedW := a.dashboard.fixedTermWidth()
			fixedH := a.dashboard.fixedTermHeight()
			if fixedW <= 0 {
				fixedW = 80
			}
			if fixedH <= 0 {
				fixedH = 24
			}
			resumeItems = append(resumeItems, resumeItem{
				repoPath: repo.Path,
				mgr:      mgr,
				resumeCfg: agent.Config{
					Rows:              fixedH,
					Cols:              fixedW,
					BypassPermissions: resolved.BypassPermissions,
					AgentProgram:      resolved.AgentProgram,
				},
				sessions: bs.Sessions,
			})
		}
		if len(resumeItems) > 0 {
			cmds = append(cmds, func() tea.Msg {
				var wg sync.WaitGroup
				for _, ri := range resumeItems {
					for _, ss := range ri.sessions {
						wg.Add(1)
						go func(mgr *agent.Manager, ss state.SessionState, cfg agent.Config) {
							defer wg.Done()
							_ = mgr.ResumeSession(ss, cfg)
						}(ri.mgr, ss, ri.resumeCfg)
					}
				}
				wg.Wait()
				repoPaths := make([]string, len(resumeItems))
				for i, ri := range resumeItems {
					repoPaths[i] = ri.repoPath
				}
				return resumeDoneMsg{repoPaths: repoPaths}
			})
		}
		// Always start on the dashboard — single repo or many.
		a.view = ViewDashboard
		a.refreshAgentList()
		a.updateDashboardPRCache()
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

			// Check for Active->Idle transition (preserves downstream side
			// effects like diff-stats refresh). The chime trigger below uses
			// a separate frame-stability signal rather than the transition.
			if prev, ok := a.lastKnownStatus[ag.ID]; ok {
				if prev == agent.StatusActive && currentStatus == agent.StatusIdle && ag.HasReceivedInput() && !ag.IsShell {
					idleTransition = true
				}
			}
			a.lastKnownStatus[ag.ID] = currentStatus

			// Chime trigger: fire once per turn when the agent is truly
			// awaiting input. Primary signal is frame stability (screen
			// unchanged for VisualStabilityWindow); fallback is prolonged
			// silence (no PTY output for StuckFallbackTimeout).
			if !ag.IsShell && ag.HasReceivedInput() && !ag.ChimedForTurn() &&
				(currentStatus == agent.StatusActive || currentStatus == agent.StatusIdle) &&
				(ag.VisualStableFor() >= agent.VisualStabilityWindow ||
					ag.TimeSinceOutput() >= agent.StuckFallbackTimeout) {
				resolved := a.resolvedCache[item.repoPath]
				if resolved.AudioEnabled && a.audioPlayer != nil {
					a.audioPlayer.Play()
					ag.MarkChimedForTurn()
				}
			}

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
		a.cleanStaleCaches()
		// Refresh diff stats periodically or on idle transition.
		var diffCmd tea.Cmd
		if sess := a.dashboard.selectedSession(); sess != nil {
			entry := a.diffStatsCache[sess.ID]
			stale := entry == nil || time.Since(entry.lastRefresh) > 5*time.Second
			if (stale || idleTransition) && !a.diffRefreshInFlight {
				diffCmd = a.refreshDiffStatsCmd()
			}
		}
		// Adaptive per-session PR polling.
		var prCmds []tea.Cmd
		if a.ghClient != nil {
			prCmds = a.pollAllSessions()
		}
		allCmds := append([]tea.Cmd{tickCmd(), diffCmd}, prCmds...)
		return a, tea.Batch(allCmds...)

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

	case prPollMsg:
		a.prPollsInFlight--
		if a.prPollsInFlight < 0 {
			a.prPollsInFlight = 0
		}
		ps := a.prPollStates[msg.sessionID]
		if ps != nil {
			ps.inFlight = false
		}
		// Only update cache if we got data; preserve existing cache on errors.
		if msg.pr != nil {
			a.prCache[msg.sessionID] = &prCacheEntry{
				pr:      msg.pr,
				checks:  msg.checks,
				reviews: msg.reviews,
			}
			// Detect check state transitions and fire notifications.
			if ps != nil && msg.checks != nil {
				prevState := ps.lastCheckState
				newState := msg.checks.State
				if prevState == "pending" && (newState == "success" || newState == "failure") {
					// Flash the session row.
					ps.flashUntil = time.Now().Add(2 * time.Second)
					if newState == "success" {
						ps.flashColor = "success"
					} else {
						ps.flashColor = "error"
					}
					// Play audio notification, gated by the session's repo AudioEnabled setting
					// (same gate as the idle-transition notification above).
					if a.audioPlayer != nil {
						repoPath := a.repoPathForSession(msg.sessionID)
						if repoPath != "" && a.resolvedCache[repoPath].AudioEnabled {
							a.audioPlayer.Play()
						}
					}
				}
				ps.lastCheckState = newState
			}
			a.updateDashboardPRCache()
		}
		return a, nil

	case prCreateMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
			return a, nil
		}
		if msg.pr != nil {
			a.prCache[msg.sessionID] = &prCacheEntry{pr: msg.pr}
			a.updateDashboardPRCache()
		}
		return a, nil

	case resumeDoneMsg:
		for _, repoPath := range msg.repoPaths {
			_ = state.Remove(repoPath)
		}
		a.refreshAgentList()
		return a, nil

	case fixChecksMsg:
		if msg.err != nil {
			a.setError(msg.err.Error())
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
	case ViewBranchPicker:
		return a.updateBranchPicker(msg)
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
			fixedW := a.dashboard.fixedTermWidth()
			fixedH := a.dashboard.fixedTermHeight()
			if fixedW <= 0 || fixedH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			resolved := a.resolvedCache[repoPath]
			mgr := a.managers[repoPath]
			if mgr == nil {
				return a, nil
			}
			cfg := agent.Config{
				Rows:              fixedH,
				Cols:              fixedW,
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
			fixedW := a.dashboard.fixedTermWidth()
			fixedH := a.dashboard.fixedTermHeight()
			if fixedW <= 0 || fixedH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			resolved := a.resolvedCache[repoPath]
			cfg := agent.Config{
				Rows:              fixedH,
				Cols:              fixedW,
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

		case "o":
			// Open branch picker to create session on existing branch/PR.
			repoPath := a.dashboard.selectedRepoPath()
			if repoPath == "" {
				repoPath = a.activeRepo
			}
			if repoPath == "" {
				a.setError("No repo available")
				return a, nil
			}
			// Build set of branches that already have active sessions.
			mgr := a.managers[repoPath]
			activeBranches := make(map[string]bool)
			if mgr != nil {
				for _, sess := range mgr.ListSessions() {
					activeBranches[sess.Worktree.Branch] = true
				}
			}
			a.branchPicker = newBranchPickerModel()
			a.branchPicker.width = a.width
			a.branchPicker.height = a.height - 1
			a.activeRepo = repoPath
			a.view = ViewBranchPicker
			return a, loadBranchPickerData(repoPath, a.ghClient, activeBranches)

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
			fixedW := a.dashboard.fixedTermWidth()
			fixedH := a.dashboard.fixedTermHeight()
			if fixedW <= 0 || fixedH <= 0 {
				a.setError("Terminal size not yet known; try again")
				return a, nil
			}
			cfg := agent.Config{
				Rows: fixedH,
				Cols: fixedW,
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
				// Always enter the diff view; the empty state is handled by
				// the renderer when there are no files.
				files := git.ParseDiffFiles(rawDiff)
				a.view = ViewDiff
				a.diff = newDiffModel(sess.GetDisplayName(), files, a.width, a.height-1)
				return a, nil
			}
			// Repo header selected — remove the repo.
			if a.cfg != nil {
				_ = config.RemoveRepo(a.cfg, item.repoPath)
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

		case "p":
			// Create PR or open existing PR URL.
			if a.ghClient == nil {
				a.setError("GitHub not configured (install gh CLI or set GITHUB_TOKEN)")
				return a, nil
			}
			sess := a.dashboard.selectedSession()
			if sess == nil {
				a.setError("No session selected")
				return a, nil
			}
			repoPath := a.dashboard.selectedRepoPath()
			if repoPath == "" {
				return a, nil
			}
			// If session already has a PR, open it in browser.
			if entry := a.prCache[sess.ID]; entry != nil && entry.pr != nil {
				url := entry.pr.URL
				go func() {
					switch runtime.GOOS {
					case "darwin":
						_ = exec.Command("open", url).Run()
					case "linux":
						_ = exec.Command("xdg-open", url).Run()
					default:
						_ = exec.Command("xdg-open", url).Run()
					}
				}()
				return a, nil
			}
			// Otherwise, push branch and create PR.
			sessionID := sess.ID
			branch := sess.Worktree.Branch
			baseBranch := sess.Worktree.BaseBranch
			title := sess.GetDisplayName()
			ghClient := a.ghClient
			return a, func() tea.Msg {
				// Push branch first.
				if err := git.PushBranch(repoPath, branch); err != nil {
					return prCreateMsg{sessionID: sessionID, err: err}
				}
				// Parse remote to get owner/repo.
				rawURL, err := git.GetRemoteURL(repoPath)
				if err != nil {
					return prCreateMsg{sessionID: sessionID, err: err}
				}
				owner, repo, err := github.ParseRemoteURL(rawURL)
				if err != nil {
					return prCreateMsg{sessionID: sessionID, err: err}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				pr, err := ghClient.CreatePR(ctx, owner, repo, branch, baseBranch, title, "")
				return prCreateMsg{sessionID: sessionID, pr: pr, err: err}
			}

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
			// If session has a PR, merge via GitHub API.
			if entry := a.prCache[sess.ID]; entry != nil && entry.pr != nil {
				a.merge.viaPR = true
				a.merge.prNumber = entry.pr.Number
			}
			a.merge.width = a.width
			a.merge.height = a.height
			return a, nil

		case "f":
			// Fix failing checks: fetch logs and send to an idle agent.
			if a.ghClient == nil {
				a.setError("GitHub not configured")
				return a, nil
			}
			sess := a.dashboard.selectedSession()
			if sess == nil {
				a.setError("No session selected")
				return a, nil
			}
			entry := a.prCache[sess.ID]
			if entry == nil || entry.pr == nil {
				a.setError("No PR for this session")
				return a, nil
			}
			if entry.checks == nil || entry.checks.Failed == 0 {
				a.setError("No failing checks")
				return a, nil
			}
			// Find first idle agent in the session.
			var targetAgent *agent.Agent
			for _, ag := range sess.Agents() {
				if ag.IsShell {
					continue
				}
				if ag.Status() == agent.StatusIdle {
					targetAgent = ag
					break
				}
			}
			if targetAgent == nil {
				a.setError("No idle agent available to fix checks")
				return a, nil
			}
			repoPath := a.dashboard.selectedRepoPath()
			sessionID := sess.ID
			prNumber := entry.pr.Number
			failedChecks := entry.checks.FailedChecks
			ghClient := a.ghClient
			ag := targetAgent
			return a, func() tea.Msg {
				rawURL, err := git.GetRemoteURL(repoPath)
				if err != nil {
					return fixChecksMsg{sessionID: sessionID, err: err}
				}
				owner, repo, err := github.ParseRemoteURL(rawURL)
				if err != nil {
					return fixChecksMsg{sessionID: sessionID, err: err}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				var msgBuilder strings.Builder
				msgBuilder.WriteString(fmt.Sprintf("The following CI checks are failing on PR #%d. Please analyze the failures and fix them:\n\n", prNumber))

				for _, fc := range failedChecks {
					logs, err := ghClient.GetFailedCheckLogs(ctx, owner, repo, fc.ID)
					if err != nil {
						logs = "(failed to fetch logs: " + err.Error() + ")"
					}
					// Truncate very long logs.
					if len(logs) > 4000 {
						logs = logs[:4000] + "\n...(truncated)"
					}
					msgBuilder.WriteString(fmt.Sprintf("## %s\n%s\n\n", fc.Name, logs))
				}

				message := msgBuilder.String()
				ag.SendText(message)
				// Send Enter key to submit.
				ag.SendKey(xvt.KeyPressEvent{Code: tea.KeyEnter})

				return fixChecksMsg{sessionID: sessionID}
			}
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
				}
			} else if msg.X >= 32 {
				// Preview panel click — enter focusTerminal if an agent is selected.
				// X==31 is the list panel's right border and is intentionally ignored.
				if a.dashboard.panelFocus == focusList && a.dashboard.selectedAgent() != nil {
					a.dashboard.panelFocus = focusTerminal
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
	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.Update(msg)
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

func (a App) updateBranchPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchPickerSelectMsg:
		a.view = ViewDashboard
		item := msg.item

		repoPath := a.activeRepo
		mgr := a.managers[repoPath]
		if mgr == nil {
			a.setError("No manager for repo")
			return a, nil
		}

		fixedW := a.dashboard.fixedTermWidth()
		fixedH := a.dashboard.fixedTermHeight()
		if fixedW <= 0 || fixedH <= 0 {
			a.setError("Terminal size not yet known; try again")
			return a, nil
		}

		resolved := a.resolvedCache[repoPath]
		cfg := agent.Config{
			Rows:              fixedH,
			Cols:              fixedW,
			BypassPermissions: resolved.BypassPermissions,
			AgentProgram:      resolved.AgentProgram,
		}

		branch := item.branch
		baseBranch := item.baseBranch
		return a, func() tea.Msg {
			sess, ag, err := mgr.CreateSessionOnBranch(branch, baseBranch, cfg)
			if err != nil {
				return createResultMsg{err: err}
			}
			return createResultMsg{sessionID: sess.ID, agentID: ag.ID}
		}

	case branchPickerCancelMsg:
		a.view = ViewDashboard
		return a, nil
	}

	var cmd tea.Cmd
	a.branchPicker, cmd = a.branchPicker.Update(msg)
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
		viaPR := a.merge.viaPR
		prNumber := a.merge.prNumber
		ghClient := a.ghClient
		return a, func() tea.Msg {
			if viaPR && ghClient != nil {
				// Merge via GitHub API.
				rawURL, err := git.GetRemoteURL(activeRepo)
				if err != nil {
					return mergeCompleteMsg{err: err}
				}
				owner, repo, err := github.ParseRemoteURL(rawURL)
				if err != nil {
					return mergeCompleteMsg{err: err}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := ghClient.MergePR(ctx, owner, repo, prNumber); err != nil {
					return mergeCompleteMsg{err: err}
				}
				return mergeCompleteMsg{}
			}
			// Local git merge.
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
			delete(a.prCache, sessID)
			delete(a.prPollStates, sessID)
			a.refreshAgentList()
			a.updateDashboardDiffStats()
			a.updateDashboardPRCache()
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
	w := a.dashboard.fixedTermWidth()
	h := a.dashboard.fixedTermHeight()
	if w > 0 && h > 0 {
		ag.Resize(h, w)
	}
}

// resizeAllForDashboard resizes every agent to the dashboard preview dimensions.
// Called on WindowSizeMsg so all agents match the new terminal size.
func (a *App) resizeAllForDashboard() {
	w := a.dashboard.fixedTermWidth()
	h := a.dashboard.fixedTermHeight()
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
	items := make([]listItem, 0, len(a.cfg.Repos))
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
		switch a.dashboard.panelFocus {
		case focusTerminal:
			hints = focusTerminalHints
		case focusConfig:
			hints = repoConfigHints
		}
		statusbar := renderStatusBar(hints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewDiff:
		body := a.diff.View()
		hints := diffSummaryHints
		if a.diff.Mode() == detailMode {
			hints = diffDetailHints
		}
		statusbar := renderStatusBar(hints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewMerge:
		content = a.merge.View()
	case ViewFileBrowser:
		body := a.repoBrowser.View()
		statusbar := renderStatusBar(repoBrowsingHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
	case ViewGlobalConfig:
		content = a.globalConfig.View()
	case ViewBranchPicker:
		body := a.branchPicker.View()
		statusbar := renderStatusBar(branchPickerHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
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

// updateDashboardPRCache passes the PR cache and poll states to the dashboard for rendering.
func (a *App) updateDashboardPRCache() {
	a.dashboard.prCache = a.prCache
	a.dashboard.prPollStates = a.prPollStates
}

// repoPathForSession returns the repo path containing the given session, or "" if not found.
func (a *App) repoPathForSession(sessionID string) string {
	for _, repo := range a.cfg.Repos {
		mgr := a.managers[repo.Path]
		if mgr == nil {
			continue
		}
		for _, sess := range mgr.ListSessions() {
			if sess.ID == sessionID {
				return repo.Path
			}
		}
	}
	return ""
}

// cleanStaleCaches removes diff stats and PR cache entries for sessions that no longer exist.
func (a *App) cleanStaleCaches() {
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
	for id := range a.prCache {
		if !activeSessions[id] {
			delete(a.prCache, id)
		}
	}
	for id := range a.prPollStates {
		if !activeSessions[id] {
			delete(a.prPollStates, id)
		}
	}
}

// pollAllSessions returns Cmds for sessions that are due for a PR status poll.
// It respects adaptive intervals and limits concurrent in-flight polls.
func (a *App) pollAllSessions() []tea.Cmd {
	const (
		maxConcurrent    = 3
		shaCheckInterval = 2 * time.Second
	)

	var cmds []tea.Cmd
	now := time.Now()

outer:
	for _, repo := range a.cfg.Repos {
		mgr := a.managers[repo.Path]
		if mgr == nil {
			continue
		}
		for _, sess := range mgr.ListSessions() {
			if a.prPollsInFlight >= maxConcurrent {
				break outer
			}

			ps := a.prPollStates[sess.ID]
			if ps == nil {
				ps = &prSessionState{}
				a.prPollStates[sess.ID] = ps
			}
			if ps.inFlight {
				continue
			}

			// Determine adaptive polling interval.
			interval := a.prPollInterval(sess.ID, ps)
			if now.Sub(ps.lastPoll) < interval {
				// Check for push detection: if no PR yet, see if the remote SHA changed.
				// Throttle git rev-parse calls to at most once every shaCheckInterval per
				// session to avoid blocking the Bubble Tea main goroutine on every tick.
				if a.prCache[sess.ID] == nil || a.prCache[sess.ID].pr == nil {
					if now.Sub(ps.lastSHACheck) < shaCheckInterval {
						continue
					}
					ps.lastSHACheck = now
					sha := getRemoteSHA(repo.Path, sess.Worktree.Branch)
					if sha != "" && sha != ps.lastRemoteSHA {
						ps.lastRemoteSHA = sha
						// Push detected — fall through to schedule an immediate poll.
					} else {
						continue
					}
				} else {
					continue
				}
			}

			ps.lastPoll = now
			ps.inFlight = true
			a.prPollsInFlight++
			cmds = append(cmds, a.refreshPRStatusForSession(sess.ID, sess.Worktree.Branch, repo.Path))
		}
	}
	return cmds
}

// prPollInterval returns the adaptive polling interval for a session.
func (a *App) prPollInterval(sessionID string, ps *prSessionState) time.Duration {
	entry := a.prCache[sessionID]
	// No PR found yet but branch may have been pushed.
	if entry == nil || entry.pr == nil {
		if ps.lastRemoteSHA != "" {
			return 10 * time.Second // branch pushed, waiting for PR
		}
		return 30 * time.Second // stable, no activity
	}
	// PR exists — adapt based on check state.
	if entry.checks != nil && entry.checks.State == "pending" {
		return 5 * time.Second
	}
	return 30 * time.Second
}

// getRemoteSHA runs `git rev-parse origin/<branch>` to detect pushes.
// Returns empty string on any error.
func getRemoteSHA(repoPath, branch string) string {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "origin/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// refreshPRStatusForSession returns a Cmd that polls PR, check, and review status for a single session.
func (a *App) refreshPRStatusForSession(sessionID, branch, repoPath string) tea.Cmd {
	ghClient := a.ghClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		rawURL, err := git.GetRemoteURL(repoPath)
		if err != nil {
			return prPollMsg{sessionID: sessionID}
		}
		owner, repo, err := github.ParseRemoteURL(rawURL)
		if err != nil {
			return prPollMsg{sessionID: sessionID}
		}

		pr, _ := ghClient.GetPR(ctx, owner, repo, branch)
		var checks *github.CheckStatus
		var reviews *github.ReviewStatus
		if pr != nil {
			checks, _ = ghClient.GetChecks(ctx, owner, repo, branch)
			reviews, _ = ghClient.GetReviews(ctx, owner, repo, pr.Number)
		}

		return prPollMsg{
			sessionID: sessionID,
			pr:        pr,
			checks:    checks,
			reviews:   reviews,
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
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return // best-effort
	}
	defer func() { _ = f.Close() }()

	// Add newline before entry if file doesn't end with one.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(entry + "\n")
}
