package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	xvt "github.com/charmbracelet/x/vt"
)

// setupTestRepo creates a temporary git repo with an initial commit.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	// Create initial file and commit.
	if err := os.WriteFile(dir+"/README.md", []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", args, err, out)
		}
	}

	return dir
}

func TestAgentRenderContainsOutput(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-echo", Task: "echo hello", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo hello; sleep 0.5")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for output.
	time.Sleep(500 * time.Millisecond)

	render := a.Render()
	if !strings.Contains(render, "hello") {
		t.Errorf("expected render to contain 'hello', got: %q", render)
	}
}

func TestAgentStatusTransitions(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-status", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo started; sleep 0.3")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should start as Starting.
	initialStatus := a.Status()
	if initialStatus != StatusStarting && initialStatus != StatusActive {
		t.Errorf("expected Starting or Active initially, got %s", initialStatus)
	}

	// Wait for output to trigger Active.
	time.Sleep(300 * time.Millisecond)
	if s := a.Status(); s != StatusActive && s != StatusDone {
		t.Errorf("expected Active or Done after output, got %s", s)
	}

	// Wait for process to exit.
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent to finish")
	}

	if s := a.Status(); s != StatusDone {
		t.Errorf("expected Done after exit, got %s", s)
	}
}

func TestMultipleSessionsUniqueWorktrees(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	sessions := make([]*Session, 3)
	for i := 0; i < 3; i++ {
		cfg := Config{Task: "test", Rows: 24, Cols: 80}
		sess, _, err := mgr.CreateSessionWithCommand(cfg, func(n string) *exec.Cmd {
			return exec.Command("bash", "-c", "sleep 2")
		})
		if err != nil {
			t.Fatal(err)
		}
		sessions[i] = sess
	}

	// Check unique worktree paths.
	paths := make(map[string]bool)
	for _, s := range sessions {
		if paths[s.Worktree.Path] {
			t.Errorf("duplicate worktree path: %s", s.Worktree.Path)
		}
		paths[s.Worktree.Path] = true
	}

	if mgr.AgentCount() != 3 {
		t.Errorf("expected 3 agents, got %d", mgr.AgentCount())
	}
}

func TestKillAndCleanup(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-kill", Task: "test", Rows: 24, Cols: 80}
	sess, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	wtPath := sess.Worktree.Path

	if err := mgr.KillSession(sess.ID); err != nil {
		t.Fatal(err)
	}

	// Worktree directory should be gone.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir to be removed, but it still exists")
	}

	if mgr.AgentCount() != 0 {
		t.Errorf("expected 0 agents after kill, got %d", mgr.AgentCount())
	}
}

func TestConfig_BypassPermissionsField(t *testing.T) {
	// Verify the BypassPermissions field exists and can be set
	cfg := Config{
		Name:              "test",
		Task:              "do something",
		Rows:              24,
		Cols:              80,
		BypassPermissions: true,
	}
	if !cfg.BypassPermissions {
		t.Error("BypassPermissions field should be settable to true")
	}

	cfg2 := Config{Name: "test2", Task: "task"}
	if cfg2.BypassPermissions {
		t.Error("BypassPermissions should default to false")
	}
}

func TestIdleSuppressedWhileTyping(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-idle-typing", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		// cat reads stdin forever, producing initial output then waiting.
		return exec.Command("bash", "-c", "echo ready; cat")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial output to set status to Active.
	time.Sleep(500 * time.Millisecond)
	if s := a.Status(); s != StatusActive {
		t.Fatalf("expected Active after output, got %s", s)
	}

	// Simulate user typing every 500ms for 5 seconds (well past the 3s idle timeout).
	for i := 0; i < 10; i++ {
		a.SendText("x")
		time.Sleep(500 * time.Millisecond)
	}

	// Agent should still be Active because user input keeps it non-idle.
	if s := a.Status(); s != StatusActive {
		t.Errorf("expected Active while typing, got %s", s)
	}
}

func TestIdleTransitionWithoutInput(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-idle-no-input", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		// Produce output then go quiet — no user input at all.
		return exec.Command("bash", "-c", "echo ready; sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial output.
	time.Sleep(500 * time.Millisecond)
	if s := a.Status(); s != StatusActive {
		t.Fatalf("expected Active after output, got %s", s)
	}

	// Wait past idle timeout (3s) + statusLoop tick (500ms) margin.
	time.Sleep(4 * time.Second)

	if s := a.Status(); s != StatusIdle {
		t.Errorf("expected Idle after timeout with no input, got %s", s)
	}
}

func TestIdleWhileComposingUsesLongerTimeout(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-composing-idle", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo ready; cat")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial output to become Active.
	time.Sleep(500 * time.Millisecond)
	if s := a.Status(); s != StatusActive {
		t.Fatalf("expected Active after output, got %s", s)
	}

	// Type some text (sets composing = true), then stop typing.
	a.SendText("hello")

	// Wait 5s — past the normal 3s idle timeout but within composing 30s timeout.
	time.Sleep(5 * time.Second)

	if s := a.Status(); s != StatusActive {
		t.Errorf("expected Active while composing (5s pause), got %s", s)
	}
}

func TestIdleAfterEnterUsesNormalTimeout(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-enter-idle", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo ready; cat")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for initial output.
	time.Sleep(500 * time.Millisecond)
	if s := a.Status(); s != StatusActive {
		t.Fatalf("expected Active after output, got %s", s)
	}

	// Type text then press Enter (clears composing).
	a.SendText("hello")
	a.SendKey(xvt.KeyPressEvent{Code: xvt.KeyEnter})

	// Wait past normal 3s idle timeout + tick margin.
	// cat echoes input back, resetting lastOutput, so allow extra margin.
	time.Sleep(5 * time.Second)

	if s := a.Status(); s != StatusIdle {
		t.Errorf("expected Idle after Enter + 4s, got %s", s)
	}
}

func TestComposingClearedOnEnter(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-composing-clear", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo ready; cat")
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	// SendText sets composing = true.
	a.SendText("hello")
	a.mu.RLock()
	if !a.composing {
		a.mu.RUnlock()
		t.Fatal("expected composing to be true after SendText")
	}
	a.mu.RUnlock()

	// SendKey(Enter) clears composing.
	a.SendKey(xvt.KeyPressEvent{Code: xvt.KeyEnter})
	a.mu.RLock()
	if a.composing {
		a.mu.RUnlock()
		t.Fatal("expected composing to be false after Enter")
	}
	a.mu.RUnlock()
}

func TestNewShellAgent(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{Rows: 24, Cols: 80}
	a, err := newShellAgent("test-shell-1", cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Kill()

	// Verify IsShell flag.
	if !a.IsShell {
		t.Error("expected IsShell to be true")
	}
	if a.Name != "shell" {
		t.Errorf("expected Name 'shell', got %q", a.Name)
	}
	if a.GetDisplayName() != "shell" {
		t.Errorf("expected display name 'shell', got %q", a.GetDisplayName())
	}

	// Send a command to trigger output.
	a.SendText("echo hello\n")
	time.Sleep(500 * time.Millisecond)

	// Should transition to Active on output.
	if s := a.Status(); s != StatusActive {
		t.Errorf("expected Active after shell output, got %s", s)
	}

	// Verify output appears in render.
	render := a.Render()
	if !strings.Contains(render, "hello") {
		t.Errorf("expected render to contain 'hello', got: %q", render)
	}

	// Shell agents should NOT transition to Idle (no statusLoop).
	time.Sleep(4 * time.Second)
	if s := a.Status(); s == StatusIdle {
		t.Error("shell agent should not transition to Idle (no statusLoop)")
	}
}

func TestNaturalExitCleansUpGoroutines(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-natural-exit", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo done")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the agent to exit naturally.
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent to finish")
	}

	// writeLoopDone should already be closed (goroutine cleaned up).
	select {
	case <-a.writeLoopDone:
	default:
		t.Error("writeLoopDone should be closed after natural exit")
	}

	if s := a.Status(); s != StatusDone {
		t.Errorf("expected Done, got %s", s)
	}
}

func TestKillAfterNaturalExitDoesNotPanic(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Name: "test-kill-after-exit", Task: "test", Rows: 24, Cols: 80}
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo done")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for natural exit.
	select {
	case <-a.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent to finish")
	}

	// Kill on an already-exited agent must not panic.
	a.Kill()
}

func TestShutdownCleansAll(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)

	for i := 0; i < 3; i++ {
		cfg := Config{Task: "test", Rows: 24, Cols: 80}
		_, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
			return exec.Command("bash", "-c", "sleep 60")
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	mgr.Shutdown()

	if mgr.AgentCount() != 0 {
		t.Errorf("expected 0 agents after shutdown, got %d", mgr.AgentCount())
	}
}
