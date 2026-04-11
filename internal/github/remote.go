package github

import (
	"fmt"
	"strings"
)

// ParseRemoteURL extracts the owner and repo name from a GitHub remote URL.
// Supported formats:
//   - git@github.com:owner/repo.git
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo
//   - ssh://git@github.com/owner/repo.git
func ParseRemoteURL(rawURL string) (owner, repo string, err error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("empty remote URL")
	}

	var path string

	switch {
	case strings.HasPrefix(rawURL, "git@github.com:"):
		// git@github.com:owner/repo.git
		path = strings.TrimPrefix(rawURL, "git@github.com:")

	case strings.HasPrefix(rawURL, "https://github.com/"):
		// https://github.com/owner/repo.git or https://github.com/owner/repo
		path = strings.TrimPrefix(rawURL, "https://github.com/")

	case strings.HasPrefix(rawURL, "ssh://git@github.com/"):
		// ssh://git@github.com/owner/repo.git
		path = strings.TrimPrefix(rawURL, "ssh://git@github.com/")

	default:
		return "", "", fmt.Errorf("unsupported or non-GitHub remote URL: %s", rawURL)
	}

	// Strip .git suffix and trailing slash.
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")

	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse owner/repo from remote URL: %s", rawURL)
	}

	return parts[0], parts[1], nil
}
