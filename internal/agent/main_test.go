package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain redirects $HOME to a per-process temp dir so tests that exercise
// session creation never touch the real ~/.baton/setlist.jsonl (or any other
// global baton state). Without this, every CreateSession in the test suite
// appends a row to the user's real setlist file.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "baton-agent-tests-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Setenv("HOME", tmp); err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config")); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
