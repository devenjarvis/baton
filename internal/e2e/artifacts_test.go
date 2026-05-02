//go:build e2e

package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// TestArtifactsOnPlanReview drives a bash-scripted agent through a
// plan-approval-shaped interaction: enter alt-screen, draw a frame ending in a
// distinctive marker, sleep long enough for baton to render it to the outer
// terminal, then redraw a shorter frame. The test asserts on baton's emitted
// View (written to BATON_E2E_DEBUG_DUMP by dashboard.View), not on the
// downstream terminal emulator — because lipgloss already width-pads content
// inside the preview box, the Render()→RenderPadded() distinction shows up
// deterministically in baton's output but can be masked by tu/Bubble Tea diff
// rendering at the terminal layer. Regression target: after alt-screen entry
// and a clean redraw, baton's preview View must not contain GHOST_ARTIFACT_FOO.
func TestArtifactsOnPlanReview(t *testing.T) {
	s := newSession(t)
	dumpPath := t.TempDir() + "/baton_view_dump.txt"
	s.extraEnv = append(s.extraEnv, "BATON_E2E_DEBUG_DUMP="+dumpPath)
	s.Start()
	s.WaitForText("AGENTS", 10000)

	// Create a session; "n" auto-focuses the terminal and launches bash.
	s.Press("n")
	s.WaitForText(`\$`, 10000)

	// Frame 1: alt-screen enter + clear + home, 4 rows, then a long
	// GHOST_ARTIFACT marker on row 5. The marker is longer than frame 2's
	// prompt so trailing cells must be cleared for the artifact to vanish.
	// The first sleep is tuned above the 100ms baton tick so baton definitely
	// ticks frame 1 into the outer terminal before frame 2 overwrites the VT
	// cells. Frame 2 uses \e[2J\e[H (clear + home) so every VT cell beyond
	// '> ' is explicitly EmptyCell — this is exactly the condition that
	// exposes renderLine's trailing-whitespace trim.
	script := `printf '\033[?1049h\033[2J\033[H' && ` +
		`for i in 1 2 3 4; do echo "LINE $i"; done && ` +
		`printf 'GHOST_ARTIFACT_FOO'; ` +
		`sleep 0.6; ` +
		`printf '\033[2J\033[H' && ` +
		`for i in 1 2 3 4; do echo "LINE $i"; done && ` +
		`printf '> '; sleep 30`
	s.Type(script + "\n")

	// Wait past the in-script sleep plus a few baton ticks so the latest
	// dump reflects frame 2.
	time.Sleep(2 * time.Second)
	s.WaitStable(500)

	view, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("reading view dump: %v", err)
	}
	viewStr := string(view)

	if !strings.Contains(viewStr, "LINE 4") {
		t.Fatalf("expected frame 2 content (LINE 4) in baton view\nView:\n%s", viewStr)
	}
	if strings.Contains(viewStr, "GHOST_ARTIFACT_FOO") {
		t.Errorf("frame 1 ghost marker still in baton's view after frame 2 redraw\nView:\n%s", viewStr)
	}
}

// TestPreviewPanelModeSwitch verifies that dashboard.View() always covers the
// full terminal width and has the expected number of lines in both focusList and
// focusTerminal modes. Regression for two structural layout bugs:
//
//   - Width mismatch: in focusList mode the preview container used
//     Width(fixedTermWidth) instead of Width(fixedTermWidth+2), leaving 2 columns
//     unwritten per render (stale cells from prior focusTerminal renders).
//   - Scrollback padding: lines retrieved from scrollback history were not padded
//     to vpWidth before being joined, allowing short lines to leave stale cells.
//
// Note: the PR height overflow (focusTerminal Height with PR visible) requires a
// live GitHub PR to trigger and is not covered here.
func TestPreviewPanelModeSwitch(t *testing.T) {
	// tu session is launched at 120x40 (see Session.Start).
	const (
		termWidth     = 120
		termHeight    = 40
		contentHeight = termHeight - 2 // statusbar row + title row
	)

	s := newSession(t)
	dumpPath := t.TempDir() + "/mode_switch_dump.txt"
	s.extraEnv = append(s.extraEnv, "BATON_E2E_DEBUG_DUMP="+dumpPath)
	s.Start()
	s.WaitForText("AGENTS", 10000)

	// Create a session; "n" auto-focuses the terminal and launches bash.
	s.Press("n")
	s.WaitForText(`\$`, 10000)

	// Emit visible content so the preview renders something non-trivial.
	s.Type("echo LAYOUT_MARKER_LINE\n")
	s.WaitForText("LAYOUT_MARKER_LINE", 5000)
	s.WaitStable(500)

	// focusTerminal: baseline sanity.
	assertViewDimensions(t, "focusTerminal", dumpPath, termWidth, contentHeight)

	// Switch to focusList — the key regression mode.
	s.Press("Escape")
	s.WaitForText("navigate", 5000)
	s.WaitStable(300)

	// Each rendered line must cover the full terminal width. Without the fix,
	// Width(fixedTermWidth) produced 118-char lines instead of 120.
	assertViewDimensions(t, "focusList", dumpPath, termWidth, contentHeight)

	// Stress: multiple focus-mode round trips must not degrade the invariant.
	for range 3 {
		s.Press("Enter")
		s.WaitForText(`\$`, 3000)
		s.Press("Escape")
		s.WaitForText("navigate", 3000)
		s.WaitStable(300)
	}
	assertViewDimensions(t, "focusList after repeated switches", dumpPath, termWidth, contentHeight)
}

// assertViewDimensions reads the BATON_E2E_DEBUG_DUMP at dumpPath and asserts
// that the view has exactly wantHeight lines and that every line is exactly
// wantWidth display cells wide (ANSI stripped).
func assertViewDimensions(t *testing.T, mode, dumpPath string, wantWidth, wantHeight int) {
	t.Helper()
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("%s: reading dump: %v", mode, err)
	}
	view := strings.TrimRight(string(data), "\n")
	lines := strings.Split(view, "\n")

	if got := len(lines); got != wantHeight {
		t.Errorf("%s: view has %d lines, want %d", mode, got, wantHeight)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != wantWidth {
			t.Errorf("%s: line %d visible width = %d, want %d", mode, i, got, wantWidth)
		}
	}
}
