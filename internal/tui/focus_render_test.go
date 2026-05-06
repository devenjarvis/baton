package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/muesli/termenv"
)

// TestRenderFocusActiveCursor verifies that the selected session card in focus
// mode is visually distinct: the leading stripe glyph is rendered in
// ColorSecondary (cyan) for the selected session and a different color for
// unselected sessions. Selection is no longer signalled by a "> " chevron.
func TestRenderFocusActiveCursor(t *testing.T) {
	// Force TrueColor so the rendered ANSI escapes carry the foreground color
	// we want to assert against. Without this, lipgloss strips colors when
	// stdout is not a TTY and selection becomes indistinguishable in the
	// rendered string.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

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

	selectedStripe := lipgloss.NewStyle().Foreground(ColorSecondary).Render("▎")
	if !strings.Contains(selectedStripe, "▎") {
		t.Fatalf("expected styled stripe to contain glyph; got %q", selectedStripe)
	}

	var selectedLine, unselectedLine string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "active-b") && selectedLine == "":
			selectedLine = line
		case strings.Contains(line, "active-a") && unselectedLine == "":
			unselectedLine = line
		}
	}
	if selectedLine == "" || unselectedLine == "" {
		t.Fatalf("could not find both session header lines in output:\n%s", out)
	}

	if !strings.Contains(selectedLine, selectedStripe) {
		t.Fatalf("selected card missing cyan stripe %q\nselected line: %q\nfull:\n%s",
			selectedStripe, selectedLine, out)
	}
	// Unselected cards must still carry the stripe glyph (just in a different
	// color), so confirm the glyph is present before asserting the cyan color
	// is *not*.
	if !strings.Contains(unselectedLine, "▎") {
		t.Fatalf("unselected card missing stripe glyph entirely\nunselected line: %q\nfull:\n%s",
			unselectedLine, out)
	}
	if strings.Contains(unselectedLine, selectedStripe) {
		t.Fatalf("unselected card unexpectedly carries selection stripe color\nunselected line: %q\nfull:\n%s",
			unselectedLine, out)
	}
}
