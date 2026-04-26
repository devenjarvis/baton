package tui

import (
	"bufio"
	"context"
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
	"github.com/devenjarvis/baton/internal/diffmodel"
	"github.com/devenjarvis/baton/internal/git"
	"github.com/devenjarvis/baton/internal/github"
	"github.com/devenjarvis/baton/internal/state"
	"github.com/devenjarvis/baton/internal/vt"
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

// killScope distinguishes an agent-level kill from a session-level kill so the
// result handler knows which closing set to clean up.
type killScope int

const (
	killScopeAgent killScope = iota
	killScopeSession
)

// killResultMsg carries the result of an async KillAgent/KillSession call.
// agentID is empty for session-scoped kills; the closing-set cleanup iterates
// the manager to find stale IDs instead.
type killResultMsg struct {
	scope     killScope
	sessionID string
	agentID   string
	agentIDs  []string // for session scope: all agent IDs that were in the session
	err       error
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
	globalConfig globalConfigModel

	width       int
	height      int
	err         string
	errTicks    int // ticks remaining to show error
	confirmQuit bool

	lastKnownStatus map[string]agent.Status
	audioPlayer     *audio.Player

	// closingAgents and closingSessions track in-flight kill requests so the
	// dashboard can render a "closing…" indicator while the async teardown runs.
	// Lives in the TUI because it's purely a UI concern.
	closingAgents   map[string]bool
	closingSessions map[string]bool

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
		closingAgents:   make(map[string]bool),
		closingSessions: make(map[string]bool),
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
		a.diff, _ = a.diff.Update(tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 1})
		a.repoBrowser.width = msg.Width
		a.repoBrowser.height = msg.Height - 1
		a.branchPicker.width = msg.Width
		a.branchPicker.height = msg.Height - 1
		a.recomputePRSectionY()
		// A resize remaps the VT viewport — any in-flight selection is now
		// pointing at stale cells. Drop it.
		a.dashboard.clearSelection()

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
		a.dashboard.sidebarWidth = config.Resolve(a.globalSettings, nil).SidebarWidth

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
		// Detect Active->Idle transitions for diff-stats refresh. Chime
		// notifications are fired in the EventStatusChanged handler below,
		// which reacts the instant Claude's Stop hook arrives.
		idleTransition := false
		for _, item := range a.dashboard.items {
			if item.kind != listItemAgent || item.agent == nil {
				continue
			}
			ag := item.agent
			currentStatus := ag.Status()
			if prev, ok := a.lastKnownStatus[ag.ID]; ok {
				if prev == agent.StatusActive && currentStatus == agent.StatusIdle && !ag.IsShell {
					idleTransition = true
				}
			}
			a.lastKnownStatus[ag.ID] = currentStatus
		}
		// Detect alt-screen transitions and trigger a resize so Claude's TUI
		// redraws cleanly (replaces the old splashResizeMsg delayed timer).
		fixedW := a.dashboard.fixedTermWidth()
		fixedH := a.dashboard.fixedTermHeight()
		if fixedW > 0 && fixedH > 0 {
			selected := a.dashboard.selectedAgent()
			for _, item := range a.dashboard.items {
				if item.kind != listItemAgent || item.agent == nil {
					continue
				}
				if item.agent.AltScreenEntered() {
					item.agent.Resize(fixedH, fixedW)
					// VT history is cleared on alt-screen entry; any prior
					// scrollOffset now indexes into an empty buffer and would
					// leave the preview visually frozen until the user hits
					// home. Snap the currently focused agent back to live.
					if item.agent == selected {
						a.dashboard.scrollOffset = 0
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
		// Chime when Claude's Stop hook arrives (EventStatusChanged with Idle).
		// Gated only by ChimedForTurn + HasReceivedInput: ChimedForTurn is reset
		// on Enter (Agent.SendKey), so once-per-turn semantics are enforced there
		// rather than by re-reading lastKnownStatus. This avoids a race with the
		// 100ms tickMsg: a tick landing between the manager mutating status and
		// the TUI dequeuing this event would otherwise clobber the cached "prev
		// was Active" signal and silently suppress the chime.
		if msg.event.Type == agent.EventStatusChanged {
			// Chime on both Idle (Claude finished its turn) and Waiting
			// (Claude needs user input). ChimedForTurn is the shared gate:
			// whichever fires first in a turn wins, and the other is
			// silently skipped. The flag resets on Enter or UserPromptSubmit.
			if msg.event.Status == agent.StatusIdle || msg.event.Status == agent.StatusWaiting {
				if mgr := a.managers[msg.repoPath]; mgr != nil {
					if ag := mgr.Get(msg.event.AgentID); ag != nil && !ag.IsShell {
						if ag.HasReceivedInput() && !ag.ChimedForTurn() {
							resolved := a.resolvedCache[msg.repoPath]
							if resolved.AudioEnabled && a.audioPlayer != nil {
								a.audioPlayer.Play()
								ag.MarkChimedForTurn()
							}
						}
					}
				}
			}
			a.lastKnownStatus[msg.event.AgentID] = msg.event.Status
		}
		// Branch rename invalidates any PR-by-branch lookup. Schedule a burst of
		// short-interval polls so the SHA-based lookup can rediscover the PR
		// quickly — do NOT clear the cache here; that happens only when the
		// next poll confirms the PR is gone (handled in prPollMsg).
		if msg.event.Type == agent.EventBranchRenamed && msg.event.SessionID != "" {
			ps := a.prPollStates[msg.event.SessionID]
			if ps == nil {
				ps = &prSessionState{}
				a.prPollStates[msg.event.SessionID] = ps
			}
			ps.burstUntil = time.Now().Add(60 * time.Second)
			ps.lastPoll = time.Time{}
			// Clearing lastRemoteSHA forces getRemoteSHA to re-check against
			// the new branch name on the next tick instead of comparing against
			// the SHA it read under the old branch.
			ps.lastRemoteSHA = ""
			ps.lastSHACheck = time.Time{}
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
		// Set initial dimensions before any output arrives. The alt-screen
		// transition detector in the tick handler will fire a follow-up resize
		// once Claude's TUI enters alternate screen mode.
		a.resizeSelectedForDashboard()
		return a, nil

	case killResultMsg:
		// Clean up closing-set entries regardless of error so the UI never
		// gets stuck rendering "closing…" on a row whose kill failed.
		switch msg.scope {
		case killScopeAgent:
			delete(a.closingAgents, msg.agentID)
			delete(a.lastKnownStatus, msg.agentID)
		case killScopeSession:
			delete(a.closingSessions, msg.sessionID)
			for _, id := range msg.agentIDs {
				delete(a.closingAgents, id)
				delete(a.lastKnownStatus, id)
			}
			delete(a.diffStatsCache, msg.sessionID)
		}
		if msg.err != nil {
			a.setError(msg.err.Error())
		}
		a.refreshAgentList()
		a.updateDashboardDiffStats()
		return a, nil

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
		// Fetch failed: preserve cache so a transient error doesn't blank the UI.
		if msg.err != nil {
			return a, nil
		}
		// Lookup succeeded with no PR. If we had one before, the PR has been
		// closed, merged, or its head branch was deleted — drop the stale entry
		// so the UI reflects reality.
		if msg.pr == nil {
			if _, had := a.prCache[msg.sessionID]; had {
				delete(a.prCache, msg.sessionID)
				if ps != nil {
					ps.lastCheckState = ""
				}
				a.updateDashboardPRCache()
			}
			return a, nil
		}
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
		return a, nil

	case resumeDoneMsg:
		for _, repoPath := range msg.repoPaths {
			_ = state.Remove(repoPath)
		}
		a.refreshAgentList()
		return a, nil

	}

	// Route to the active view.
	switch a.view {
	case ViewDashboard:
		return a.updateDashboard(msg)
	case ViewDiff:
		return a.updateDiff(msg)
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
			alias := strings.TrimSpace(a.dashboard.repoConfigForm.textValue("Alias"))
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
			for i, r := range a.cfg.Repos {
				if r.Path == repoPath && r.Alias != alias {
					a.cfg.Repos[i].Alias = alias
					if err := config.Save(a.cfg); err != nil {
						a.setError(err.Error())
					}
					a.refreshAgentList()
					break
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

		case "e":
			// Open the selected session's worktree in the configured IDE.
			sess := a.dashboard.selectedSession()
			if sess == nil {
				a.setError("No session selected")
				return a, nil
			}
			repoPath := a.dashboard.selectedRepoPath()
			ideCmd := strings.TrimSpace(a.resolvedCache[repoPath].IDECommand)
			if ideCmd == "" {
				a.setError("No IDE configured (set 'IDE Command' in settings)")
				return a, nil
			}
			parts := splitIDECommand(ideCmd)
			if len(parts) == 0 {
				a.setError("No IDE configured (set 'IDE Command' in settings)")
				return a, nil
			}
			worktreePath := sess.Worktree.Path
			exe := parts[0]
			args := append(parts[1:], worktreePath)
			go func() {
				cmd := exec.Command(exe, args...)
				cmd.Dir = worktreePath
				_ = cmd.Start()
			}()
			return a, nil

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
					activeBranches[sess.Branch()] = true
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

		case "p":
			// Open the selected session's PR in the browser.
			sess := a.dashboard.selectedSession()
			if sess != nil {
				if entry := a.prCache[sess.ID]; entry != nil && entry.pr != nil && entry.pr.URL != "" {
					if err := openURL(entry.pr.URL); err != nil {
						a.setError(err.Error())
					}
				}
			}
			return a, nil

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
				m, err := diffmodel.Parse(rawDiff)
				if err != nil {
					a.setError(err.Error())
					return a, nil
				}
				a.view = ViewDiff
				a.diff = newDiffModel(sess.GetDisplayName(), m, a.width, a.height-1)
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
			// Kill the selected agent asynchronously so the UI stays responsive.
			item := a.dashboard.selectedItem()
			if item == nil || item.kind != listItemAgent || item.agent == nil || item.session == nil {
				return a, nil
			}
			mgr := a.managers[item.repoPath]
			if mgr == nil {
				return a, nil
			}
			agentID := item.agent.ID
			sessionID := item.session.ID
			// Already dispatched — no-op to avoid double-kills.
			if a.closingAgents[agentID] {
				return a, nil
			}
			a.closingAgents[agentID] = true
			a.refreshAgentList()
			return a, func() tea.Msg {
				err := mgr.KillAgent(sessionID, agentID)
				return killResultMsg{
					scope:     killScopeAgent,
					sessionID: sessionID,
					agentID:   agentID,
					err:       err,
				}
			}

		case "X":
			// Kill the entire parent session of the selected agent asynchronously.
			item := a.dashboard.selectedItem()
			if item == nil || item.session == nil {
				return a, nil
			}
			mgr := a.managers[item.repoPath]
			if mgr == nil {
				return a, nil
			}
			sessID := item.session.ID
			// Already dispatched — no-op.
			if a.closingSessions[sessID] {
				return a, nil
			}
			var agentIDs []string
			for _, ag := range item.session.Agents() {
				agentIDs = append(agentIDs, ag.ID)
				a.closingAgents[ag.ID] = true
			}
			a.closingSessions[sessID] = true
			a.refreshAgentList()
			return a, func() tea.Msg {
				err := mgr.KillSession(sessID)
				return killResultMsg{
					scope:     killScopeSession,
					sessionID: sessID,
					agentIDs:  agentIDs,
					err:       err,
				}
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
				contentY := itemIndex // content-relative, same space as prSectionY
				if a.dashboard.prSectionY >= 0 && contentY >= a.dashboard.prSectionY {
					// Click in the PR checks summary section — open PR in browser.
					sess := a.dashboard.selectedSession()
					if sess != nil {
						if entry := a.prCache[sess.ID]; entry != nil && entry.pr != nil && entry.pr.URL != "" {
							if err := openURL(entry.pr.URL); err != nil {
								a.setError(err.Error())
							}
						}
					}
				} else if itemIndex >= 0 && itemIndex < len(a.dashboard.items) {
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
				// Seed a fresh selection if the click landed inside the agent's
				// VT viewport. dragSeen=false until a subsequent motion event
				// confirms an actual drag — a click without drag should not
				// produce a 1-cell selection.
				if a.dashboard.panelFocus == focusTerminal {
					if ag := a.dashboard.selectedAgent(); ag != nil {
						if termX, termY, inVP := a.screenToTermCell(msg.X, msg.Y); inVP {
							a.dashboard.selection = selection{
								anchorX: termX,
								anchorY: termY,
								cursorX: termX,
								cursorY: termY,
								active:  true,
								agentID: ag.ID,
							}
						} else {
							// Click outside the viewport (e.g., on the border)
							// drops any prior selection — matches what the user
							// expects from "click somewhere else".
							a.dashboard.clearSelection()
						}
					}
				}
			}
		}
		return a, nil
	}

	if msg, ok := msg.(tea.MouseMotionMsg); ok {
		// Drag updates the cursor end of an in-flight selection. A motion
		// event with the left button still held is the only signal we have
		// that the user is dragging — bubbletea's MouseModeCellMotion gives
		// us these while a button is down.
		if a.dashboard.selection.active && msg.Button == tea.MouseLeft {
			termX, termY, _ := a.screenToTermCell(msg.X, msg.Y)
			if w := a.dashboard.fixedTermWidth(); w > 0 {
				if termX < 0 {
					termX = 0
				} else if termX >= w {
					termX = w - 1
				}
			}
			if h := a.dashboard.fixedTermHeight(); h > 0 {
				if termY < 0 {
					termY = 0
				} else if termY >= h {
					termY = h - 1
				}
			}
			a.dashboard.selection.cursorX = termX
			a.dashboard.selection.cursorY = termY
			if termX != a.dashboard.selection.anchorX || termY != a.dashboard.selection.anchorY {
				a.dashboard.selection.dragSeen = true
			}
		}
		return a, nil
	}

	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		if a.dashboard.selection.active {
			if a.dashboard.selection.dragSeen {
				// Real drag — copy the highlighted region. The highlight
				// stays on screen until the next click clears or replaces it.
				if ag := a.dashboard.selectedAgent(); ag != nil && ag.ID == a.dashboard.selection.agentID {
					if sx, sy, ex, ey, ok := a.dashboard.selectionRect(); ok {
						text := ag.ExtractText(vt.SelectionRect{
							StartX: sx, StartY: sy, EndX: ex, EndY: ey, Active: true,
						})
						if text != "" {
							return a, tea.SetClipboard(text)
						}
					}
				}
			} else {
				// Plain click — drop the seeded selection. Focus already moved
				// in the click handler.
				a.dashboard.clearSelection()
			}
		}
		return a, nil
	}

	if msg, ok := msg.(tea.MouseWheelMsg); ok {
		if a.dashboard.panelFocus == focusTerminal {
			ag := a.dashboard.selectedAgent()
			if ag != nil {
				// Alt-screen apps (Claude's /tui fullscreen, vim, less) redraw
				// the viewport instead of scrolling — baton's scrollback is
				// inert for them. Forward the wheel event so the app can drive
				// its own scrollback. SendMouse is a no-op unless the app has
				// enabled mouse reporting.
				if ag.IsAltScreen() {
					a.forwardWheelToAgent(ag, msg)
					return a, nil
				}
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
		a.recomputePRSectionY()
		a.updateDashboardDiffStats()
		if sess := a.dashboard.selectedSession(); sess != nil {
			entry := a.diffStatsCache[sess.ID]
			if (entry == nil || time.Since(entry.lastRefresh) > 5*time.Second) && !a.diffRefreshInFlight {
				diffCmd := a.refreshDiffStatsCmd()
				return a, tea.Batch(cmd, diffCmd)
			}
		}
	}
	// Maintain the invariant: a text selection only persists while the user
	// remains focused on the same agent's terminal. Any focus or agent change
	// (sidebar nav, esc, click on the list, etc.) drops it.
	if a.dashboard.selection.active {
		ag := a.dashboard.selectedAgent()
		if a.dashboard.panelFocus != focusTerminal || ag == nil || ag.ID != a.dashboard.selection.agentID {
			a.dashboard.clearSelection()
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
	ideCommand := ""
	if rs.IDECommand != nil {
		ideCommand = *rs.IDECommand
	}
	worktreeDir := ""
	if rs.WorktreeDir != nil {
		worktreeDir = *rs.WorktreeDir
	}
	alias := ""
	for _, r := range a.cfg.Repos {
		if r.Path == repoPath {
			alias = r.Alias
			break
		}
	}

	inputWidth := 30
	var fields []formField
	fields = addTextInput(fields, "Alias", alias, "short nickname", inputWidth)
	fields = addToggle(fields, "Bypass Permissions", bypassPerms)
	fields = addTextInput(fields, "Default Branch", defaultBranch, "auto-detect", inputWidth)
	fields = addTextInput(fields, "Branch Prefix", branchPrefix, config.DefaultBranchPrefix, inputWidth)
	fields = addTextInput(fields, "Agent Program", agentProgram, config.DefaultAgentProgram, inputWidth)
	fields = addEditorFields(fields, ideCommand, inputWidth)
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
	if v := extractIDECommand(*form); v != "" {
		s.IDECommand = &v
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
		newSidebarWidth := config.Resolve(a.globalSettings, nil).SidebarWidth
		if newSidebarWidth != a.dashboard.sidebarWidth {
			a.dashboard.sidebarWidth = newSidebarWidth
			a.resizeAllForDashboard()
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

// screenToTermCell converts a screen-space mouse coordinate to a VT cell
// coordinate inside the agent preview viewport. inViewport is false when the
// point lies outside the viewport rectangle — callers that want clamping
// should clamp to [0, fixedTermWidth) × [0, fixedTermHeight) themselves.
//
// The translation mirrors the dashboard layout: an optional error/confirm-quit
// banner pushes content down, the list panel and its border occupy the first
// 32 columns, and the preview's lipgloss frame plus the metadata rows above
// the VT viewport offset the top-left cell.
func (a *App) screenToTermCell(screenX, screenY int) (termX, termY int, inViewport bool) {
	dashboardTopY := 0
	if a.err != "" {
		dashboardTopY++
	}
	if a.confirmQuit {
		dashboardTopY++
	}
	const (
		previewColOffset  = 32 // list panel width + list-panel right border
		previewLeftBorder = 1
		previewTopBorder  = 1
	)
	w := a.dashboard.fixedTermWidth()
	h := a.dashboard.fixedTermHeight()
	termX = screenX - previewColOffset - previewLeftBorder
	termY = screenY - dashboardTopY - previewTopBorder - a.dashboard.previewMetadataRows()
	inViewport = w > 0 && h > 0 && termX >= 0 && termX < w && termY >= 0 && termY < h
	return termX, termY, inViewport
}

// forwardWheelToAgent encodes a mouse wheel event and feeds it to the agent's
// terminal. Coordinates are translated from dashboard-screen space to cells
// relative to the agent's PTY viewport and clamped to [0,W)×[0,H). The
// emulator only emits bytes when the running program has enabled mouse
// reporting (DECSET 1000/1002/1003 + SGR 1006).
func (a *App) forwardWheelToAgent(ag *agent.Agent, msg tea.MouseWheelMsg) {
	termX, termY, _ := a.screenToTermCell(msg.X, msg.Y)
	if termX < 0 {
		termX = 0
	}
	if w := a.dashboard.fixedTermWidth(); w > 0 && termX >= w {
		termX = w - 1
	}
	if termY < 0 {
		termY = 0
	}
	if h := a.dashboard.fixedTermHeight(); h > 0 && termY >= h {
		termY = h - 1
	}
	ag.SendMouse(xvt.MouseWheel{
		X:      termX,
		Y:      termY,
		Button: xvt.MouseButton(msg.Button),
		Mod:    xvt.KeyMod(msg.Mod),
	})
}

func (a *App) refreshAgentList() {
	a.dashboard.closingAgents = a.closingAgents
	a.dashboard.closingSessions = a.closingSessions
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
			repoName: repo.DisplayName(),
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
	a.recomputePRSectionY()
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
		statusbar := renderStatusBar(diffHints, a.width)
		content = lipgloss.JoinVertical(lipgloss.Left, body, statusbar)
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
		confirmLine := StyleWarning.Render("Agents are running. Press q again to detach, any other key to cancel.")
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
	a.recomputePRSectionY()
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
				// Push detection runs for every session — including those with a
				// cached PR — so new commits, force-pushes, and rewrites get
				// picked up promptly instead of waiting the 30s stable interval.
				// Throttled to once per shaCheckInterval so git rev-parse does
				// not block the Bubble Tea main goroutine on every tick.
				if now.Sub(ps.lastSHACheck) < shaCheckInterval {
					continue
				}
				ps.lastSHACheck = now
				sha := getRemoteSHA(repo.Path, sess.Branch())
				if sha == "" || sha == ps.lastRemoteSHA {
					continue
				}
				ps.lastRemoteSHA = sha
				// SHA changed — arm a burst so the next minute of polls runs
				// on the short (2s) cadence, then fall through to schedule an
				// immediate poll.
				ps.burstUntil = now.Add(60 * time.Second)
			}

			ps.lastPoll = now
			ps.inFlight = true
			a.prPollsInFlight++
			cmds = append(cmds, a.refreshPRStatusForSession(sess.ID, sess.Branch(), repo.Path))
		}
	}
	return cmds
}

// prPollInterval returns the adaptive polling interval for a session.
func (a *App) prPollInterval(sessionID string, ps *prSessionState) time.Duration {
	// Event-driven burst (branch rename, new push): poll aggressively for a
	// short window so state transitions become visible within ~2s.
	if ps != nil && time.Now().Before(ps.burstUntil) {
		return 2 * time.Second
	}
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
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		rawURL, err := git.GetRemoteURL(repoPath)
		if err != nil {
			return prPollMsg{sessionID: sessionID, err: err}
		}
		owner, repo, err := github.ParseRemoteURL(rawURL)
		if err != nil {
			return prPollMsg{sessionID: sessionID, err: err}
		}

		// Prefer SHA-based lookup: invariant to branch renames, so a PR opened
		// under a random baton/<adj>-<noun> name (before Haiku rename finishes)
		// is still discovered after the rename. Fall back to branch lookup when
		// the commit hasn't been pushed or SHA lookup returns no PR.
		var pr *github.PRState
		sha := getRemoteSHA(repoPath, branch)
		if sha != "" {
			pr, _ = ghClient.GetPRBySHA(ctx, owner, repo, sha)
		}
		if pr == nil {
			var err error
			pr, err = ghClient.GetPR(ctx, owner, repo, branch)
			if err != nil {
				return prPollMsg{sessionID: sessionID, err: err}
			}
		}
		var checks *github.CheckStatus
		var reviews *github.ReviewStatus
		if pr != nil {
			var err error
			// Prefer SHA for checks when available — matches what CI ran against.
			checkRef := branch
			if sha != "" {
				checkRef = sha
			}
			checks, err = ghClient.GetChecks(ctx, owner, repo, checkRef)
			if err != nil {
				return prPollMsg{sessionID: sessionID, err: err}
			}
			reviews, err = ghClient.GetReviews(ctx, owner, repo, pr.Number)
			if err != nil {
				return prPollMsg{sessionID: sessionID, err: err}
			}
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

// openURL opens the given URL in the system's default browser. Fire-and-forget.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// Escape embedded quotes to avoid shell injection via cmd.exe.
		safeURL := strings.ReplaceAll(url, `"`, `%22`)
		cmd = exec.Command("cmd", "/c", "start", `"`+safeURL+`"`)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// recomputePRSectionY updates d.prSectionY with the content-relative row index
// (0-indexed, after the AGENTS title and separator) where the PR checks section
// begins, or -1 when no PR section is rendered. Call after any layout change.
// Must mirror the truncation logic in dashboardModel.renderList so mouse clicks
// map to the correct visual rows.
func (a *App) recomputePRSectionY() {
	d := &a.dashboard
	d.prSectionY = -1

	sess := d.selectedSession()
	if sess == nil {
		return
	}
	entry := a.prCache[sess.ID]
	if entry == nil || entry.pr == nil {
		return
	}

	contentH := d.contentHeight()
	// Budget for the PR panel, matching renderList.
	prBudget := 6
	if half := contentH / 2; prBudget > half {
		prBudget = half
	}
	if prBudget < 2 {
		return
	}

	agentListHeight := len(d.items)
	// Apply the same list truncation renderList performs, so availCheckHeight
	// reflects the post-truncation list length.
	if agentListHeight > contentH-prBudget {
		maxList := contentH - prBudget
		if maxList < 1 {
			maxList = 1
		}
		agentListHeight = maxList
	}
	maxCheckHeight := contentH / 3
	availCheckHeight := contentH - agentListHeight
	if availCheckHeight > maxCheckHeight && maxCheckHeight >= prBudget {
		availCheckHeight = maxCheckHeight
	}
	if availCheckHeight < 2 {
		return
	}

	d.prSectionY = contentH - availCheckHeight
}
