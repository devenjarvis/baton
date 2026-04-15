//go:build e2e

package e2e

import "testing"

func TestSettingsOverlay(t *testing.T) {
	s := newSession(t)
	s.Start()

	// Wait for dashboard to appear.
	s.WaitForText("AGENTS", 10000)

	// Press "s" to open global settings overlay.
	s.Press("s")
	s.WaitStable(1000)

	// Verify settings view content appears — look for setting field labels.
	s.AssertScreenContains("Audio Enabled")
	s.AssertScreenContains("Bypass Permissions")

	// Press Escape to return to dashboard.
	s.Press("Escape")
	s.WaitStable(1000)

	// Verify dashboard is restored.
	s.AssertScreenContains("AGENTS")
}

func TestDiffView(t *testing.T) {
	s := newSession(t)
	s.Start()

	// Wait for dashboard to appear.
	s.WaitForText("AGENTS", 10000)

	// Create a new session — "n" key. This auto-focuses the terminal.
	s.Press("n")

	// Wait for bash prompt inside the worktree.
	s.WaitForText("\\$", 10000)

	// Create a file, stage it, and commit so it shows up in branch diff.
	s.Type("echo test > file.txt && git add file.txt && git commit -m 'add file'\n")
	s.WaitStable(2000)

	// Press Escape to return to list focus.
	s.Press("Escape")
	s.WaitStable(1000)

	// Press "d" to open diff view.
	s.Press("d")
	s.WaitStable(2000)

	// Verify the diff view shows the new file. We assert on the diff status
	// bar hints (which are only rendered in the diff view, not the dashboard)
	// AND on the file name, so the assertion can't pass if the diff view
	// failed to open.
	screen := s.Screenshot()
	t.Logf("Diff view screen:\n%s", screen)
	s.AssertScreenContains("file.txt")
	s.AssertScreenContains("scroll diff") // unique to diffHints

	// Exit diff view.
	s.Press("Escape")
	s.WaitStable(1000)

	// Verify dashboard is restored.
	s.AssertScreenContains("AGENTS")
}

func TestFileBrowser(t *testing.T) {
	s := newSession(t)
	s.Start()

	// Wait for dashboard to appear.
	s.WaitForText("AGENTS", 10000)

	// Press "a" to open file browser overlay.
	s.Press("a")
	s.WaitStable(1000)

	// Verify the file browser overlay is showing its characteristic headers.
	s.AssertScreenContains("DIRECTORIES")
	s.AssertScreenContains("DETAILS")

	// Press Escape to close.
	s.Press("Escape")
	s.WaitStable(1000)

	// Verify dashboard is restored.
	s.AssertScreenContains("AGENTS")
}
