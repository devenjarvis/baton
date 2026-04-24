package agent

import (
	"os"
	"path/filepath"
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
	oldPath := wt.Path

	s := newSession("session-1", "warm-ibis", wt)

	if s.HasClaudeName() {
		t.Error("HasClaudeName() should be false before rename")
	}

	actual, err := s.RenameBranch(repo, "baton/add-dark-mode", "")
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

	// The on-disk worktree directory is moved to match the new branch name so
	// Session/Branch/Worktree all stay congruent.
	if got := filepath.Base(s.Worktree.Path); got != "add-dark-mode" {
		t.Errorf("Worktree.Path basename = %q, want %q", got, "add-dark-mode")
	}
	if s.Worktree.Name != "add-dark-mode" {
		t.Errorf("Worktree.Name = %q, want %q", s.Worktree.Name, "add-dark-mode")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old worktree path %q should not exist after rename (err=%v)", oldPath, err)
	}
	if _, err := os.Stat(s.Worktree.Path); err != nil {
		t.Errorf("new worktree path %q should exist after rename: %v", s.Worktree.Path, err)
	}

	// Second rename is a no-op.
	second, err := s.RenameBranch(repo, "baton/second-attempt", "")
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

// TestSessionRenameBranch_WorktreeCollision verifies that when the target
// worktree directory is already occupied on disk, Session.RenameBranch still
// relocates the worktree — falling back to a "-2" suffix — so the session
// name, branch, and on-disk directory remain congruent.
func TestSessionRenameBranch_WorktreeCollision(t *testing.T) {
	repo := setupTestRepo(t)

	wt, err := git.CreateWorktree(repo, "warm-ibis", "", "", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	oldPath := wt.Path

	// Occupy the primary destination with a non-empty unrelated directory.
	blockerPath := filepath.Join(repo, ".baton", "worktrees", "add-dark-mode")
	if err := os.MkdirAll(blockerPath, 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockerPath, "sentinel"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	s := newSession("session-1", "warm-ibis", wt)

	actual, err := s.RenameBranch(repo, "baton/add-dark-mode", "")
	if err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if actual != "baton/add-dark-mode" {
		t.Errorf("branch rename should still succeed, got %q", actual)
	}
	if !s.HasClaudeName() {
		t.Error("HasClaudeName should be true after rename")
	}
	if filepath.Base(s.Worktree.Path) != "add-dark-mode-2" {
		t.Errorf("worktree path basename = %q, want %q", filepath.Base(s.Worktree.Path), "add-dark-mode-2")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old worktree path %q should be gone (err=%v)", oldPath, err)
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
	_, err = s.RenameBranch(repo, "", "")
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
