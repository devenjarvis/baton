package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	bpty "github.com/devenjarvis/baton/internal/pty"
	"github.com/devenjarvis/baton/internal/vt"

	xvt "github.com/charmbracelet/x/vt"
)

// Agent ties together a PTY and VT terminal into a managed unit.
// Agents do not own worktrees — sessions do.
type Agent struct {
	ID           string
	Name         string
	Task         string
	IsShell      bool   // true for shell agents (not Claude)
	WorktreePath string // reference only; session owns the worktree
	CreatedAt    time.Time

	pty      *bpty.PTY
	terminal *vt.Terminal

	mu             sync.RWMutex
	displayName    string
	claudePid      int
	hasClaudeName  bool
	status         Status
	lastOutput  time.Time
	lastInput   time.Time
	composing   bool
	exitErr     error

	done         chan struct{}
	stop         chan struct{}
	writeLoopDone chan struct{}
	killOnce     sync.Once
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

// newAgent creates and starts an agent with the default claude command.
// The worktreePath is provided by the session — agents do not create worktrees.
func newAgent(id string, cfg Config, worktreePath string) (*Agent, error) {
	term := vt.New(cfg.Cols, cfg.Rows)

	var cmd *exec.Cmd
	if cfg.BypassPermissions {
		if cfg.Task != "" {
			cmd = exec.Command("claude", "--dangerously-skip-permissions", cfg.Task)
		} else {
			cmd = exec.Command("claude", "--dangerously-skip-permissions")
		}
	} else {
		if cfg.Task != "" {
			cmd = exec.Command("claude", cfg.Task)
		} else {
			cmd = exec.Command("claude")
		}
	}
	cmd.Dir = worktreePath
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")

	p := &bpty.PTY{}
	if err := p.Start(cmd, uint16(cfg.Rows), uint16(cfg.Cols)); err != nil {
		return nil, err
	}

	claudePid := p.Pid()

	a := &Agent{
		ID:           id,
		Name:         cfg.Name,
		Task:         cfg.Task,
		WorktreePath: worktreePath,
		CreatedAt:    time.Now(),
		pty:          p,
		terminal:     term,
		claudePid:    claudePid,
		status:       StatusStarting,
		done:          make(chan struct{}),
		stop:          make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}

	if cfg.Task != "" {
		a.SetDisplayName(slugify(cfg.Task))
	}

	go a.readLoop()
	go a.writeLoop()
	go a.statusLoop()

	return a, nil
}

// newAgentWithCommand creates an agent using a custom command instead of claude.
// Used for testing. The worktreePath is provided by the session.
func newAgentWithCommand(id string, cfg Config, worktreePath string, cmd *exec.Cmd) (*Agent, error) {
	term := vt.New(cfg.Cols, cfg.Rows)

	cmd.Dir = worktreePath
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")

	p := &bpty.PTY{}
	if err := p.Start(cmd, uint16(cfg.Rows), uint16(cfg.Cols)); err != nil {
		return nil, err
	}

	a := &Agent{
		ID:           id,
		Name:         cfg.Name,
		Task:         cfg.Task,
		WorktreePath: worktreePath,
		CreatedAt:    time.Now(),
		pty:          p,
		terminal:     term,
		claudePid:    p.Pid(),
		status:       StatusStarting,
		done:          make(chan struct{}),
		stop:          make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}

	go a.readLoop()
	go a.writeLoop()
	go a.statusLoop()

	return a, nil
}

// newShellAgent creates an agent that spawns the user's shell instead of claude.
// It reuses the same PTY+VT setup and readLoop/writeLoop but skips statusLoop —
// shell agents stay StatusActive once output is received.
func newShellAgent(id string, cfg Config, worktreePath string) (*Agent, error) {
	term := vt.New(cfg.Cols, cfg.Rows)

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = worktreePath
	cmd.Env = append(cmd.Environ(), "TERM=xterm-256color")

	p := &bpty.PTY{}
	if err := p.Start(cmd, uint16(cfg.Rows), uint16(cfg.Cols)); err != nil {
		return nil, err
	}

	a := &Agent{
		ID:           id,
		Name:         "shell",
		IsShell:      true,
		WorktreePath: worktreePath,
		CreatedAt:    time.Now(),
		pty:          p,
		terminal:     term,
		displayName:  "shell",
		status:       StatusStarting,
		done:         make(chan struct{}),
		stop:         make(chan struct{}),
		writeLoopDone: make(chan struct{}),
	}

	go a.readLoop()
	go a.writeLoop()
	// No statusLoop — shell agents don't transition to Idle.

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
			timeout := idleTimeout
			if a.composing {
				timeout = composingIdleTimeout
			}
			if a.status == StatusActive && time.Since(a.lastOutput) > timeout && time.Since(a.lastInput) > timeout {
				a.status = StatusIdle
				a.composing = false
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
	a.mu.Lock()
	a.lastInput = time.Now()
	if key.Code == xvt.KeyEnter {
		a.composing = false
	} else {
		a.composing = true
	}
	a.mu.Unlock()
	a.terminal.SendKey(key)
}

// SendText forwards text input to the VT terminal.
func (a *Agent) SendText(text string) {
	a.mu.Lock()
	a.lastInput = time.Now()
	a.composing = true
	a.mu.Unlock()
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

// SetDisplayName sets the human-readable display name for this agent.
func (a *Agent) SetDisplayName(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.displayName = name
}

// GetDisplayName returns the display name if set, otherwise Name.
func (a *Agent) GetDisplayName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.displayName != "" {
		return a.displayName
	}
	return a.Name
}

// HasReceivedInput reports whether the user has sent any input to this agent.
func (a *Agent) HasReceivedInput() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.lastInput.IsZero()
}

// HasDisplayName reports whether a display name has been explicitly set.
func (a *Agent) HasDisplayName() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.displayName != ""
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
// Safe to call multiple times — subsequent calls are no-ops.
func (a *Agent) Kill() {
	a.killOnce.Do(func() {
		close(a.stop)
		a.pty.Close()
		// Close terminal to unblock writeLoop's Read call.
		a.terminal.Close()
	})
	// Wait for writeLoop to finish before returning.
	<-a.writeLoopDone
}

// ClaudeSessionDir overrides the session directory for testing.
// When empty, claudeSessionDir() uses the real home directory.
var ClaudeSessionDir string

func claudeSessionDir() string {
	if ClaudeSessionDir != "" {
		return ClaudeSessionDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "sessions")
}

// PollClaudeSessionName reads the Claude session file for this agent's PID
// and returns the "name" field if present. Returns "" on any error or if
// the name field is not yet populated.
func (a *Agent) PollClaudeSessionName() string {
	a.mu.RLock()
	pid := a.claudePid
	a.mu.RUnlock()

	if pid == 0 {
		return ""
	}

	dir := claudeSessionDir()
	if dir == "" {
		return ""
	}

	path := filepath.Join(dir, fmt.Sprintf("%d.json", pid))
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var session struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return ""
	}
	return session.Name
}

// HasClaudeName reports whether the agent has been given a Claude session name.
func (a *Agent) HasClaudeName() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.hasClaudeName
}

// SetClaudeName sets whether the agent has been given a Claude session name.
func (a *Agent) SetClaudeName(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hasClaudeName = v
}

