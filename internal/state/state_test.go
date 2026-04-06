package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	saved := &BatonState{
		Version: 1,
		SavedAt: time.Now().Truncate(time.Second),
		Sessions: []SessionState{
			{
				ID:           "sess-1",
				Name:         "warm-xerus",
				DisplayName:  "Warm Xerus",
				WorktreePath: "/tmp/worktrees/warm-xerus",
				Branch:       "baton/warm-xerus",
				BaseBranch:   "main",
				Agents: []AgentState{
					{
						ID:              "agent-1",
						Name:            "agent-alpha",
						DisplayName:     "Alpha",
						Task:            "implement feature X",
						ClaudeSessionID: "claude-abc123",
					},
					{
						ID:   "agent-2",
						Name: "agent-beta",
					},
				},
			},
			{
				ID:           "sess-2",
				Name:         "dark-drake",
				WorktreePath: "/tmp/worktrees/dark-drake",
				Branch:       "baton/dark-drake",
				BaseBranch:   "main",
				Agents:       []AgentState{},
			},
		},
	}

	if err := Save(dir, saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}

	if loaded.Version != saved.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, saved.Version)
	}
	if !loaded.SavedAt.Equal(saved.SavedAt) {
		t.Errorf("SavedAt: got %v, want %v", loaded.SavedAt, saved.SavedAt)
	}
	if len(loaded.Sessions) != len(saved.Sessions) {
		t.Fatalf("Sessions count: got %d, want %d", len(loaded.Sessions), len(saved.Sessions))
	}

	s := loaded.Sessions[0]
	if s.ID != "sess-1" {
		t.Errorf("Session ID: got %q, want %q", s.ID, "sess-1")
	}
	if s.Name != "warm-xerus" {
		t.Errorf("Session Name: got %q, want %q", s.Name, "warm-xerus")
	}
	if s.DisplayName != "Warm Xerus" {
		t.Errorf("Session DisplayName: got %q, want %q", s.DisplayName, "Warm Xerus")
	}
	if s.WorktreePath != "/tmp/worktrees/warm-xerus" {
		t.Errorf("WorktreePath: got %q", s.WorktreePath)
	}
	if s.Branch != "baton/warm-xerus" {
		t.Errorf("Branch: got %q", s.Branch)
	}
	if s.BaseBranch != "main" {
		t.Errorf("BaseBranch: got %q", s.BaseBranch)
	}
	if len(s.Agents) != 2 {
		t.Fatalf("Agents count: got %d, want 2", len(s.Agents))
	}

	a := s.Agents[0]
	if a.ID != "agent-1" {
		t.Errorf("Agent ID: got %q", a.ID)
	}
	if a.Name != "agent-alpha" {
		t.Errorf("Agent Name: got %q", a.Name)
	}
	if a.DisplayName != "Alpha" {
		t.Errorf("Agent DisplayName: got %q", a.DisplayName)
	}
	if a.Task != "implement feature X" {
		t.Errorf("Agent Task: got %q", a.Task)
	}
	if a.ClaudeSessionID != "claude-abc123" {
		t.Errorf("Agent ClaudeSessionID: got %q", a.ClaudeSessionID)
	}

	// Check omitempty fields are absent for agent-2
	a2 := s.Agents[1]
	if a2.DisplayName != "" {
		t.Errorf("Agent2 DisplayName should be empty, got %q", a2.DisplayName)
	}
	if a2.Task != "" {
		t.Errorf("Agent2 Task should be empty, got %q", a2.Task)
	}

	// Check second session has empty display name (omitempty)
	s2 := loaded.Sessions[1]
	if s2.DisplayName != "" {
		t.Errorf("Session2 DisplayName should be empty, got %q", s2.DisplayName)
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load on missing file should not error, got: %v", err)
	}
	if loaded != nil {
		t.Fatalf("Load on missing file should return nil, got: %+v", loaded)
	}
}

func TestRemoveIdempotent(t *testing.T) {
	dir := t.TempDir()

	saved := &BatonState{
		Version:  1,
		SavedAt:  time.Now().Truncate(time.Second),
		Sessions: []SessionState{},
	}
	if err := Save(dir, saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// First remove should succeed
	if err := Remove(dir); err != nil {
		t.Fatalf("First Remove: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(statePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("File should not exist after Remove")
	}

	// Second remove should also succeed (idempotent)
	if err := Remove(dir); err != nil {
		t.Fatalf("Second Remove: %v", err)
	}
}

func TestSaveCreatesBatonDir(t *testing.T) {
	dir := t.TempDir()
	// Use a subdirectory that doesn't exist yet
	repoPath := filepath.Join(dir, "newrepo")

	saved := &BatonState{
		Version:  1,
		SavedAt:  time.Now().Truncate(time.Second),
		Sessions: []SessionState{},
	}

	if err := Save(repoPath, saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify the .baton directory was created
	info, err := os.Stat(filepath.Join(repoPath, ".baton"))
	if err != nil {
		t.Fatalf("Expected .baton dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".baton should be a directory")
	}

	// Verify the file exists and is loadable
	loaded, err := Load(repoPath)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil after Save")
	}
	if loaded.Version != 1 {
		t.Errorf("Version: got %d, want 1", loaded.Version)
	}
}
