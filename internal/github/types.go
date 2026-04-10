package github

// PRState holds the state of a GitHub pull request.
type PRState struct {
	Number    int
	Title     string
	URL       string
	State     string // "open", "closed", "merged"
	Mergeable bool
	Draft     bool
}

// CheckStatus holds the combined check/CI status for a git ref.
type CheckStatus struct {
	State        string // "success", "failure", "pending"
	Total        int
	Passed       int
	Failed       int
	Pending      int
	FailedChecks []FailedCheck
}

// FailedCheck holds details about a single failed check run.
type FailedCheck struct {
	ID         int64
	Name       string
	Conclusion string
	DetailsURL string
}
