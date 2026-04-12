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

// IsRepo reports whether path is inside a git repository.
// It runs `git rev-parse --git-dir` in path and returns true on success.
func IsRepo(path string) bool {
	_, err := runGit(path, "rev-parse", "--git-dir")
	return err == nil
}

// BaseBranch returns the current branch name for the given repo.
func BaseBranch(repoPath string) (string, error) {
	return runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
}

// UpdateBaseBranch fetches the given branch from origin and attempts to
// fast-forward the local ref to match. This is best-effort: if the fetch
// fails (e.g. offline), an error is returned. If the fast-forward fails
// (e.g. local has diverged), the error is silently ignored since the fetch
// already updated origin/<branch> which can be used as a start point.
func UpdateBaseBranch(repoPath, branch string) error {
	if _, err := runGit(repoPath, "fetch", "origin", branch); err != nil {
		return fmt.Errorf("fetching origin/%s: %w", branch, err)
	}

	// Check if local is ancestor of remote (safe to fast-forward).
	if _, err := runGit(repoPath, "merge-base", "--is-ancestor", branch, "origin/"+branch); err != nil {
		// Local has diverged — skip fast-forward, but fetch succeeded.
		return nil
	}

	// Fast-forward the local branch.
	current, _ := BaseBranch(repoPath)
	if current == branch {
		// Branch is checked out — use merge --ff-only.
		runGit(repoPath, "merge", "--ff-only", "origin/"+branch)
	} else {
		// Branch is not checked out — update ref directly.
		runGit(repoPath, "branch", "-f", branch, "origin/"+branch)
	}

	return nil
}

// CreateWorktree creates a new git worktree for the named agent.
// branchPrefix and worktreeDir control naming and placement; pass empty
// strings to use defaults ("baton/" and ".baton/worktrees").
// An optional startPoint specifies the commit to branch from; if omitted,
// the worktree branches from the current HEAD.
func CreateWorktree(repoPath, agentName, branchPrefix, worktreeDir string, startPoint ...string) (*WorktreeInfo, error) {
	if branchPrefix == "" {
		branchPrefix = "baton/"
	}
	if worktreeDir == "" {
		worktreeDir = ".baton/worktrees"
	}

	base, err := BaseBranch(repoPath)
	if err != nil {
		return nil, fmt.Errorf("getting base branch: %w", err)
	}

	branch := branchPrefix + agentName
	wtPath := filepath.Join(repoPath, worktreeDir, agentName)

	args := []string{"worktree", "add", "-b", branch, wtPath}
	if len(startPoint) > 0 && startPoint[0] != "" {
		args = append(args, startPoint[0])
	}

	if _, err := runGit(repoPath, args...); err != nil {
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

// AttachWorktree creates a new git worktree that checks out an existing branch.
// Unlike CreateWorktree, it does NOT create a new branch (no -b flag).
// For remote-only branches, it fetches from origin first so git can
// auto-create the local tracking branch.
// worktreeDir defaults to ".baton/worktrees" if empty.
func AttachWorktree(repoPath, name, worktreeDir, branch string) (*WorktreeInfo, error) {
	if worktreeDir == "" {
		worktreeDir = ".baton/worktrees"
	}

	// Check if the branch exists locally.
	_, localErr := runGit(repoPath, "rev-parse", "--verify", branch)

	if localErr != nil {
		// Local branch doesn't exist — try fetching from origin.
		// This handles the case where the remote knows about the branch
		// but we haven't fetched it yet.
		if _, err := runGit(repoPath, "fetch", "origin", branch); err != nil {
			// Fetch failed — branch doesn't exist on origin either.
			return nil, fmt.Errorf("branch %q not found locally or on origin", branch)
		}
	}

	base, err := BaseBranch(repoPath)
	if err != nil {
		return nil, fmt.Errorf("getting base branch: %w", err)
	}

	wtPath := filepath.Join(repoPath, worktreeDir, name)

	if _, err := runGit(repoPath, "worktree", "add", wtPath, branch); err != nil {
		return nil, fmt.Errorf("attaching worktree: %w", err)
	}

	absPath, err := filepath.Abs(wtPath)
	if err != nil {
		return nil, err
	}

	return &WorktreeInfo{
		Name:       name,
		Path:       absPath,
		Branch:     branch,
		BaseBranch: base,
	}, nil
}

// ListLocalBranches returns the names of all local branches in the repo.
func ListLocalBranches(repoPath string) ([]string, error) {
	out, err := runGit(repoPath, "branch", "--format", "%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("listing local branches: %w", err)
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == "HEAD" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// ListRemoteBranches returns the names of all remote branches, with the
// "origin/" prefix stripped and HEAD entries filtered out.
func ListRemoteBranches(repoPath string) ([]string, error) {
	out, err := runGit(repoPath, "branch", "-r", "--format", "%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("listing remote branches: %w", err)
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		// Strip origin/ prefix.
		short := strings.TrimPrefix(name, "origin/")
		if short == "HEAD" {
			continue
		}
		branches = append(branches, short)
	}
	return branches, nil
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
// branchPrefix controls which branches are considered baton-managed;
// pass empty string to use the default ("baton/").
func ListWorktrees(repoPath, branchPrefix string) ([]*WorktreeInfo, error) {
	if branchPrefix == "" {
		branchPrefix = "baton/"
	}

	out, err := runGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	base, _ := BaseBranch(repoPath)
	branchRef := "branch refs/heads/" + branchPrefix

	var worktrees []*WorktreeInfo
	var current *WorktreeInfo

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path := strings.TrimPrefix(line, "worktree ")
			current = &WorktreeInfo{Path: path}
		case strings.HasPrefix(line, branchRef):
			if current != nil {
				branch := strings.TrimPrefix(line, "branch refs/heads/")
				name := strings.TrimPrefix(branch, branchPrefix)
				current.Branch = branch
				current.Name = name
				current.BaseBranch = base
				worktrees = append(worktrees, current)
			}
		case line == "":
			current = nil
		}
	}

	return worktrees, nil
}

// PushBranch pushes the given branch to origin with upstream tracking.
func PushBranch(repoPath, branch string) error {
	_, err := runGit(repoPath, "push", "-u", "origin", branch)
	return err
}

// GetRemoteURL returns the URL for the "origin" remote of the repo at repoPath.
func GetRemoteURL(repoPath string) (string, error) {
	return runGit(repoPath, "remote", "get-url", "origin")
}
