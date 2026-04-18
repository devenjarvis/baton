package config

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var prefixSlugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// ExpandBranchPrefix substitutes the recognized template variables in raw and
// returns the result. Supported tokens:
//
//   - {user}: git `user.name` (falling back to $USER), slugified. If both are
//     empty, the token is dropped entirely — the surrounding separators remain
//     as the user wrote them.
//   - {date}: today's date in YYYY-MM-DD form.
//
// Unknown {foo} tokens are left literal. No Go template syntax is used so
// existing prefixes containing literal braces are unaffected.
func ExpandBranchPrefix(raw string) string {
	out := raw
	out = strings.ReplaceAll(out, "{user}", resolveUserSlug())
	out = strings.ReplaceAll(out, "{date}", time.Now().Format("2006-01-02"))
	return out
}

func resolveUserSlug() string {
	if name := strings.TrimSpace(gitUserName()); name != "" {
		if slug := prefixSlug(name); slug != "" {
			return slug
		}
	}
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		if slug := prefixSlug(name); slug != "" {
			return slug
		}
	}
	return ""
}

func gitUserName() string {
	cmd := exec.Command("git", "config", "user.name")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// prefixSlug lowercases s and collapses runs of non-alphanumeric characters
// to "-", trimming leading/trailing "-". Mirrors agent.slugify without the
// length cap (prefixes are expected to be short).
func prefixSlug(s string) string {
	slug := prefixSlugRe.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(slug, "-")
}
