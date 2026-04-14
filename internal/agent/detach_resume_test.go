package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/baton/internal/state"
)

func TestDetachSnapshotsState(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set a session ID on the agent to simulate polling.
	ag.SetClaudeSessionID("test-session-uuid")
	ag.SetDisplayName("my-task")
	ag.SetClaudeName(true)

	wtPath := sess.Worktree.Path
	sessID := sess.ID

	bs := mgr.Detach()
	if bs == nil {
		t.Fatal("expected non-nil BatonState from Detach")
	}

	// State should have the session.
	if len(bs.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(bs.Sessions))
	}

	ss := bs.Sessions[0]
	if ss.ID != sessID {
		t.Errorf("expected session ID %s, got %s", sessID, ss.ID)
	}
	if ss.WorktreePath != wtPath {
		t.Errorf("expected worktree path %s, got %s", wtPath, ss.WorktreePath)
	}

	// Agent state should be captured.
	if len(ss.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(ss.Agents))
	}
	as := ss.Agents[0]
	if as.ClaudeSessionID != "test-session-uuid" {
		t.Errorf("expected session ID 'test-session-uuid', got %q", as.ClaudeSessionID)
	}
	if as.DisplayName != "my-task" {
		t.Errorf("expected display name 'my-task', got %q", as.DisplayName)
	}

	// Worktree should still exist (not cleaned up).
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree should still exist after detach")
	}

	// Manager should have no sessions after detach.
	if mgr.AgentCount() != 0 {
		t.Errorf("expected 0 agents after detach, got %d", mgr.AgentCount())
	}
}

func TestDetachEmptyReturnsNil(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())

	bs := mgr.Detach()
	if bs != nil {
		t.Errorf("expected nil BatonState for empty manager, got %+v", bs)
	}
}

func TestDetachSaveLoadRoundTrip(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	ag.SetClaudeSessionID("uuid-abc")

	_ = sess // used implicitly

	bs := mgr.Detach()
	if bs == nil {
		t.Fatal("expected non-nil BatonState")
	}

	// Save to disk.
	if err := state.Save(repo, bs); err != nil {
		t.Fatal(err)
	}

	// Load from disk.
	loaded, err := state.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected loaded state, got nil")
	}
	if len(loaded.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(loaded.Sessions))
	}
	if loaded.Sessions[0].Agents[0].ClaudeSessionID != "uuid-abc" {
		t.Errorf("expected session ID 'uuid-abc', got %q", loaded.Sessions[0].Agents[0].ClaudeSessionID)
	}
}

func TestResumeSessionCreatesAgentWithResume(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH")
	}
	repo := setupTestRepo(t)

	// First manager: create a session and detach.
	mgr1 := NewManager(repo, defaultTestSettings())
	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr1.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	ag.SetClaudeSessionID("resume-uuid-123")
	ag.SetDisplayName("my-agent")
	ag.SetClaudeName(true)

	sessID := sess.ID
	sessName := sess.Name
	wtPath := sess.Worktree.Path
	branch := sess.Worktree.Branch
	baseBranch := sess.Worktree.BaseBranch

	bs := mgr1.Detach()
	if bs == nil {
		t.Fatal("expected non-nil BatonState")
	}

	// Second manager: resume from saved state.
	mgr2 := NewManager(repo, defaultTestSettings())
	defer mgr2.Shutdown()

	resumeCfg := Config{Rows: 24, Cols: 80}
	if err := mgr2.ResumeSession(bs.Sessions[0], resumeCfg); err != nil {
		t.Fatal(err)
	}

	// Verify session was recreated.
	resumedSess := mgr2.GetSession(sessID)
	if resumedSess == nil {
		t.Fatal("resumed session not found")
	}
	if resumedSess.Name != sessName {
		t.Errorf("expected session name %q, got %q", sessName, resumedSess.Name)
	}
	if resumedSess.Worktree.Path != wtPath {
		t.Errorf("expected worktree path %q, got %q", wtPath, resumedSess.Worktree.Path)
	}
	if resumedSess.Worktree.Branch != branch {
		t.Errorf("expected branch %q, got %q", branch, resumedSess.Worktree.Branch)
	}
	if resumedSess.Worktree.BaseBranch != baseBranch {
		t.Errorf("expected base branch %q, got %q", baseBranch, resumedSess.Worktree.BaseBranch)
	}

	// Verify agent was created.
	agents := resumedSess.Agents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].GetDisplayName() != "my-agent" {
		t.Errorf("expected display name 'my-agent', got %q", agents[0].GetDisplayName())
	}
	if agents[0].ClaudeSessionID() != "resume-uuid-123" {
		t.Errorf("expected claude session ID 'resume-uuid-123', got %q", agents[0].ClaudeSessionID())
	}
}

func TestResumeSessionMissingWorktreeReturnsError(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	ss := state.SessionState{
		ID:           "session-99",
		Name:         "nonexistent",
		WorktreePath: "/tmp/does-not-exist-" + time.Now().Format("20060102150405"),
		Branch:       "baton/nonexistent",
		BaseBranch:   "main",
		Agents: []state.AgentState{
			{ID: "session-99-agent-1", Name: "test"},
		},
	}

	err := mgr.ResumeSession(ss, Config{Rows: 24, Cols: 80})
	if err == nil {
		t.Fatal("expected error for missing worktree")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %s", err)
	}
}

func TestResumeNextIDNoCollision(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH")
	}
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	// Create a session and detach from a first manager to get the worktree.
	mgr1 := NewManager(repo, defaultTestSettings())
	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, _, err := mgr1.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}
	wtPath := sess.Worktree.Path
	mgr1.Detach()

	// Resume with a high session ID.
	ss := state.SessionState{
		ID:           "session-50",
		Name:         sess.Name,
		WorktreePath: wtPath,
		Branch:       sess.Worktree.Branch,
		BaseBranch:   sess.Worktree.BaseBranch,
		Agents: []state.AgentState{
			{ID: "session-50-agent-1", Name: "test"},
		},
	}

	if err := mgr.ResumeSession(ss, Config{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}

	// Create a new session — its ID should be > 50.
	newSess, _, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	if newSess.ID == "session-50" {
		t.Error("new session ID should not collide with resumed session")
	}
	// The nextID should have been bumped past 50, so the new session
	// should have an ID > 50.
	num := parseSessionNum(newSess.ID)
	if num <= 50 {
		t.Errorf("expected new session ID > 50, got %s (num=%d)", newSess.ID, num)
	}
}

func TestBuildResumeArgs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		sessID   string
		wantArgs []string
	}{
		{
			name:     "with session ID and bypass",
			cfg:      Config{BypassPermissions: true, Task: "do stuff"},
			sessID:   "uuid-123",
			wantArgs: []string{"--dangerously-skip-permissions", "--resume", "uuid-123", "do stuff"},
		},
		{
			name:     "empty session ID falls back to continue",
			cfg:      Config{BypassPermissions: true},
			sessID:   "",
			wantArgs: []string{"--dangerously-skip-permissions", "--continue"},
		},
		{
			name:     "no bypass no task",
			cfg:      Config{},
			sessID:   "uuid-456",
			wantArgs: []string{"--resume", "uuid-456"},
		},
		{
			name:     "continue with task",
			cfg:      Config{Task: "hello"},
			sessID:   "",
			wantArgs: []string{"--continue", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResumeArgs(tt.cfg, tt.sessID)
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.wantArgs), len(got), got)
			}
			for i, want := range tt.wantArgs {
				if got[i] != want {
					t.Errorf("arg[%d]: expected %q, got %q", i, want, got[i])
				}
			}
		})
	}
}

func TestForceQuitCleansEverything(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())

	cfg := Config{Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}

	ag.SetClaudeSessionID("uuid-force")
	wtPath := sess.Worktree.Path

	// Simulate force quit: Shutdown + Remove state.
	mgr.Shutdown()
	_ = state.Remove(repo)

	// Worktree should be gone (Shutdown calls Cleanup).
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after Shutdown")
	}

	// State file should not exist.
	loaded, err := state.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Error("expected no state file after force quit")
	}
}

func TestDetachResumePreservesOwnsBranch(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH")
	}
	repo := setupTestRepo(t)

	// Create a branch to attach to.
	cmd := exec.Command("git", "branch", "feature/test-detach")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating branch: %v\n%s", err, out)
	}

	mgr1 := NewManager(repo, defaultTestSettings())
	cfg := Config{Task: "test", Rows: 24, Cols: 80}

	// Create an attached session (ownsBranch=false).
	sess, _, err := mgr1.CreateSessionOnBranchWithCommand("feature/test-detach", "main", cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 60")
	})
	if err != nil {
		t.Fatal(err)
	}
	wtPath := sess.Worktree.Path

	bs := mgr1.Detach()
	if bs == nil {
		t.Fatal("expected non-nil BatonState")
	}

	// OwnsBranch should be false in saved state.
	if bs.Sessions[0].OwnsBranch {
		t.Error("expected OwnsBranch=false for attached session")
	}

	// Resume and verify.
	mgr2 := NewManager(repo, defaultTestSettings())
	defer mgr2.Shutdown()

	if err := mgr2.ResumeSession(bs.Sessions[0], Config{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}

	resumedSess := mgr2.GetSession(sess.ID)
	if resumedSess == nil {
		t.Fatal("resumed session not found")
	}
	if resumedSess.Worktree.Path != wtPath {
		t.Errorf("expected worktree path %s, got %s", wtPath, resumedSess.Worktree.Path)
	}

	// Kill the resumed session — branch should be preserved (ownsBranch=false).
	if err := mgr2.KillSession(sess.ID); err != nil {
		t.Fatal(err)
	}

	branchCmd := exec.Command("git", "branch", "--list", "feature/test-detach")
	branchCmd.Dir = repo
	out, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("git branch --list: %v", err)
	}
	if !strings.Contains(string(out), "feature/test-detach") {
		t.Error("branch should be preserved after killing resumed attached session")
	}
}

func TestParseSessionNum(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"session-1", 1},
		{"session-50", 50},
		{"session-0", 0},
		{"invalid", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseSessionNum(tt.input)
		if got != tt.want {
			t.Errorf("parseSessionNum(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
