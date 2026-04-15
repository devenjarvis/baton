//go:build e2e

package e2e

import "testing"

// TestQuitNoAgents verifies that pressing "q" with no running agents exits
// baton immediately with exit code 0.
func TestQuitNoAgents(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	s.Press("q")
	s.WaitStable(2000)

	alive, exitCode := s.Status()
	if alive {
		t.Fatalf("expected process to have exited, but it is still alive")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

// TestQuitConfirmation verifies the detach (q) flow when agents are running:
// first "q" shows a confirmation message, second "q" detaches and exits.
func TestQuitConfirmation(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	// Create a new agent session (bash).
	s.Press("n")
	s.WaitForText("\\$", 10000)

	// Return to list focus so quit key is handled at the app level.
	s.Press("Escape")
	s.WaitStable(1000)

	// First "q" — should show confirmation, not exit.
	s.Press("q")
	s.WaitStable(1000)

	// The confirmation banner is distinctive (and only shown when confirmQuit
	// is set); the always-present status bar hints don't say "Agents are running".
	s.AssertScreenContains("Agents are running")

	// Second "q" — actually detach and exit.
	s.Press("q")
	s.WaitStable(3000)

	alive, exitCode := s.Status()
	if alive {
		t.Fatalf("expected process to have exited after detach, but it is still alive")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

// TestForceQuit verifies the force quit (Q) flow when agents are running:
// first "Q" shows a confirmation message, second "Q" force quits and exits.
func TestForceQuit(t *testing.T) {
	s := newSession(t)
	s.Start()

	s.WaitForText("AGENTS", 10000)

	// Create a new agent session (bash).
	s.Press("n")
	s.WaitForText("\\$", 10000)

	// Return to list focus so quit key is handled at the app level.
	s.Press("Escape")
	s.WaitStable(1000)

	// First "Q" — should show confirmation, not exit.
	s.Press("Q")
	s.WaitStable(1000)

	// The confirmation banner is distinctive (and only shown when confirmQuit
	// is set); the always-present status bar hints don't say "Agents are running".
	s.AssertScreenContains("Agents are running")

	// Second "Q" — actually force quit and exit.
	s.Press("Q")
	// Force quit cleans up worktrees and kills agents, which can take a few seconds.
	s.WaitStable(5000)

	alive, exitCode := s.Status()
	if alive {
		t.Fatalf("expected process to have exited after force quit, but it is still alive")
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}
