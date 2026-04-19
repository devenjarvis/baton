package github

import "time"

// PRState holds the state of a GitHub pull request.
type PRState struct {
	Number     int
	Title      string
	URL        string
	State      string // "open", "closed", "merged"
	Mergeable  bool
	Draft      bool
	HeadBranch string // branch the PR is from
	BaseBranch string // branch the PR targets
}

// CheckStatus holds the combined check/CI status for a git ref.
type CheckStatus struct {
	State   string // "success", "failure", "pending"
	Total   int
	Passed  int
	Failed  int
	Pending int
	Runs    []CheckRun
}

// CheckRun holds details about a single check run.
type CheckRun struct {
	Name       string
	Status     string // "queued", "in_progress", "completed"
	Conclusion string // "success", "failure", "cancelled", "skipped", etc.
	StartedAt  time.Time
	Duration   time.Duration
}

// ReviewStatus holds the aggregated review status for a PR.
type ReviewStatus struct {
	State            string // "approved", "changes_requested", "pending"
	Approved         int
	Pending          int
	ChangesRequested int
}
