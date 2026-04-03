package agent

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/devenjarvis/baton/internal/git"
	bpty "github.com/devenjarvis/baton/internal/pty"
	"github.com/devenjarvis/baton/internal/vt"

	xvt "github.com/charmbracelet/x/vt"
)

// Agent ties together a PTY, VT terminal, and git worktree into a managed unit.
type Agent struct {
	ID        string
	Name      string
	Task      string
	Worktree  *git.WorktreeInfo
	CreatedAt time.Time

	pty      *bpty.PTY
	terminal *vt.Terminal

	mu         sync.RWMutex
	status     Status
	lastOutput time.Time
	exitErr    error

	done         chan struct{}
	stop         chan struct{}
	writeLoopDone chan struct{}
}

// Config holds parameters for creating a new agent.
type Config struct {
	Name              string
	Task              string
	Rows              int
	Cols              int
	RepoPath          string
	BypassPermissions bool
}

// newAgent creates and starts an agent. Called by Manager.
func newAgent(id string, cfg Config) (*Agent, error) {
	wt, err := git.CreateWorktree(cfg.RepoPath, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	term := vt.New(cfg.Cols, cfg.Rows)

	var cmd *exec.Cmd
	if cfg.BypassPermissions {
		cmd = exec.Command("claude", "--dangerously-skip-permissions", cfg.Task)
	} else {
		cmd = exec.Command("claude", cfg.Task)
	}
	cmd.Dir = wt.Path
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")

	p := &bpty.PTY{}
	if err := p.Start(cmd, uint16(cfg.Rows), uint16(cfg.Cols)); err != nil {
		// Clean up worktree on failure.
		_ = git.RemoveWorktree(cfg.RepoPath, wt, true)
		return nil, fmt.Errorf("starting PTY: %w", err)
	}

	a := &Agent{
		ID:        id,
		Name:      cfg.Name,
		Task:      cfg.Task,
		Worktree:  wt,
		CreatedAt: time.Now(),
		pty:       p,
		terminal:  term,
		status:    StatusStarting,
		done:          make(chan struct{}),
		stop:          make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}

	go a.readLoop()
	go a.writeLoop()
	go a.statusLoop()

	return a, nil
}

// newAgentWithCommand creates an agent using a custom command instead of claude.
// Used for testing.
func newAgentWithCommand(id string, cfg Config, cmd *exec.Cmd) (*Agent, error) {
	wt, err := git.CreateWorktree(cfg.RepoPath, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	term := vt.New(cfg.Cols, cfg.Rows)

	cmd.Dir = wt.Path
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")

	p := &bpty.PTY{}
	if err := p.Start(cmd, uint16(cfg.Rows), uint16(cfg.Cols)); err != nil {
		_ = git.RemoveWorktree(cfg.RepoPath, wt, true)
		return nil, fmt.Errorf("starting PTY: %w", err)
	}

	a := &Agent{
		ID:        id,
		Name:      cfg.Name,
		Task:      cfg.Task,
		Worktree:  wt,
		CreatedAt: time.Now(),
		pty:       p,
		terminal:  term,
		status:    StatusStarting,
		done:          make(chan struct{}),
		stop:          make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}

	go a.readLoop()
	go a.writeLoop()
	go a.statusLoop()

	return a, nil
}

// readLoop reads PTY output and feeds it to the VT terminal.
func (a *Agent) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := a.pty.Read(buf)
		if n > 0 {
			a.terminal.Write(buf[:n])
			a.mu.Lock()
			a.lastOutput = time.Now()
			if a.status == StatusStarting {
				a.status = StatusActive
			}
			a.mu.Unlock()
		}
		if err != nil {
			break
		}
	}

	// Process has exited — wait for the PTY done signal.
	<-a.pty.Done()

	a.mu.Lock()
	a.exitErr = a.pty.Err()
	if a.exitErr != nil {
		a.status = StatusError
	} else {
		a.status = StatusDone
	}
	a.mu.Unlock()

	close(a.done)
}

// writeLoop reads escape sequences from the VT terminal and writes them to the PTY.
func (a *Agent) writeLoop() {
	defer close(a.writeLoopDone)
	buf := make([]byte, 256)
	for {
		n, err := a.terminal.Read(buf)
		if n > 0 {
			a.pty.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// statusLoop periodically checks for idle status.
func (a *Agent) statusLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-a.stop:
			return
		case <-ticker.C:
			a.mu.Lock()
			if a.status == StatusActive && time.Since(a.lastOutput) > idleTimeout {
				a.status = StatusIdle
			} else if a.status == StatusIdle && time.Since(a.lastOutput) <= idleTimeout {
				a.status = StatusActive
			}
			a.mu.Unlock()
		}
	}
}

// Render returns the full terminal screen as an ANSI string.
func (a *Agent) Render() string {
	return a.terminal.Render()
}

// RenderRegion returns a subset of terminal rows.
func (a *Agent) RenderRegion(startRow, endRow int) string {
	return a.terminal.RenderRegion(startRow, endRow)
}

// SendKey forwards a key event to the VT terminal.
func (a *Agent) SendKey(key xvt.KeyPressEvent) {
	a.terminal.SendKey(key)
}

// SendText forwards text input to the VT terminal.
func (a *Agent) SendText(text string) {
	a.terminal.SendText(text)
}

// ScrollbackLines returns the scrollback buffer as ANSI-encoded strings, oldest first.
func (a *Agent) ScrollbackLines() []string {
	return a.terminal.ScrollbackLines()
}

// Resize updates both the VT terminal and PTY dimensions.
func (a *Agent) Resize(rows, cols int) {
	a.terminal.Resize(cols, rows)
	a.pty.Resize(uint16(rows), uint16(cols))
}

// Status returns the current agent status.
func (a *Agent) Status() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// Elapsed returns how long the agent has been running.
func (a *Agent) Elapsed() time.Duration {
	return time.Since(a.CreatedAt)
}

// Done returns a channel that fires when the agent's process exits.
func (a *Agent) Done() <-chan struct{} {
	return a.done
}

// ExitErr returns the process exit error, if any.
func (a *Agent) ExitErr() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.exitErr
}

// Kill terminates the agent's process and waits for goroutines to exit.
func (a *Agent) Kill() {
	close(a.stop)
	a.pty.Close()
	// Close terminal to unblock writeLoop's Read call.
	a.terminal.Close()
	// Wait for writeLoop to finish before returning.
	<-a.writeLoopDone
}

// Cleanup removes the agent's worktree and branch.
func (a *Agent) Cleanup(repoPath string) error {
	return git.RemoveWorktree(repoPath, a.Worktree, true)
}
