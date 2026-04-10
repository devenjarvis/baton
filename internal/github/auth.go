package github

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GetToken returns a GitHub API token. It first tries the `gh` CLI
// (`gh auth token`), then falls back to the GITHUB_TOKEN environment variable.
// Returns an error only if both sources fail.
func GetToken() (string, error) {
	// Try gh CLI first.
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err == nil {
		token := strings.TrimSpace(string(out))
		if token != "" {
			return token, nil
		}
	}

	// Fall back to environment variable.
	token := os.Getenv("GITHUB_TOKEN")
	if token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub token found: install gh CLI and run 'gh auth login', or set GITHUB_TOKEN")
}
