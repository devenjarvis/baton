package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeInfo holds metadata about an agent's git worktree.
type WorktreeInfo struct {
	Name       string // agent name
	Path       string // absolute path to worktree dir
	Branch     string // branch name (baton/<name>)
	BaseBranch string // branch worktree was created from
}

// runGit executes a git command in the given directory and returns its output.
// On error, the returned error includes stderr for debugging.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// BaseBranch returns the current branch name for the given repo.
func BaseBranch(repoPath string) (string, error) {
	return runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
}

// CreateWorktree creates a new git worktree for the named agent.
// The worktree is placed at .baton/worktrees/<name> with branch baton/<name>.
func CreateWorktree(repoPath, agentName string) (*WorktreeInfo, error) {
	base, err := BaseBranch(repoPath)
	if err != nil {
		return nil, fmt.Errorf("getting base branch: %w", err)
	}

	branch := "baton/" + agentName
	wtPath := filepath.Join(repoPath, ".baton", "worktrees", agentName)

	if _, err := runGit(repoPath, "worktree", "add", "-b", branch, wtPath); err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}

	absPath, err := filepath.Abs(wtPath)
	if err != nil {
		return nil, err
	}

	return &WorktreeInfo{
		Name:       agentName,
		Path:       absPath,
		Branch:     branch,
		BaseBranch: base,
	}, nil
}

// RemoveWorktree removes a worktree and optionally deletes its branch.
func RemoveWorktree(repoPath string, wt *WorktreeInfo, deleteBranch bool) error {
	if _, err := runGit(repoPath, "worktree", "remove", "--force", wt.Path); err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	if deleteBranch {
		if _, err := runGit(repoPath, "branch", "-D", wt.Branch); err != nil {
			return fmt.Errorf("deleting branch: %w", err)
		}
	}

	return nil
}

// ListWorktrees returns all baton-managed worktrees in the repo.
// It identifies baton worktrees by their branch prefix "baton/".
func ListWorktrees(repoPath string) ([]*WorktreeInfo, error) {
	out, err := runGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []*WorktreeInfo
	var current *WorktreeInfo

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path := strings.TrimPrefix(line, "worktree ")
			current = &WorktreeInfo{Path: path}
		case strings.HasPrefix(line, "branch refs/heads/baton/"):
			if current != nil {
				branch := strings.TrimPrefix(line, "branch refs/heads/")
				name := strings.TrimPrefix(branch, "baton/")
				current.Branch = branch
				current.Name = name
				worktrees = append(worktrees, current)
			}
		case line == "":
			current = nil
		}
	}

	return worktrees, nil
}
