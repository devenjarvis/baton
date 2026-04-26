package agent

import (
	"os/exec"
	"testing"

	"github.com/devenjarvis/baton/internal/git"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"fix: auth bug in login", "fix-auth-bug-in-login"},
		{"  spaces  and  stuff  ", "spaces-and-stuff"},
		{"UPPERCASE", "uppercase"},
		{"a-b-c", "a-b-c"},
		{"123-start", "123-start"},
		{"", ""},
		{"!@#$%", ""},
		{"a very long string that exceeds the forty character limit for slugs yes", "a-very-long-string-that-exceeds-the-fort"},
	}

	for _, tc := range tests {
		got := slugify(tc.input)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSessionGetDisplayName_Fallback(t *testing.T) {
	s := &Session{
		Name:   "eager-panda",
		agents: make(map[string]*Agent),
	}

	// Should fall back to Name.
	if got := s.GetDisplayName(); got != "eager-panda" {
		t.Errorf("GetDisplayName() = %q, want %q", got, "eager-panda")
	}

	if s.HasDisplayName() {
		t.Error("HasDisplayName() should be false before SetDisplayName")
	}

	s.SetDisplayName("fix-auth-bug")
	if got := s.GetDisplayName(); got != "fix-auth-bug" {
		t.Errorf("GetDisplayName() = %q, want %q", got, "fix-auth-bug")
	}

	if !s.HasDisplayName() {
		t.Error("HasDisplayName() should be true after SetDisplayName")
	}
}

func TestSessionRenameBranch(t *testing.T) {
	repo := setupTestRepo(t)

	wt, err := git.CreateWorktree(repo, "warm-ibis", "", "", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	origPath := wt.Path

	s := newSession("session-1", "warm-ibis", wt)

	if s.HasClaudeName() {
		t.Error("HasClaudeName() should be false before rename")
	}

	actual, err := s.RenameBranch(repo, "baton/add-dark-mode")
	if err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if actual != "baton/add-dark-mode" {
		t.Errorf("expected branch %q, got %q", "baton/add-dark-mode", actual)
	}
	if s.Worktree.Branch != "baton/add-dark-mode" {
		t.Errorf("Worktree.Branch = %q, want %q", s.Worktree.Branch, "baton/add-dark-mode")
	}
	if s.Name != "add-dark-mode" {
		t.Errorf("Session.Name = %q, want %q", s.Name, "add-dark-mode")
	}
	if !s.HasClaudeName() {
		t.Error("HasClaudeName() should be true after rename")
	}

	// The on-disk worktree path must NOT be moved during rename — moving it
	// would yank the directory out from under the running Claude process.
	if s.Worktree.Path != origPath {
		t.Errorf("Worktree.Path changed during rename: got %q, want %q", s.Worktree.Path, origPath)
	}

	// Second rename is a no-op.
	second, err := s.RenameBranch(repo, "baton/second-attempt")
	if err != nil {
		t.Fatalf("second RenameBranch: %v", err)
	}
	if second != "baton/add-dark-mode" {
		t.Errorf("second rename should be no-op, got %q", second)
	}
	if s.Name != "add-dark-mode" {
		t.Errorf("second rename should not change Name, got %q", s.Name)
	}
}

func TestAgentAutoNamedAsTrack(t *testing.T) {
	repo := setupTestRepo(t)
	wt, err := git.CreateWorktree(repo, "bohemian-rhapsody", "", "", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	s := newSession("session-1", "bohemian-rhapsody", wt)
	bash := func(name string) *exec.Cmd { return exec.Command("bash", "-c", "sleep 5") }

	// First agent with no explicit name gets track-1 / Track 1.
	a1, err := s.AddAgent(Config{Rows: 24, Cols: 80}, bash)
	if err != nil {
		t.Fatalf("AddAgent agent 1: %v", err)
	}
	if a1.Name != "track-1" {
		t.Errorf("agent 1 Name = %q, want track-1", a1.Name)
	}
	if got := a1.GetDisplayName(); got != "Track 1" {
		t.Errorf("agent 1 GetDisplayName = %q, want Track 1", got)
	}

	// Second agent with no explicit name gets track-2 / Track 2.
	a2, err := s.AddAgent(Config{Rows: 24, Cols: 80}, bash)
	if err != nil {
		t.Fatalf("AddAgent agent 2: %v", err)
	}
	if a2.Name != "track-2" {
		t.Errorf("agent 2 Name = %q, want track-2", a2.Name)
	}
	if got := a2.GetDisplayName(); got != "Track 2" {
		t.Errorf("agent 2 GetDisplayName = %q, want Track 2", got)
	}

	// Explicit cfg.Name bypasses track numbering.
	a3, err := s.AddAgent(Config{Name: "my-custom-name", Rows: 24, Cols: 80}, bash)
	if err != nil {
		t.Fatalf("AddAgent agent 3: %v", err)
	}
	if a3.Name != "my-custom-name" {
		t.Errorf("agent 3 Name = %q, want my-custom-name", a3.Name)
	}
}

func TestSessionRenameBranch_FailureLeavesStateUnchanged(t *testing.T) {
	repo := setupTestRepo(t)

	wt, err := git.CreateWorktree(repo, "warm-ibis", "", "", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	s := newSession("session-1", "warm-ibis", wt)
	origBranch := wt.Branch
	origName := s.Name

	// Pin git config so rename would otherwise succeed, then sabotage via an
	// empty target which RenameBranch rejects without touching state.
	_, err = s.RenameBranch(repo, "")
	if err == nil {
		t.Fatal("expected error for empty target")
	}

	if s.Worktree.Branch != origBranch {
		t.Errorf("Worktree.Branch changed on failure: got %q, want %q", s.Worktree.Branch, origBranch)
	}
	if s.Name != origName {
		t.Errorf("Session.Name changed on failure: got %q, want %q", s.Name, origName)
	}
	if s.HasClaudeName() {
		t.Error("HasClaudeName() should stay false on failure")
	}
}
