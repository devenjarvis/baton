//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// repoOnlyHint is shown in the dashboard preview when only a repo is selected
// and it has no sessions yet. We use it as a sentinel for "no sessions exist
// in this repo" — once a session is created and selected, the preview switches
// to showing the worktree path / task / terminal output instead.
const repoOnlyHint = "Press enter to configure this repo"

// TestSessionCreation verifies that pressing "n" on the dashboard creates a
// new session, auto-focuses the terminal showing a bash prompt, and that
// pressing Escape returns to the list where a session row is now visible.
func TestSessionCreation(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	// Sanity: no agent rows yet, and the repo-only hint is visible.
	if got := countAgentRows(s.Screenshot()); got != 0 {
		t.Fatalf("expected 0 agent rows before create, got %d\n%s", got, s.Screenshot())
	}
	s.AssertScreenContains(repoOnlyHint)

	// Press "n" to create a new session — auto-focuses the terminal.
	s.Press("n")
	s.WaitForText(`\$`, 10000)

	// Return to list focus — wait for the dashboard's "navigate" hint
	// to confirm the focus actually switched back.
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	// After creating a session, the sidebar should show at least one agent row.
	if got := countAgentRows(s.Screenshot()); got < 1 {
		t.Errorf("expected at least 1 agent row after create, got %d\n%s",
			got, s.Screenshot())
	}
}

// TestAgentAddition verifies that pressing "c" adds a second agent row to an
// existing session.
func TestAgentAddition(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	// Create the first session/agent.
	s.Press("n")
	s.WaitForText(`\$`, 10000)
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	beforeAgentCount := countAgentRows(s.Screenshot())
	if beforeAgentCount < 1 {
		t.Fatalf("expected at least 1 agent row before adding, got %d\n%s",
			beforeAgentCount, s.Screenshot())
	}

	// Add a second agent to the session. After "c", baton creates an agent
	// asynchronously and auto-focuses its terminal. We can't rely on
	// WaitForText(`\$`) to signal a new prompt — the FIRST agent's prompt may
	// still be visible in the preview pane, so the wait can match instantly
	// without the new agent existing yet. Instead, poll the agent-row count
	// after escaping back to the list.
	s.Press("c")
	s.WaitForText(`\$`, 10000)
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	if !waitForAgentCount(s, beforeAgentCount+1, 10000) {
		t.Errorf("expected agent row count to reach %d after adding, last seen %d\n%s",
			beforeAgentCount+1, countAgentRows(s.Screenshot()), s.Screenshot())
	}
}

// waitForAgentCount polls Screenshot for up to timeoutMs and returns true once
// countAgentRows reaches at least min. Used to handle async agent creation
// where a render-driven wait (WaitForText) isn't sufficient.
func waitForAgentCount(s *Session, min, timeoutMs int) bool {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		if countAgentRows(s.Screenshot()) >= min {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return countAgentRows(s.Screenshot()) >= min
}

// TestAgentKill verifies that pressing "x" kills the selected agent.
// With one agent in the session, killing it removes the whole session,
// so the agent count should drop back to 0 and the repo-only hint reappears.
func TestAgentKill(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	s.Press("n")
	s.WaitForText(`\$`, 10000)
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	// Sanity: at least one agent row exists.
	if countAgentRows(s.Screenshot()) == 0 {
		t.Fatalf("expected agent row before kill, got none\n%s", s.Screenshot())
	}

	// Navigate down to the agent row, then kill it.
	s.Press("j")
	s.WaitStable(500)
	s.Press("x")

	// Wait for the repo-only hint to reappear (sole-agent kill removes the
	// session; selection falls back to the repo header which shows this hint).
	s.WaitForText(repoOnlyHint, 10000)
	if got := countAgentRows(s.Screenshot()); got != 0 {
		t.Errorf("expected 0 agent rows after kill, got %d\n%s", got, s.Screenshot())
	}
}

// TestSessionKill verifies that pressing "X" kills the entire session and
// removes its rows from the sidebar.
func TestSessionKill(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	s.Press("n")
	s.WaitForText(`\$`, 10000)
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	// Sanity: agent rows exist before the kill.
	if got := countAgentRows(s.Screenshot()); got == 0 {
		t.Fatalf("expected agent row(s) before session kill, got 0\n%s", s.Screenshot())
	}

	s.Press("X")

	// After killing the only session, the repo-only hint reappears.
	s.WaitForText(repoOnlyHint, 10000)
	if got := countAgentRows(s.Screenshot()); got != 0 {
		t.Errorf("expected 0 agent rows after session kill, got %d\n%s", got, s.Screenshot())
	}
}

// countAgentRows counts agent rows in the dashboard sidebar.
// Agent rows are indented under sessions and start with a status symbol from
// agent.Status.Symbol(): ▷ ▶ ⏺ ⏸ ⏭ ⏹ (or "$" for shell agents). Session header
// rows contain box-drawing dashes "──", so we skip those.
func countAgentRows(screen string) int {
	statusSymbols := []string{"▷ ", "▶ ", "⏺ ", "⏸ ", "⏭ ", "⏹ ", "$ "}
	count := 0
	for _, line := range strings.Split(screen, "\n") {
		// Strip leading whitespace and the optional selection arrow.
		trimmed := strings.TrimLeft(line, " ")
		trimmed = strings.TrimPrefix(trimmed, "▸ ")
		// Session-header rows contain box-drawing dashes; skip them.
		if strings.Contains(line, "──") {
			continue
		}
		for _, sym := range statusSymbols {
			if strings.HasPrefix(trimmed, sym) {
				count++
				break
			}
		}
	}
	return count
}
