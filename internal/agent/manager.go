package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/git"
	"github.com/devenjarvis/baton/internal/hook"
	"github.com/devenjarvis/baton/internal/state"
)

// EventType represents the kind of agent event.
type EventType int

const (
	EventCreated EventType = iota
	EventStatusChanged
	EventOutput
	EventDone
	EventError
	EventSessionClosed
)

// Event represents something that happened to an agent.
type Event struct {
	Type      EventType
	AgentID   string
	SessionID string
	Status    Status
}

// Manager manages the lifecycle of all sessions and their agents.
type Manager struct {
	repoPath string
	settings config.ResolvedSettings

	mu       sync.RWMutex
	sessions map[string]*Session
	nextID   int

	events   chan Event
	done     chan struct{}
	watchers sync.WaitGroup

	hookServer     *hook.Server
	hookSocketPath string
	hookDispatcher sync.WaitGroup
}

// NewManager creates a new agent manager for the given repo.
//
// The manager owns a hook.Server listening on <repoPath>/.baton/hook.sock that
// routes Claude Code hook events to agents by BATON_AGENT_ID. If the socket
// fails to start (e.g. filesystem permissions), the manager logs to stderr
// and continues with hooks disabled; spawned agents will then never transition
// out of Active.
func NewManager(repoPath string, settings config.ResolvedSettings) *Manager {
	m := &Manager{
		repoPath: repoPath,
		settings: settings,
		sessions: make(map[string]*Session),
		events:   make(chan Event, 64),
		done:     make(chan struct{}),
	}

	batonDir := filepath.Join(repoPath, ".baton")
	if err := os.MkdirAll(batonDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "baton: creating %s: %v (hooks disabled)\n", batonDir, err)
		return m
	}
	socketPath := hookSocketPath(repoPath)
	srv, err := hook.NewServer(socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "baton: starting hook server on %s: %v (hooks disabled)\n", socketPath, err)
		return m
	}
	m.hookServer = srv
	m.hookSocketPath = socketPath

	m.hookDispatcher.Add(1)
	go m.dispatchHookEvents()

	return m
}

// HookSocketPath returns the unix socket path the manager's hook server is
// listening on, or "" if the server failed to start.
func (m *Manager) HookSocketPath() string {
	return m.hookSocketPath
}

// hookSocketPath returns the unix socket path for a given repoPath.
//
// Preferred layout: <repoPath>/.baton/hook.sock — easy to inspect and cleaned
// up with the rest of baton's per-repo state. macOS limits unix socket paths
// to 104 bytes, so when the preferred path would exceed a safe threshold we
// fall back to a short hashed name under os.TempDir(). Tests exercise the
// fallback path via deeply nested temp directories.
func hookSocketPath(repoPath string) string {
	preferred := filepath.Join(repoPath, ".baton", "hook.sock")
	// 104 is the darwin sun_path limit; leave headroom for the trailing NUL
	// and any quirks. 100 is comfortably below.
	if len(preferred) < 100 {
		return preferred
	}
	h := sha256.Sum256([]byte(repoPath))
	return filepath.Join(os.TempDir(), fmt.Sprintf("baton-%x.sock", h[:8]))
}

// dispatchHookEvents reads hook events from the server and routes each to the
// agent named by AgentID. Unknown agent IDs are dropped silently. On the
// first UserPromptSubmit for an agent, the prompt is slugified and applied
// as the auto-generated display name on both the agent and its session.
func (m *Manager) dispatchHookEvents() {
	defer m.hookDispatcher.Done()
	for e := range m.hookServer.Events() {
		a, sessID := m.findAgent(e.AgentID)
		if a == nil {
			// This can happen if an agent has already been killed but Claude's
			// final Stop/SessionEnd hook is still in flight. Drop silently.
			continue
		}
		if changed := a.OnHookEvent(e); changed {
			m.emit(Event{
				Type:      EventStatusChanged,
				AgentID:   a.ID,
				SessionID: sessID,
				Status:    a.Status(),
			})
		}
		if e.Kind == hook.KindUserPromptSubmit {
			m.applyAutoName(a, sessID, e.Prompt)
		}
	}
}

// applyAutoName derives a display name from the first UserPromptSubmit prompt
// and applies it to the agent and (if unset) the session. One-shot: once
// HasClaudeName is true the agent is never renamed again, even when the
// first prompt slugifies to empty — mirrors the old Claude-session-file
// auto-name behavior so we don't silently overwrite a user-accepted name
// later in the session.
//
// Mirrors OnHookEvent's late-event guard: a stray UserPromptSubmit arriving
// after the agent reached Done or Error must not rename a terminal row.
func (m *Manager) applyAutoName(a *Agent, sessID, prompt string) {
	if a.HasClaudeName() {
		return
	}
	if st := a.Status(); st == StatusDone || st == StatusError {
		return
	}
	if slug := slugify(prompt); slug != "" {
		a.SetDisplayName(slug)
		if sess := m.GetSession(sessID); sess != nil && !sess.HasDisplayName() {
			sess.SetDisplayName(slug)
		}
	}
	a.SetClaudeName(true)
}

// findAgent locates an agent across all sessions and returns it with the
// containing session ID.
func (m *Manager) findAgent(agentID string) (*Agent, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if a := s.GetAgent(agentID); a != nil {
			return a, s.ID
		}
	}
	return nil, ""
}

// UpdateSettings replaces the manager's resolved settings.
// New sessions will use the updated values; existing sessions are unaffected.
func (m *Manager) UpdateSettings(s config.ResolvedSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings = s
}

// Settings returns the current resolved settings.
func (m *Manager) Settings() config.ResolvedSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// CreateSession starts a new session with its first agent using the default claude command.
func (m *Manager) CreateSession(cfg Config) (*Session, *Agent, error) {
	sess, err := m.createSessionWorktree(cfg)
	if err != nil {
		return nil, nil, err
	}

	a, err := sess.AddAgentDefault(cfg)
	if err != nil {
		// Clean up worktree on failure.
		_ = sess.Cleanup(m.repoPath)
		m.mu.Lock()
		delete(m.sessions, sess.ID)
		m.mu.Unlock()
		return nil, nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sess.ID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sess.ID)
	}()

	return sess, a, nil
}

// CreateSessionWithCommand starts a new session with its first agent using a custom command.
func (m *Manager) CreateSessionWithCommand(cfg Config, cmd func(name string) *exec.Cmd) (*Session, *Agent, error) {
	sess, err := m.createSessionWorktree(cfg)
	if err != nil {
		return nil, nil, err
	}

	a, err := sess.AddAgent(cfg, cmd(cfg.Name))
	if err != nil {
		_ = sess.Cleanup(m.repoPath)
		m.mu.Lock()
		delete(m.sessions, sess.ID)
		m.mu.Unlock()
		return nil, nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sess.ID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sess.ID)
	}()

	return sess, a, nil
}

// CreateSessionOnBranch starts a new session on an existing branch using the default claude command.
func (m *Manager) CreateSessionOnBranch(branch, baseBranch string, cfg Config) (*Session, *Agent, error) {
	sess, err := m.createSessionOnBranchWorktree(branch, baseBranch, cfg)
	if err != nil {
		return nil, nil, err
	}

	a, err := sess.AddAgentDefault(cfg)
	if err != nil {
		_ = sess.Cleanup(m.repoPath)
		m.mu.Lock()
		delete(m.sessions, sess.ID)
		m.mu.Unlock()
		return nil, nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sess.ID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sess.ID)
	}()

	return sess, a, nil
}

// CreateSessionOnBranchWithCommand starts a new session on an existing branch using a custom command.
func (m *Manager) CreateSessionOnBranchWithCommand(branch, baseBranch string, cfg Config, cmd func(name string) *exec.Cmd) (*Session, *Agent, error) {
	sess, err := m.createSessionOnBranchWorktree(branch, baseBranch, cfg)
	if err != nil {
		return nil, nil, err
	}

	a, err := sess.AddAgent(cfg, cmd(cfg.Name))
	if err != nil {
		_ = sess.Cleanup(m.repoPath)
		m.mu.Lock()
		delete(m.sessions, sess.ID)
		m.mu.Unlock()
		return nil, nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sess.ID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sess.ID)
	}()

	return sess, a, nil
}

// createSessionOnBranchWorktree creates a session attached to an existing branch.
// The session does NOT own the branch — cleanup removes the worktree but preserves it.
// If baseBranch is non-empty, it overrides the default base on the returned WorktreeInfo.
func (m *Manager) createSessionOnBranchWorktree(branch, baseBranch string, cfg Config) (*Session, error) {
	m.mu.Lock()
	existing := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		existing = append(existing, s.Name)
	}
	name := slugifyBranchName(branch, existing)
	m.nextID++
	id := fmt.Sprintf("session-%d", m.nextID)
	settings := m.settings
	m.mu.Unlock()

	cfg.RepoPath = m.repoPath
	if cfg.AgentProgram == "" {
		cfg.AgentProgram = settings.AgentProgram
	}

	wt, err := git.AttachWorktree(m.repoPath, name, settings.WorktreeDir, branch)
	if err != nil {
		return nil, fmt.Errorf("attaching worktree: %w", err)
	}

	// Override base branch if caller provided one (e.g. from PR data).
	if baseBranch != "" {
		wt.BaseBranch = baseBranch
	}

	sess := newSession(id, name, wt)
	sess.hookSocketPath = m.hookSocketPath
	// ownsBranch stays false — we didn't create this branch.

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// slugifyBranchName derives a session name from a branch name.
// Takes the last path segment (e.g. "feature/add-auth" → "add-auth"), slugifies it,
// and falls back to RandomName if the result is empty or collides.
func slugifyBranchName(branch string, existing []string) string {
	parts := strings.Split(branch, "/")
	last := parts[len(parts)-1]
	name := slugify(last)

	if name == "" {
		return RandomName(existing)
	}

	// Check for collision.
	for _, e := range existing {
		if e == name {
			return RandomName(existing)
		}
	}

	return name
}

// createSessionWorktree creates a session with its worktree, adds it to the map.
func (m *Manager) createSessionWorktree(cfg Config) (*Session, error) {
	// Generate session name.
	m.mu.Lock()
	existing := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		existing = append(existing, s.Name)
	}
	name := RandomName(existing)
	m.nextID++
	id := fmt.Sprintf("session-%d", m.nextID)
	settings := m.settings
	m.mu.Unlock()

	cfg.RepoPath = m.repoPath
	if cfg.AgentProgram == "" {
		cfg.AgentProgram = settings.AgentProgram
	}

	// Determine base branch: use configured default, or auto-detect.
	baseBranch := settings.DefaultBranch
	if baseBranch == "" {
		if detected, err := git.BaseBranch(m.repoPath); err == nil {
			baseBranch = detected
		}
	}

	// Best-effort: update base branch from remote so the worktree
	// starts from the latest code. If offline, fall back to local HEAD.
	startPoint := ""
	if baseBranch != "" {
		if err := git.UpdateBaseBranch(m.repoPath, baseBranch); err == nil {
			startPoint = "origin/" + baseBranch
		}
	}

	wt, err := git.CreateWorktree(m.repoPath, name, settings.BranchPrefix, settings.WorktreeDir, baseBranch, startPoint)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	sess := newSession(id, name, wt)
	sess.hookSocketPath = m.hookSocketPath
	sess.ownsBranch = true

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// AddAgent adds an agent to an existing session using the default claude command.
func (m *Manager) AddAgent(sessionID string, cfg Config) (*Agent, error) {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	settings := m.settings
	m.mu.RUnlock()

	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	cfg.RepoPath = m.repoPath
	if cfg.AgentProgram == "" {
		cfg.AgentProgram = settings.AgentProgram
	}

	a, err := sess.AddAgentDefault(cfg)
	if err != nil {
		return nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sessionID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sessionID)
	}()

	return a, nil
}

// AddAgentWithCommand adds an agent to an existing session using a custom command.
func (m *Manager) AddAgentWithCommand(sessionID string, cfg Config, cmd func(name string) *exec.Cmd) (*Agent, error) {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()

	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	cfg.RepoPath = m.repoPath

	a, err := sess.AddAgent(cfg, cmd(cfg.Name))
	if err != nil {
		return nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sessionID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sessionID)
	}()

	return a, nil
}

// AddShell adds a shell agent to an existing session.
func (m *Manager) AddShell(sessionID string, cfg Config) (*Agent, error) {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()

	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	cfg.RepoPath = m.repoPath

	a, err := sess.AddShell(cfg)
	if err != nil {
		return nil, err
	}

	m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sessionID, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a, sessionID)
	}()

	return a, nil
}

// Create starts a new session with its first agent (backward-compatible wrapper).
func (m *Manager) Create(cfg Config) (*Agent, error) {
	_, a, err := m.CreateSession(cfg)
	return a, err
}

// CreateWithCommand starts a new session with a custom command (backward-compatible wrapper).
func (m *Manager) CreateWithCommand(cfg Config, cmd func(name string) *exec.Cmd) (*Agent, error) {
	_, a, err := m.CreateSessionWithCommand(cfg, cmd)
	return a, err
}

// GetSession returns a session by ID, or nil if not found.
func (m *Manager) GetSession(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// ListSessions returns all sessions.
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// Get returns an agent by ID (searches all sessions).
func (m *Manager) Get(id string) *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if a := s.GetAgent(id); a != nil {
			return a
		}
	}
	return nil
}

// List returns all agents across all sessions.
func (m *Manager) List() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Agent
	for _, s := range m.sessions {
		result = append(result, s.Agents()...)
	}
	return result
}

// KillAgent kills a single agent within a session. If the session becomes
// empty after the kill, the session is automatically cleaned up and removed.
func (m *Manager) KillAgent(sessionID, agentID string) error {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()

	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if sess.GetAgent(agentID) == nil {
		return fmt.Errorf("agent %s not found in session %s", agentID, sessionID)
	}

	sess.KillAgent(agentID)

	// Auto-close empty sessions.
	if sess.AgentCount() == 0 {
		m.closeSession(sessionID, sess)
	}

	return nil
}

// KillSession kills all agents in a session, removes the worktree, and deletes the session.
func (m *Manager) KillSession(sessionID string) error {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()

	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	sess.KillAll()

	if err := sess.Cleanup(m.repoPath); err != nil {
		return fmt.Errorf("cleanup session %s: %w", sessionID, err)
	}

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	return nil
}

// Kill terminates an agent and cleans up its session (backward-compatible).
// Finds the session containing the agent and kills the entire session.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	for _, sess := range m.sessions {
		if a := sess.GetAgent(id); a != nil {
			sessID := sess.ID
			m.mu.RUnlock()
			return m.KillSession(sessID)
		}
	}
	m.mu.RUnlock()
	return fmt.Errorf("agent %s not found", id)
}

// Events returns a channel that emits agent lifecycle events.
func (m *Manager) Events() <-chan Event {
	return m.events
}

// Shutdown kills all sessions and cleans up.
func (m *Manager) Shutdown() {
	close(m.done)

	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	wg.Add(len(sessions))
	for _, s := range sessions {
		go func() {
			defer wg.Done()
			s.KillAll()
			_ = s.Cleanup(m.repoPath)
		}()
	}
	wg.Wait()

	m.watchers.Wait()
	m.stopHookServer()

	m.mu.Lock()
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	close(m.events)
}

// stopHookServer closes the hook server and waits for the dispatcher goroutine.
// Safe to call multiple times; no-op if the server never started.
func (m *Manager) stopHookServer() {
	if m.hookServer == nil {
		return
	}
	_ = m.hookServer.Close()
	m.hookDispatcher.Wait()
	m.hookServer = nil
}

// AgentCount returns the total number of agents across all sessions.
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.sessions {
		count += s.AgentCount()
	}
	return count
}

// RepoPath returns the manager's repo path.
func (m *Manager) RepoPath() string {
	return m.repoPath
}

// Detach snapshots all sessions into a BatonState, kills all agents but preserves
// worktrees, and shuts down the manager. Returns the state for persistence.
func (m *Manager) Detach() *state.BatonState {
	close(m.done)

	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	// Snapshot state before killing agents.
	sessionStates := make([]state.SessionState, 0, len(sessions))
	for _, s := range sessions {
		ss := state.SessionState{
			ID:           s.ID,
			Name:         s.Name,
			DisplayName:  s.GetDisplayName(),
			WorktreePath: s.Worktree.Path,
			Branch:       s.Worktree.Branch,
			BaseBranch:   s.Worktree.BaseBranch,
			OwnsBranch:   s.ownsBranch,
		}
		for _, a := range s.Agents() {
			as := state.AgentState{
				ID:              a.ID,
				Name:            a.Name,
				DisplayName:     a.GetDisplayName(),
				Task:            a.Task,
				ClaudeSessionID: a.ClaudeSessionID(),
			}
			ss.Agents = append(ss.Agents, as)
		}
		sessionStates = append(sessionStates, ss)
	}

	// Kill all agents but do NOT call Cleanup (preserve worktrees).
	var wg sync.WaitGroup
	wg.Add(len(sessions))
	for _, s := range sessions {
		go func() {
			defer wg.Done()
			s.KillAll()
		}()
	}
	wg.Wait()

	m.watchers.Wait()
	m.stopHookServer()

	m.mu.Lock()
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	close(m.events)

	if len(sessionStates) == 0 {
		return nil
	}

	return &state.BatonState{
		Version:  1,
		SavedAt:  time.Now(),
		Sessions: sessionStates,
	}
}

// ResumeSession recreates a session from saved state without creating a new worktree.
// It verifies the worktree directory exists, constructs a Session from saved data,
// and spawns agents with --resume flags.
func (m *Manager) ResumeSession(ss state.SessionState, cfg Config) error {
	// Verify worktree directory exists.
	if _, err := os.Stat(ss.WorktreePath); err != nil {
		return fmt.Errorf("worktree %s not found: %w", ss.WorktreePath, err)
	}

	wt := &git.WorktreeInfo{
		Name:       ss.Name,
		Path:       ss.WorktreePath,
		Branch:     ss.Branch,
		BaseBranch: ss.BaseBranch,
	}

	sess := newSession(ss.ID, ss.Name, wt)
	sess.hookSocketPath = m.hookSocketPath
	sess.ownsBranch = ss.OwnsBranch
	if ss.DisplayName != "" && ss.DisplayName != ss.Name {
		sess.SetDisplayName(ss.DisplayName)
	}

	m.mu.Lock()
	m.sessions[ss.ID] = sess
	// Parse session ID number to avoid collisions with nextID.
	if num := parseSessionNum(ss.ID); num >= m.nextID {
		m.nextID = num + 1
	}
	m.mu.Unlock()

	settings := m.Settings()

	for _, as := range ss.Agents {
		agentCfg := Config{
			Name:              as.Name,
			Task:              as.Task,
			Rows:              cfg.Rows,
			Cols:              cfg.Cols,
			RepoPath:          m.repoPath,
			BypassPermissions: cfg.BypassPermissions,
			AgentProgram:      settings.AgentProgram,
		}

		a, err := sess.AddAgentResumed(agentCfg, as.ClaudeSessionID)
		if err != nil {
			// Clean up any agents already created in this session.
			sess.KillAll()
			m.mu.Lock()
			delete(m.sessions, ss.ID)
			m.mu.Unlock()
			return fmt.Errorf("resuming agent %s: %w", as.Name, err)
		}

		// Restore display name from saved state.
		if as.DisplayName != "" {
			a.SetDisplayName(as.DisplayName)
			a.SetClaudeName(true)
		}

		m.emit(Event{Type: EventCreated, AgentID: a.ID, SessionID: sess.ID, Status: StatusStarting})

		m.watchers.Add(1)
		go func() {
			defer m.watchers.Done()
			m.watchAgent(a, sess.ID)
		}()
	}

	return nil
}

// parseSessionNum extracts the numeric ID from a session ID like "session-3".
func parseSessionNum(id string) int {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return n
}

func (m *Manager) watchAgent(a *Agent, sessionID string) {
	select {
	case <-a.Done():
		status := a.Status()
		m.emit(Event{Type: EventDone, AgentID: a.ID, SessionID: sessionID, Status: status})

		// Auto-close session if all agents are done.
		m.mu.RLock()
		sess := m.sessions[sessionID]
		m.mu.RUnlock()
		if sess != nil && sess.Status() == StatusDone {
			m.closeSession(sessionID, sess)
		}
	case <-m.done:
	}
}

// closeSession cleans up and removes a session, emitting EventSessionClosed.
// Safe to call concurrently — only the first caller performs cleanup.
func (m *Manager) closeSession(sessionID string, sess *Session) {
	m.mu.Lock()
	if _, exists := m.sessions[sessionID]; !exists {
		m.mu.Unlock()
		return // already cleaned up by another goroutine
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()

	sess.KillAll()
	_ = sess.Cleanup(m.repoPath)

	m.emit(Event{Type: EventSessionClosed, SessionID: sessionID})
}

func (m *Manager) emit(e Event) {
	select {
	case m.events <- e:
	default:
	}
}
