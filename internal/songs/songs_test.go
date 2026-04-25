package songs

import (
	"regexp"
	"testing"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func TestSlug_MatchesValidNameRegex(t *testing.T) {
	for _, tr := range catalog {
		slug := tr.Slug()
		if !validNameRe.MatchString(slug) {
			t.Errorf("Track %q.Slug() = %q which does not match validName regex", tr.Name, slug)
		}
	}
}

func TestSlug_NonEmpty(t *testing.T) {
	for _, tr := range catalog {
		if tr.Slug() == "" {
			t.Errorf("Track %q has empty Slug()", tr.Name)
		}
	}
}

func TestCatalog_AllFieldsNonEmpty(t *testing.T) {
	if len(catalog) == 0 {
		t.Fatal("catalog is empty")
	}
	for i, tr := range catalog {
		if tr.Name == "" {
			t.Errorf("catalog[%d].Name is empty", i)
		}
		if tr.Artist == "" {
			t.Errorf("catalog[%d].Artist is empty (track %q)", i, tr.Name)
		}
		if tr.ISRC == "" {
			t.Errorf("catalog[%d].ISRC is empty (track %q)", i, tr.Name)
		}
	}
}

func TestPick_NonEmpty(t *testing.T) {
	tr := Pick(nil)
	if tr.Name == "" {
		t.Fatal("Pick(nil) returned a track with empty Name")
	}
}

func TestPick_AvoidsCollision(t *testing.T) {
	// Run up to len(catalog) rounds — once we've picked every track the
	// catalog naturally exhausts, so we cap there.
	rounds := 200
	if rounds > len(catalog) {
		rounds = len(catalog)
	}
	existing := make([]string, 0, rounds)
	for i := 0; i < rounds; i++ {
		tr := Pick(existing)
		slug := tr.Slug()
		for _, e := range existing {
			if e == slug {
				t.Errorf("Pick returned slug %q which already exists in the existing list", slug)
			}
		}
		existing = append(existing, slug)
	}
}

func TestPick_WithExistingAvoidsDuplicate(t *testing.T) {
	first := Pick(nil)
	existing := []string{first.Slug()}

	for i := 0; i < 50; i++ {
		tr := Pick(existing)
		if tr.Slug() != first.Slug() {
			return // success
		}
	}
	t.Errorf("Pick kept returning %q even with it in the existing list", first.Slug())
}
