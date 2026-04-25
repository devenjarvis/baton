package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// BranchNamer asynchronously summarizes a user prompt into a branch-slug
// suitable for concatenation with the configured branch prefix. The instruction
// passed in is the fully rendered template (the user's BranchNamePrompt with
// the {prompt} token already substituted) so the namer can pipe it verbatim to
// the underlying model. The returned slug has already been normalized through
// slugify() so callers can use it without additional sanitization.
type BranchNamer func(ctx context.Context, instruction string) (string, error)

const claudeHaikuModel = "claude-haiku-4-5"

// DefaultBranchNamer returns a BranchNamer that shells out to
// `claude -p --model claude-haiku-4-5` to summarize the user's first prompt.
// The instruction is piped in on stdin so the argv stays bounded regardless
// of prompt or template length.
//
// This namer always uses "claude" on PATH, independent of cfg.AgentProgram —
// users who configure a non-claude agent will simply get no rename when
// claude is absent (the random branch persists, retried on next prompt).
func DefaultBranchNamer() BranchNamer {
	return func(ctx context.Context, instruction string) (string, error) {
		claudePath, err := exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("claude not found on PATH: %w", err)
		}
		return runClaudeHaiku(ctx, claudePath, instruction)
	}
}

func runClaudeHaiku(ctx context.Context, claudePath, instruction string) (string, error) {
	cmd := exec.CommandContext(ctx, claudePath, "-p", "--model", claudeHaikuModel)
	cmd.Stdin = strings.NewReader(instruction)
	// Bound how long Wait() blocks on pipe drain after the context kills the
	// process — otherwise a descendant sleep can hold the stdout pipe open
	// and keep Wait blocked long past cancellation.
	cmd.WaitDelay = 500 * time.Millisecond

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude haiku: %w (stderr=%q)", err, strings.TrimSpace(stderr.String()))
	}

	slug := slugify(strings.TrimSpace(stdout.String()))
	if slug == "" {
		return "", fmt.Errorf("claude haiku: empty slug after slugify")
	}
	return slug, nil
}
