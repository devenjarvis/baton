package agent

import (
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/devenjarvis/baton/internal/git"
)

// Session owns a git worktree and holds one or more agents that share it.
type Session struct {
	ID        string
	Name      string
	Worktree  *git.WorktreeInfo
	CreatedAt time.Time

	mu           sync.RWMutex
	agents       map[string]*Agent
	nextAgentNum int
}

// newSession creates a session with the given worktree.
func newSession(id, name string, wt *git.WorktreeInfo) *Session {
	return &Session{
		ID:        id,
		Name:      name,
		Worktree:  wt,
		CreatedAt: time.Now(),
		agents:    make(map[string]*Agent),
	}
}

// AddAgent creates and starts a new agent within this session using the session's worktree.
func (s *Session) AddAgent(cfg Config, cmd *exec.Cmd) (*Agent, error) {
	s.mu.Lock()
	if cfg.Name == "" {
		cfg.Name = RandomName(s.existingNames())
	}
	s.nextAgentNum++
	num := s.nextAgentNum
	id := fmt.Sprintf("%s-agent-%d", s.ID, num)
	s.mu.Unlock()

	a, err := newAgentWithCommand(id, cfg, s.Worktree.Path, cmd)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.agents[id] = a
	s.mu.Unlock()

	return a, nil
}

// AddAgentDefault creates and starts a new agent using the default claude command.
func (s *Session) AddAgentDefault(cfg Config) (*Agent, error) {
	s.mu.Lock()
	if cfg.Name == "" {
		cfg.Name = RandomName(s.existingNames())
	}
	s.nextAgentNum++
	num := s.nextAgentNum
	id := fmt.Sprintf("%s-agent-%d", s.ID, num)
	s.mu.Unlock()

	a, err := newAgent(id, cfg, s.Worktree.Path)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.agents[id] = a
	s.mu.Unlock()

	return a, nil
}

// GetAgent returns an agent by ID, or nil if not found.
func (s *Session) GetAgent(id string) *Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agents[id]
}

// Agents returns all agents sorted by CreatedAt.
func (s *Session) Agents() []*Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

// Status returns a composite status across all agents in the session.
// Priority: any Active→Active, any Starting→Starting, any Idle→Idle,
// any Error→Error, all Done→Done, no agents→Idle.
func (s *Session) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.agents) == 0 {
		return StatusIdle
	}

	hasStarting := false
	hasIdle := false
	hasError := false
	allDone := true

	for _, a := range s.agents {
		st := a.Status()
		switch st {
		case StatusActive:
			return StatusActive
		case StatusStarting:
			hasStarting = true
			allDone = false
		case StatusIdle:
			hasIdle = true
			allDone = false
		case StatusError:
			hasError = true
			allDone = false
		case StatusDone:
			// continue
		default:
			allDone = false
		}
	}

	if hasStarting {
		return StatusStarting
	}
	if hasIdle {
		return StatusIdle
	}
	if hasError {
		return StatusError
	}
	if allDone {
		return StatusDone
	}
	return StatusIdle
}

// AgentCount returns the number of agents in this session.
func (s *Session) AgentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.agents)
}

// KillAgent kills a single agent but does not remove the session.
func (s *Session) KillAgent(id string) {
	s.mu.RLock()
	a := s.agents[id]
	s.mu.RUnlock()

	if a == nil {
		return
	}

	a.Kill()
	<-a.Done()

	s.mu.Lock()
	delete(s.agents, id)
	s.mu.Unlock()
}

// KillAll kills all agents in this session.
func (s *Session) KillAll() {
	s.mu.RLock()
	agents := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.RUnlock()

	for _, a := range agents {
		a.Kill()
		<-a.Done()
	}

	s.mu.Lock()
	s.agents = make(map[string]*Agent)
	s.mu.Unlock()
}

// Cleanup removes the session's worktree and branch.
func (s *Session) Cleanup(repoPath string) error {
	return git.RemoveWorktree(repoPath, s.Worktree, true)
}

// existingNames returns the session name and all current agent names.
// Must be called with s.mu held.
func (s *Session) existingNames() []string {
	names := make([]string, 0, len(s.agents)+1)
	names = append(names, s.Name)
	for _, a := range s.agents {
		names = append(names, a.Name)
	}
	return names
}

// Elapsed returns how long the session has been running.
func (s *Session) Elapsed() time.Duration {
	return time.Since(s.CreatedAt)
}
