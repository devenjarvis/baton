package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devenjarvis/baton/internal/git"
)

// initTestRepo creates a temporary git repo with an initial commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "checkout", "-b", "main"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", args, err, out)
		}
	}

	// Create an initial file and commit so the repo is not empty.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %v\n%s", args, err, out)
		}
	}

	return dir
}

func TestBaseBranch(t *testing.T) {
	repo := initTestRepo(t)

	branch, err := git.BaseBranch(repo)
	if err != nil {
		t.Fatalf("BaseBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected 'main', got %q", branch)
	}
}

func TestCreateWorktree(t *testing.T) {
	repo := initTestRepo(t)

	wt, err := git.CreateWorktree(repo, "agent1")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Worktree directory should exist.
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Errorf("worktree path %s does not exist", wt.Path)
	}

	// Branch should be baton/agent1.
	if wt.Branch != "baton/agent1" {
		t.Errorf("expected branch 'baton/agent1', got %q", wt.Branch)
	}

	// BaseBranch should be main.
	if wt.BaseBranch != "main" {
		t.Errorf("expected base branch 'main', got %q", wt.BaseBranch)
	}

	// The branch should exist in git.
	cmd := exec.Command("git", "branch", "--list", "baton/agent1")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --list: %v", err)
	}
	if !strings.Contains(string(out), "baton/agent1") {
		t.Errorf("branch baton/agent1 not found in git branch output: %s", out)
	}
}

func TestListWorktrees(t *testing.T) {
	repo := initTestRepo(t)

	_, err := git.CreateWorktree(repo, "agent1")
	if err != nil {
		t.Fatalf("CreateWorktree agent1: %v", err)
	}
	_, err = git.CreateWorktree(repo, "agent2")
	if err != nil {
		t.Fatalf("CreateWorktree agent2: %v", err)
	}

	list, err := git.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}

	names := make(map[string]bool)
	for _, w := range list {
		names[w.Name] = true
	}

	if !names["agent1"] || !names["agent2"] {
		t.Errorf("expected agent1 and agent2 in list, got %v", names)
	}
}

func TestDiffAndMerge(t *testing.T) {
	repo := initTestRepo(t)

	wt, err := git.CreateWorktree(repo, "agent1")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Make a change in the worktree and commit it.
	newFile := filepath.Join(wt.Path, "feature.txt")
	if err := os.WriteFile(newFile, []byte("new feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "feature.txt"},
		{"git", "commit", "-m", "add feature"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wt.Path
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, out)
		}
	}

	// Diff should show the change.
	diff, err := git.Diff(repo, wt)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Errorf("diff should mention feature.txt, got:\n%s", diff)
	}
	if !strings.Contains(diff, "new feature") {
		t.Errorf("diff should contain 'new feature', got:\n%s", diff)
	}

	// DiffStats should report files/insertions.
	stats, err := git.GetDiffStats(repo, wt)
	if err != nil {
		t.Fatalf("GetDiffStats: %v", err)
	}
	if stats.Files < 1 {
		t.Errorf("expected at least 1 file changed, got %d", stats.Files)
	}
	if stats.Insertions < 1 {
		t.Errorf("expected at least 1 insertion, got %d", stats.Insertions)
	}

	// Merge the worktree back.
	if err := git.MergeWorktree(repo, wt, "merge agent1 work"); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}

	// The merged file should now exist on the base branch.
	mergedFile := filepath.Join(repo, "feature.txt")
	data, err := os.ReadFile(mergedFile)
	if err != nil {
		t.Fatalf("reading merged file: %v", err)
	}
	if string(data) != "new feature\n" {
		t.Errorf("expected 'new feature\\n', got %q", string(data))
	}
}

func TestRemoveWorktree(t *testing.T) {
	repo := initTestRepo(t)

	wt, err := git.CreateWorktree(repo, "agent1")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := git.RemoveWorktree(repo, wt, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	// Directory should be gone.
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree path %s still exists", wt.Path)
	}

	// Branch should be deleted.
	cmd := exec.Command("git", "branch", "--list", "baton/agent1")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --list: %v", err)
	}
	if strings.Contains(string(out), "baton/agent1") {
		t.Errorf("branch baton/agent1 should be deleted but still exists")
	}
}
