package github

import (
	"context"
	"fmt"
	"strings"

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

// CreatePR creates a new pull request and returns its state.
func (c *Client) CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (*PRState, error) {
	pr, _, err := c.gh.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
		Title: gh.Ptr(title),
		Head:  gh.Ptr(head),
		Base:  gh.Ptr(base),
		Body:  gh.Ptr(body),
	})
	if err != nil {
		return nil, fmt.Errorf("creating PR: %w", err)
	}

	return prToState(pr), nil
}

// MergePR merges the pull request using the repository's default merge method.
func (c *Client) MergePR(ctx context.Context, owner, repo string, number int) error {
	_, _, err := c.gh.PullRequests.Merge(ctx, owner, repo, number, "", nil)
	if err != nil {
		return fmt.Errorf("merging PR #%d: %w", number, err)
	}
	return nil
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
			status.FailedChecks = append(status.FailedChecks, FailedCheck{
				ID:         run.GetID(),
				Name:       run.GetName(),
				Conclusion: conclusion,
				DetailsURL: run.GetDetailsURL(),
			})
		}
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

// GetFailedCheckLogs returns the annotations/log output for a specific failed check run.
func (c *Client) GetFailedCheckLogs(ctx context.Context, owner, repo string, checkRunID int64) (string, error) {
	annotations, _, err := c.gh.Checks.ListCheckRunAnnotations(ctx, owner, repo, checkRunID, &gh.ListOptions{
		PerPage: 100,
	})
	if err != nil {
		return "", fmt.Errorf("listing annotations for check run %d: %w", checkRunID, err)
	}

	if len(annotations) == 0 {
		return "(no annotations found for this check run)", nil
	}

	var b strings.Builder
	for _, a := range annotations {
		fmt.Fprintf(&b, "%s:%d — %s: %s\n",
			a.GetPath(),
			a.GetStartLine(),
			a.GetAnnotationLevel(),
			a.GetMessage(),
		)
	}
	return b.String(), nil
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
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		URL:       pr.GetHTMLURL(),
		State:     state,
		Mergeable: mergeable,
		Draft:     pr.GetDraft(),
	}
}
