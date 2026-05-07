package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/hook"
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

// TestRenderFocusStripeMintForAllIdleSession verifies the render path
// surfaces ColorReady for an all-idle session (every non-shell agent is
// StatusIdle, no asking-question, no DoneAt). A second session in the same
// render pass with at least one StatusActive agent must continue to render
// the muted stripe — the new accent must not bleed across sessions.
func TestRenderFocusStripeMintForAllIdleSession(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	sessIdle := &agent.Session{Name: "all-idle"}
	sessIdle.SetLifecyclePhase(agent.LifecycleInProgress)
	idleA := &agent.Agent{Name: "idle-a"}
	idleA.ForceStatusForTest(agent.StatusIdle)
	idleB := &agent.Agent{Name: "idle-b"}
	idleB.ForceStatusForTest(agent.StatusIdle)

	sessActive := &agent.Session{Name: "has-active"}
	sessActive.SetLifecyclePhase(agent.LifecycleInProgress)
	activeAg := &agent.Agent{Name: "active"}
	activeAg.OnHookEvent(hook.Event{Kind: hook.KindSessionStart})

	d := newDashboardModel()
	d.width = 120
	d.height = 39
	d.focusModeActive = true
	d.focusCursorSection = focusSectionActive
	// Keep the cursor away from either of these sessions so neither gets the
	// selection-highlight cyan stripe — that would mask the underlying
	// stripe color we want to assert against.
	d.focusActiveIdx = 99
	d.items = []listItem{
		{kind: listItemRepo, repoPath: "/r", repoName: "repo"},
		{kind: listItemSession, repoPath: "/r", session: sessIdle},
		{kind: listItemAgent, repoPath: "/r", session: sessIdle, agent: idleA},
		{kind: listItemAgent, repoPath: "/r", session: sessIdle, agent: idleB},
		{kind: listItemSession, repoPath: "/r", session: sessActive},
		{kind: listItemAgent, repoPath: "/r", session: sessActive, agent: activeAg},
	}

	out := d.renderFullscreenFocus(120, 39)

	mintStripe := lipgloss.NewStyle().Foreground(ColorReady).Render("▎")
	mutedStripe := lipgloss.NewStyle().Foreground(ColorMuted).Render("▎")

	var idleLine, activeLine string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "all-idle") && idleLine == "":
			idleLine = line
		case strings.Contains(line, "has-active") && activeLine == "":
			activeLine = line
		}
	}
	if idleLine == "" || activeLine == "" {
		t.Fatalf("could not find both session header lines in output:\n%s", out)
	}

	if !strings.Contains(idleLine, mintStripe) {
		t.Fatalf("all-idle card missing mint stripe %q\nline: %q\nfull:\n%s",
			mintStripe, idleLine, out)
	}
	if !strings.Contains(activeLine, mutedStripe) {
		t.Fatalf("active card missing muted stripe %q\nline: %q\nfull:\n%s",
			mutedStripe, activeLine, out)
	}
	if strings.Contains(activeLine, mintStripe) {
		t.Fatalf("active card unexpectedly carries mint stripe (mint must not bleed across sessions)\nline: %q\nfull:\n%s",
			activeLine, out)
	}
}
