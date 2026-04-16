package agent

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"fix: auth bug in login", "fix-auth-bug-in-login"},
		{"  spaces  and  stuff  ", "spaces-and-stuff"},
		{"UPPERCASE", "uppercase"},
		{"a-b-c", "a-b-c"},
		{"123-start", "123-start"},
		{"", ""},
		{"!@#$%", ""},
		{"a very long string that exceeds the forty character limit for slugs yes", "a-very-long-string-that-exceeds-the-fort"},
	}

	for _, tc := range tests {
		got := slugify(tc.input)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSessionGetDisplayName_Fallback(t *testing.T) {
	s := &Session{
		Name:   "eager-panda",
		agents: make(map[string]*Agent),
	}

	// Should fall back to Name.
	if got := s.GetDisplayName(); got != "eager-panda" {
		t.Errorf("GetDisplayName() = %q, want %q", got, "eager-panda")
	}

	if s.HasDisplayName() {
		t.Error("HasDisplayName() should be false before SetDisplayName")
	}

	s.SetDisplayName("fix-auth-bug")
	if got := s.GetDisplayName(); got != "fix-auth-bug" {
		t.Errorf("GetDisplayName() = %q, want %q", got, "fix-auth-bug")
	}

	if !s.HasDisplayName() {
		t.Error("HasDisplayName() should be true after SetDisplayName")
	}
}
