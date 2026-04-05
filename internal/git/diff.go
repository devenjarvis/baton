package git

import (
	"fmt"
	"sort"
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

// DiffFile holds the parsed diff for a single file.
type DiffFile struct {
	Status string   // "M", "A", or "D"
	Path   string   // file path from b/... header
	Lines  []string // all diff lines for this file including headers
}

// ParseDiffFiles splits a raw unified diff into per-file chunks.
func ParseDiffFiles(rawDiff string) []DiffFile {
	if rawDiff == "" {
		return []DiffFile{}
	}

	var files []DiffFile
	var current *DiffFile

	for _, line := range strings.Split(rawDiff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			// Flush the previous file.
			if current != nil {
				files = append(files, *current)
			}
			// Extract path from b/... part.
			path := ""
			fields := strings.Fields(line)
			// fields: ["diff", "--git", "a/foo", "b/foo"]
			if len(fields) >= 4 {
				bPart := fields[3]
				if strings.HasPrefix(bPart, "b/") {
					path = bPart[2:]
				} else {
					path = bPart
				}
			}
			current = &DiffFile{
				Status: "M",
				Path:   path,
				Lines:  []string{line},
			}
			continue
		}
		if current == nil {
			continue
		}
		if line == "new file mode" || strings.HasPrefix(line, "new file mode ") {
			current.Status = "A"
		} else if line == "deleted file mode" || strings.HasPrefix(line, "deleted file mode ") {
			current.Status = "D"
		}
		current.Lines = append(current.Lines, line)
	}

	// Flush the last file.
	if current != nil {
		files = append(files, *current)
	}

	return files
}

// FileStat holds per-file diff statistics.
type FileStat struct {
	Path       string // file path
	Status     string // "A", "M", or "D"
	Insertions int
	Deletions  int
}

// GetPerFileDiffStats returns per-file insertion/deletion counts and statuses,
// including uncommitted working tree changes. It also returns aggregate DiffStats.
// It diffs the worktree's current state (committed + uncommitted) against the base
// branch in a single pass to avoid double-counting.
func GetPerFileDiffStats(repoPath string, wt *WorktreeInfo) ([]FileStat, *DiffStats, error) {
	// Run from the worktree directory so that both committed and uncommitted
	// changes are captured in a single diff against the base branch.
	numstatOut, err := runGit(wt.Path, "diff", "--numstat", "--diff-filter=AMD", wt.BaseBranch)
	if err != nil {
		return nil, nil, fmt.Errorf("getting per-file numstat: %w", err)
	}

	nameStatusOut, err := runGit(wt.Path, "diff", "--name-status", "--diff-filter=AMD", wt.BaseBranch)
	if err != nil {
		return nil, nil, fmt.Errorf("getting per-file name-status: %w", err)
	}

	fileMap := make(map[string]*FileStat)
	parseNumstat(numstatOut, fileMap)
	parseNameStatus(nameStatusOut, fileMap)

	// Build result slice and aggregate stats.
	var result []FileStat
	agg := &DiffStats{}
	for _, fs := range fileMap {
		result = append(result, *fs)
		agg.Files++
		agg.Insertions += fs.Insertions
		agg.Deletions += fs.Deletions
	}

	// Sort by path for stable display order.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})

	return result, agg, nil
}

// parseNumstat parses `git diff --numstat` output and merges into fileMap.
// For existing entries, insertions and deletions are added (merged).
func parseNumstat(output string, fileMap map[string]*FileStat) {
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		path := fields[2]
		ins := 0
		del := 0
		if fields[0] != "-" {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				ins = n
			}
		}
		if fields[1] != "-" {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				del = n
			}
		}

		if existing, ok := fileMap[path]; ok {
			existing.Insertions += ins
			existing.Deletions += del
		} else {
			fileMap[path] = &FileStat{
				Path:       path,
				Insertions: ins,
				Deletions:  del,
			}
		}
	}
}

// parseNameStatus parses `git diff --name-status` output and sets statuses in fileMap.
// Later calls overwrite earlier statuses (uncommitted status takes precedence).
func parseNameStatus(output string, fileMap map[string]*FileStat) {
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := fields[1]

		if existing, ok := fileMap[path]; ok {
			existing.Status = status
		} else {
			fileMap[path] = &FileStat{
				Path:   path,
				Status: status,
			}
		}
	}
}

// MergeWorktree merges the worktree branch into the base branch using --no-ff.
// Returns an error if there are merge conflicts.
func MergeWorktree(repoPath string, wt *WorktreeInfo, message string) error {
	if _, err := runGit(repoPath, "merge", "--no-ff", "-m", message, wt.Branch); err != nil {
		return fmt.Errorf("merging worktree: %w", err)
	}
	return nil
}
