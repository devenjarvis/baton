package github

import (
	"context"
	"fmt"
	"time"

	gh "github.com/google/go-github/v69/github"
)

// Client wraps the GitHub API client with methods for PR and check operations.
type Client struct {
	gh *gh.Client
}

// NewClient creates a new GitHub API client using a token from GetToken().
func NewClient() (*Client, error) {
	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	client := gh.NewClient(nil).WithAuthToken(token)
	return &Client{gh: client}, nil
}

// GetPR finds an open pull request for the given head branch.
// Returns nil (not an error) if no open PR exists for the branch.
func (c *Client) GetPR(ctx context.Context, owner, repo, branch string) (*PRState, error) {
	prs, _, err := c.gh.PullRequests.List(ctx, owner, repo, &gh.PullRequestListOptions{
		Head:  owner + ":" + branch,
		State: "open",
		ListOptions: gh.ListOptions{
			PerPage: 1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing PRs: %w", err)
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return prToState(prs[0]), nil
}

// ListPRs returns open pull requests for the given repository (up to 100).
func (c *Client) ListPRs(ctx context.Context, owner, repo string) ([]*PRState, error) {
	prs, _, err := c.gh.PullRequests.List(ctx, owner, repo, &gh.PullRequestListOptions{
		State: "open",
		ListOptions: gh.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing PRs: %w", err)
	}

	states := make([]*PRState, len(prs))
	for i, pr := range prs {
		states[i] = prToState(pr)
	}
	return states, nil
}

// GetChecks returns the combined check status for the given git ref (SHA or branch).
func (c *Client) GetChecks(ctx context.Context, owner, repo, ref string) (*CheckStatus, error) {
	result, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, &gh.ListCheckRunsOptions{
		ListOptions: gh.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing check runs: %w", err)
	}

	status := &CheckStatus{
		Total: result.GetTotal(),
	}

	for _, run := range result.CheckRuns {
		conclusion := run.GetConclusion()
		switch {
		case run.GetStatus() != "completed":
			status.Pending++
		case conclusion == "success" || conclusion == "skipped" || conclusion == "neutral":
			status.Passed++
		default:
			status.Failed++
		}

		cr := CheckRun{
			Name:       run.GetName(),
			Status:     run.GetStatus(),
			Conclusion: conclusion,
		}
		if run.StartedAt != nil {
			cr.StartedAt = run.StartedAt.Time
			if run.CompletedAt != nil {
				cr.Duration = run.CompletedAt.Sub(run.StartedAt.Time)
			} else {
				cr.Duration = time.Since(run.StartedAt.Time)
			}
		}
		status.Runs = append(status.Runs, cr)
	}

	switch {
	case status.Failed > 0:
		status.State = "failure"
	case status.Pending > 0:
		status.State = "pending"
	default:
		status.State = "success"
	}

	return status, nil
}

// GetReviews returns the aggregated review status for a pull request.
// It deduplicates by user, keeping only the latest review per reviewer.
func (c *Client) GetReviews(ctx context.Context, owner, repo string, number int) (*ReviewStatus, error) {
	var allReviews []*gh.PullRequestReview
	opts := &gh.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("listing reviews: %w", err)
		}
		allReviews = append(allReviews, reviews...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Deduplicate: latest review per user wins.
	latestByUser := make(map[int64]*gh.PullRequestReview)
	for _, r := range allReviews {
		uid := r.GetUser().GetID()
		// COMMENTED state means review was started but not submitted — skip.
		if r.GetState() == "COMMENTED" {
			continue
		}
		if existing, ok := latestByUser[uid]; !ok || r.GetSubmittedAt().After(existing.GetSubmittedAt().Time) {
			latestByUser[uid] = r
		}
	}

	status := &ReviewStatus{}
	for _, r := range latestByUser {
		switch r.GetState() {
		case "APPROVED":
			status.Approved++
		case "CHANGES_REQUESTED":
			status.ChangesRequested++
		default:
			status.Pending++
		}
	}

	switch {
	case status.ChangesRequested > 0:
		status.State = "changes_requested"
	case status.Approved > 0 && status.Pending == 0:
		status.State = "approved"
	default:
		status.State = "pending"
	}

	return status, nil
}

// prToState converts a GitHub API PullRequest to our PRState type.
func prToState(pr *gh.PullRequest) *PRState {
	state := pr.GetState()
	if pr.GetMerged() {
		state = "merged"
	}

	mergeable := false
	if pr.Mergeable != nil {
		mergeable = *pr.Mergeable
	}

	return &PRState{
		Number:     pr.GetNumber(),
		Title:      pr.GetTitle(),
		URL:        pr.GetHTMLURL(),
		State:      state,
		Mergeable:  mergeable,
		Draft:      pr.GetDraft(),
		HeadBranch: pr.GetHead().GetRef(),
		BaseBranch: pr.GetBase().GetRef(),
	}
}
