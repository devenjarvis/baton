package git

import (
	"fmt"
	"strconv"
	"strings"
)

// DiffStats holds summary statistics for a diff.
type DiffStats struct {
	Files      int
	Insertions int
	Deletions  int
}

// Diff returns the full diff between the base branch and the worktree branch.
func Diff(repoPath string, wt *WorktreeInfo) (string, error) {
	out, err := runGit(repoPath, "diff", wt.BaseBranch+"..."+wt.Branch)
	if err != nil {
		return "", fmt.Errorf("getting diff: %w", err)
	}
	return out, nil
}

// GetDiffStats returns summary statistics for the diff between the base and worktree branches.
func GetDiffStats(repoPath string, wt *WorktreeInfo) (*DiffStats, error) {
	out, err := runGit(repoPath, "diff", "--numstat", wt.BaseBranch+"..."+wt.Branch)
	if err != nil {
		return nil, fmt.Errorf("getting diff stats: %w", err)
	}

	stats := &DiffStats{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Binary files show "-" for insertions/deletions.
		if fields[0] != "-" {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				stats.Insertions += n
			}
		}
		if fields[1] != "-" {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				stats.Deletions += n
			}
		}
		stats.Files++
	}

	return stats, nil
}

// MergeWorktree merges the worktree branch into the base branch using --no-ff.
// Returns an error if there are merge conflicts.
func MergeWorktree(repoPath string, wt *WorktreeInfo, message string) error {
	if _, err := runGit(repoPath, "merge", "--no-ff", "-m", message, wt.Branch); err != nil {
		return fmt.Errorf("merging worktree: %w", err)
	}
	return nil
}
