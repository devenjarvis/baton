package agent

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestSessionCreation(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 2")
	})
	if err != nil {
		t.Fatal(err)
	}

	if sess.Name == "" {
		t.Error("session name should not be empty")
	}
	if sess.Worktree == nil {
		t.Fatal("session worktree should not be nil")
	}
	if ag == nil {
		t.Fatal("first agent should not be nil")
	}
	if sess.AgentCount() != 1 {
		t.Errorf("expected 1 agent, got %d", sess.AgentCount())
	}
}

func TestMultipleAgentsShareWorktree(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag1, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	ag2, err := mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both agents should share the same worktree path.
	if ag1.WorktreePath != ag2.WorktreePath {
		t.Errorf("agents should share worktree path: %s != %s", ag1.WorktreePath, ag2.WorktreePath)
	}
	if ag1.WorktreePath != sess.Worktree.Path {
		t.Errorf("agent worktree path should match session: %s != %s", ag1.WorktreePath, sess.Worktree.Path)
	}
	if sess.AgentCount() != 2 {
		t.Errorf("expected 2 agents, got %d", sess.AgentCount())
	}
	if mgr.AgentCount() != 2 {
		t.Errorf("expected 2 total agents, got %d", mgr.AgentCount())
	}
}

func TestKillAgentSessionSurvives(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag1, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Kill just the first agent.
	if err := mgr.KillAgent(sess.ID, ag1.ID); err != nil {
		t.Fatal(err)
	}

	// Session should still exist with one agent.
	if mgr.GetSession(sess.ID) == nil {
		t.Error("session should still exist after killing one agent")
	}
	if sess.AgentCount() != 1 {
		t.Errorf("expected 1 agent remaining, got %d", sess.AgentCount())
	}
	if mgr.AgentCount() != 1 {
		t.Errorf("expected 1 total agent, got %d", mgr.AgentCount())
	}

	// Worktree should still exist.
	if _, err := os.Stat(sess.Worktree.Path); os.IsNotExist(err) {
		t.Error("worktree should still exist after killing one agent")
	}
}

func TestKillSessionCleansAll(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	wtPath := sess.Worktree.Path
	sessID := sess.ID

	if err := mgr.KillSession(sessID); err != nil {
		t.Fatal(err)
	}

	// Session should be gone.
	if mgr.GetSession(sessID) != nil {
		t.Error("session should be removed after KillSession")
	}
	// Worktree should be gone.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after KillSession")
	}
	if mgr.AgentCount() != 0 {
		t.Errorf("expected 0 agents, got %d", mgr.AgentCount())
	}
}

func TestSessionCompositeStatus(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	// Create a session with an agent that exits quickly.
	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo hi; sleep 0.3")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Initially should be starting or active.
	s := sess.Status()
	if s != StatusStarting && s != StatusActive {
		t.Errorf("expected Starting or Active initially, got %s", s)
	}

	// Add a second agent that runs longer.
	_, err = mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo world; sleep 2")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait briefly for output.
	time.Sleep(200 * time.Millisecond)
	s = sess.Status()
	if s != StatusActive && s != StatusStarting {
		t.Errorf("expected Active or Starting with running agents, got %s", s)
	}

	// Wait for the first agent to finish but the second is still running.
	time.Sleep(500 * time.Millisecond)
	// Session should still be active since agent 2 is still running.
	s = sess.Status()
	if s != StatusActive && s != StatusIdle {
		t.Errorf("expected Active or Idle (agent2 still running), got %s", s)
	}
}

func TestSessionAgentsSorted(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag1, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond) // ensure different CreatedAt

	ag2, err := mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	agents := sess.Agents()
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	if agents[0].ID != ag1.ID {
		t.Errorf("expected first agent %s, got %s", ag1.ID, agents[0].ID)
	}
	if agents[1].ID != ag2.ID {
		t.Errorf("expected second agent %s, got %s", ag2.ID, agents[1].ID)
	}
}

func TestKillLastAgentAutoClosesSession(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag1, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	ag2, err := mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}

	wtPath := sess.Worktree.Path
	sessID := sess.ID

	// Kill first agent — session should survive.
	if err := mgr.KillAgent(sessID, ag1.ID); err != nil {
		t.Fatal(err)
	}
	if mgr.GetSession(sessID) == nil {
		t.Fatal("session should still exist after killing first agent")
	}

	// Kill second agent — session should auto-close.
	if err := mgr.KillAgent(sessID, ag2.ID); err != nil {
		t.Fatal(err)
	}
	if mgr.GetSession(sessID) != nil {
		t.Error("session should be removed after killing last agent")
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after session auto-close")
	}

	// Verify EventSessionClosed was emitted.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-mgr.Events():
			if e.Type == EventSessionClosed && e.SessionID == sessID {
				return // success
			}
		case <-deadline:
			t.Error("expected EventSessionClosed event")
			return
		}
	}
}

func TestNaturalExitAutoClosesSession(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo done")
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "echo done")
	})
	if err != nil {
		t.Fatal(err)
	}

	wtPath := sess.Worktree.Path
	sessID := sess.ID

	// Wait for both agents to exit naturally and session to auto-close.
	deadline := time.After(5 * time.Second)
	gotSessionClosed := false
	for !gotSessionClosed {
		select {
		case e := <-mgr.Events():
			if e.Type == EventSessionClosed && e.SessionID == sessID {
				gotSessionClosed = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for EventSessionClosed")
		}
	}

	if mgr.GetSession(sessID) != nil {
		t.Error("session should be removed after all agents exit naturally")
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after session auto-close")
	}
}

func TestAddAgentDefaultAssignsUniqueName(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo)
	defer mgr.Shutdown()

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag1, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Agent should get a random name distinct from session name.
	if ag1.Name == "" {
		t.Fatal("agent 1 should have a non-empty name")
	}
	if ag1.Name == sess.Name {
		t.Errorf("agent name %q should differ from session name %q", ag1.Name, sess.Name)
	}

	// Add a second agent — should also get a unique name.
	ag2, err := mgr.AddAgentWithCommand(sess.ID, cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	if ag2.Name == "" {
		t.Fatal("agent 2 should have a non-empty name")
	}
	if ag2.Name == ag1.Name {
		t.Errorf("agent 2 name %q should differ from agent 1 name %q", ag2.Name, ag1.Name)
	}
	if ag2.Name == sess.Name {
		t.Errorf("agent 2 name %q should differ from session name %q", ag2.Name, sess.Name)
	}

	// Explicit names should be preserved.
	ag3, err := mgr.AddAgentWithCommand(sess.ID, Config{Name: "custom-name", Task: "test", Rows: 24, Cols: 80}, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	if ag3.Name != "custom-name" {
		t.Errorf("explicit name should be preserved, got %q", ag3.Name)
	}
}
