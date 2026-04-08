package agent

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devenjarvis/baton/internal/git"
	"github.com/devenjarvis/baton/internal/state"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

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

	mu       sync.RWMutex
	sessions map[string]*Session
	nextID   int

	events   chan Event
	done     chan struct{}
	watchers sync.WaitGroup
}

// NewManager creates a new agent manager for the given repo.
func NewManager(repoPath string) *Manager {
	return &Manager{
		repoPath: repoPath,
		sessions: make(map[string]*Session),
		events:   make(chan Event, 64),
		done:     make(chan struct{}),
	}
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
	m.mu.Unlock()

	cfg.RepoPath = m.repoPath

	// Best-effort: update base branch from remote so the worktree
	// starts from the latest code. If offline, fall back to local HEAD.
	startPoint := ""
	if base, err := git.BaseBranch(m.repoPath); err == nil && base != "" {
		if err := git.UpdateBaseBranch(m.repoPath, base); err == nil {
			startPoint = "origin/" + base
		}
	}

	wt, err := git.CreateWorktree(m.repoPath, name, startPoint)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	sess := newSession(id, name, wt)

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// AddAgent adds an agent to an existing session using the default claude command.
func (m *Manager) AddAgent(sessionID string, cfg Config) (*Agent, error) {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	m.mu.RUnlock()

	if sess == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	cfg.RepoPath = m.repoPath

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

	for _, s := range sessions {
		s.KillAll()
		s.Cleanup(m.repoPath)
	}

	m.watchers.Wait()

	m.mu.Lock()
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	close(m.events)
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
	var sessionStates []state.SessionState
	for _, s := range sessions {
		ss := state.SessionState{
			ID:           s.ID,
			Name:         s.Name,
			DisplayName:  s.GetDisplayName(),
			WorktreePath: s.Worktree.Path,
			Branch:       s.Worktree.Branch,
			BaseBranch:   s.Worktree.BaseBranch,
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
	for _, s := range sessions {
		s.KillAll()
	}

	m.watchers.Wait()

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

	for _, as := range ss.Agents {
		agentCfg := Config{
			Name:              as.Name,
			Task:              as.Task,
			Rows:              cfg.Rows,
			Cols:              cfg.Cols,
			RepoPath:          m.repoPath,
			BypassPermissions: cfg.BypassPermissions,
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
