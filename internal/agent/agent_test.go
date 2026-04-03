package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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

func TestMultipleAgentsUniqueWorktrees(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	agents := make([]*Agent, 3)
	names := []string{"alpha", "beta", "gamma"}

	for i, name := range names {
		cfg := Config{Name: name, Task: "test", Rows: 24, Cols: 80}
		a, err := mgr.CreateWithCommand(cfg, func(n string) *exec.Cmd {
			return exec.Command("bash", "-c", "sleep 2")
		})
		if err != nil {
			t.Fatal(err)
		}
		agents[i] = a
	}

	// Check unique worktree paths.
	paths := make(map[string]bool)
	for _, a := range agents {
		if paths[a.Worktree.Path] {
			t.Errorf("duplicate worktree path: %s", a.Worktree.Path)
		}
		paths[a.Worktree.Path] = true
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
	a, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	wtPath := a.Worktree.Path

	if err := mgr.Kill(a.ID); err != nil {
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

func TestShutdownCleansAll(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)

	for i := 0; i < 3; i++ {
		cfg := Config{Name: "shut-" + string(rune('a'+i)), Task: "test", Rows: 24, Cols: 80}
		_, err := mgr.CreateWithCommand(cfg, func(name string) *exec.Cmd {
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
