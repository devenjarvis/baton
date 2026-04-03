package agent

import "math/rand/v2"

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
