package agent

import (
	"fmt"
	"os/exec"
	"regexp"
	"sync"
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
)

// Event represents something that happened to an agent.
type Event struct {
	Type    EventType
	AgentID string
	Status  Status
}

// Manager manages the lifecycle of all agents.
type Manager struct {
	repoPath string

	mu     sync.RWMutex
	agents map[string]*Agent
	nextID int

	events   chan Event
	done     chan struct{}
	watchers sync.WaitGroup
}

// NewManager creates a new agent manager for the given repo.
func NewManager(repoPath string) *Manager {
	return &Manager{
		repoPath: repoPath,
		agents:   make(map[string]*Agent),
		events:   make(chan Event, 64),
		done:     make(chan struct{}),
	}
}

// Create starts a new agent with the given config.
func (m *Manager) Create(cfg Config) (*Agent, error) {
	if !validName.MatchString(cfg.Name) {
		return nil, fmt.Errorf("invalid agent name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", cfg.Name)
	}

	// Check name uniqueness.
	m.mu.RLock()
	for _, a := range m.agents {
		if a.Name == cfg.Name {
			m.mu.RUnlock()
			return nil, fmt.Errorf("agent %q already exists", cfg.Name)
		}
	}
	m.mu.RUnlock()

	cfg.RepoPath = m.repoPath

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("agent-%d", m.nextID)
	m.mu.Unlock()

	a, err := newAgent(id, cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.agents[id] = a
	m.mu.Unlock()

	m.emit(Event{Type: EventCreated, AgentID: id, Status: StatusStarting})

	// Watch for status changes and completion.
	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a)
	}()

	return a, nil
}

// CreateWithCommand starts a new agent with a custom command (for testing).
func (m *Manager) CreateWithCommand(cfg Config, cmd func(name string) *exec.Cmd) (*Agent, error) {
	cfg.RepoPath = m.repoPath

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("agent-%d", m.nextID)
	m.mu.Unlock()

	a, err := newAgentWithCommand(id, cfg, cmd(cfg.Name))
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.agents[id] = a
	m.mu.Unlock()

	m.emit(Event{Type: EventCreated, AgentID: id, Status: StatusStarting})

	m.watchers.Add(1)
	go func() {
		defer m.watchers.Done()
		m.watchAgent(a)
	}()

	return a, nil
}

// Get returns an agent by ID.
func (m *Manager) Get(id string) *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[id]
}

// List returns all agents.
func (m *Manager) List() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, a)
	}
	return result
}

// Kill terminates an agent and cleans up its resources.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	a := m.agents[id]
	m.mu.RUnlock()

	if a == nil {
		return fmt.Errorf("agent %s not found", id)
	}

	a.Kill()
	<-a.Done()

	if err := a.Cleanup(m.repoPath); err != nil {
		return fmt.Errorf("cleanup agent %s: %w", id, err)
	}

	m.mu.Lock()
	delete(m.agents, id)
	m.mu.Unlock()

	return nil
}

// Events returns a channel that emits agent lifecycle events.
func (m *Manager) Events() <-chan Event {
	return m.events
}

// Shutdown kills all agents and cleans up.
func (m *Manager) Shutdown() {
	// Signal all watcher goroutines to stop first.
	close(m.done)

	m.mu.RLock()
	agents := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.RUnlock()

	for _, a := range agents {
		a.Kill()
		<-a.Done()
		a.Cleanup(m.repoPath)
	}

	// Wait for all watcher goroutines to finish before closing the channel.
	m.watchers.Wait()

	m.mu.Lock()
	m.agents = make(map[string]*Agent)
	m.mu.Unlock()

	close(m.events)
}

// AgentCount returns the number of active agents.
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// RepoPath returns the manager's repo path.
func (m *Manager) RepoPath() string {
	return m.repoPath
}

func (m *Manager) watchAgent(a *Agent) {
	select {
	case <-a.Done():
		status := a.Status()
		m.emit(Event{Type: EventDone, AgentID: a.ID, Status: status})
	case <-m.done:
	}
}

func (m *Manager) emit(e Event) {
	select {
	case m.events <- e:
	default:
		// Drop event if channel is full — non-blocking.
	}
}
