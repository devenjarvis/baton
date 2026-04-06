package agent

import (
	"math/rand/v2"
	"regexp"
	"strings"
)

var adjectives = []string{
	"brave", "calm", "dark", "eager", "fair",
	"grand", "happy", "icy", "jolly", "keen",
	"lively", "misty", "noble", "odd", "proud",
	"quiet", "rapid", "sharp", "tidy", "urban",
	"vivid", "warm", "witty", "young", "zesty",
	"bold", "crisp", "dusty", "frosty", "gentle",
}

var nouns = []string{
	"falcon", "badger", "crane", "dingo", "egret",
	"ferret", "gecko", "heron", "ibis", "jackal",
	"kestrel", "lemur", "marten", "newt", "osprey",
	"panda", "quail", "raven", "stoat", "toucan",
	"urial", "viper", "walrus", "xerus", "yak",
	"zebra", "bison", "cobra", "drake", "elk",
}

// RandomName returns a random adjective-noun name (e.g. "brave-falcon") that
// does not appear in the existing slice. It retries up to 100 times to avoid
// collisions; if all attempts collide it returns the last attempt anyway.
func RandomName(existing []string) string {
	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	var name string
	for i := 0; i < 100; i++ {
		adj := adjectives[rand.IntN(len(adjectives))]
		noun := nouns[rand.IntN(len(nouns))]
		name = adj + "-" + noun
		if _, found := existingSet[name]; !found {
			return name
		}
	}
	return name
}

var nonAlnumRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// slugify lowercases s, collapses runs of non-alphanumeric characters to "-",
// trims leading/trailing "-", and truncates to 40 characters.
// Returns "" if the result is empty or doesn't start with [a-zA-Z0-9].
func slugify(s string) string {
	slug := nonAlnumRe.ReplaceAllString(strings.ToLower(s), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "-")
	}
	if slug == "" {
		return ""
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		if slug[0] < '0' || slug[0] > '9' {
			return ""
		}
	}
	return slug
}
