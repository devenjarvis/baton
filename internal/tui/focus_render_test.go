package tui

import (
	"strings"
	"testing"

	"github.com/devenjarvis/baton/internal/agent"
)

// TestRenderFocusActiveCursor verifies that when the focus cursor is on an
// active session, that row is rendered with the "> " selection prefix.
func TestRenderFocusActiveCursor(t *testing.T) {
	sessA := &agent.Session{Name: "active-a"}
	sessA.SetLifecyclePhase(agent.LifecycleInProgress)
	sessB := &agent.Session{Name: "active-b"}
	sessB.SetLifecyclePhase(agent.LifecycleInProgress)

	d := newDashboardModel()
	d.width = 120
	d.height = 39
	d.focusModeActive = true
	d.focusCursorSection = focusSectionActive
	d.focusActiveIdx = 1
	d.items = []listItem{
		{kind: listItemRepo, repoPath: "/r", repoName: "repo"},
		{kind: listItemSession, repoPath: "/r", session: sessA},
		{kind: listItemSession, repoPath: "/r", session: sessB},
	}

	out := d.renderFullscreenFocus(120, 39)
	lines := strings.Split(out, "\n")
	var sawSelected bool
	for _, line := range lines {
		if strings.Contains(line, "active-b") && strings.Contains(line, "> ") {
			sawSelected = true
		}
		if strings.Contains(line, "active-a") && strings.Contains(line, "> ") {
			t.Fatalf("active-a should not be selected (focusActiveIdx=1):\n%s", line)
		}
	}
	if !sawSelected {
		t.Fatalf("expected selection marker on active-b, got:\n%s", out)
	}
}
