package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPollClaudeSessionName_NoFile(t *testing.T) {
	dir := t.TempDir()
	old := ClaudeSessionDir
	ClaudeSessionDir = dir
	defer func() { ClaudeSessionDir = old }()

	a := &Agent{claudePid: 99999}
	if got := a.PollClaudeSessionName(); got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestPollClaudeSessionName_NoNameField(t *testing.T) {
	dir := t.TempDir()
	old := ClaudeSessionDir
	ClaudeSessionDir = dir
	defer func() { ClaudeSessionDir = old }()

	// Write a JSON file without a "name" field.
	data, _ := json.Marshal(map[string]any{"id": "abc123"})
	if err := os.WriteFile(filepath.Join(dir, "12345.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{claudePid: 12345}
	if got := a.PollClaudeSessionName(); got != "" {
		t.Errorf("expected empty string for missing name field, got %q", got)
	}
}

func TestPollClaudeSessionName_WithName(t *testing.T) {
	dir := t.TempDir()
	old := ClaudeSessionDir
	ClaudeSessionDir = dir
	defer func() { ClaudeSessionDir = old }()

	data, _ := json.Marshal(map[string]any{"name": "fix-auth-bug", "id": "abc123"})
	if err := os.WriteFile(filepath.Join(dir, "42.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{claudePid: 42}
	got := a.PollClaudeSessionName()
	if got != "fix-auth-bug" {
		t.Errorf("expected %q, got %q", "fix-auth-bug", got)
	}
}

func TestPollClaudeSessionName_ZeroPid(t *testing.T) {
	a := &Agent{claudePid: 0}
	if got := a.PollClaudeSessionName(); got != "" {
		t.Errorf("expected empty string for zero PID, got %q", got)
	}
}

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
