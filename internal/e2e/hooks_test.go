//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeStubScript is a bash stub installed as `<dir>/claude`. Baton's
// supportsHooks check keys off filepath.Base(agent_program), so the file must
// be named `claude`. The stub ignores any args (baton passes
// `--settings <path>`), inherits BATON_HOOK_SOCKET / BATON_AGENT_ID from
// baton's env wiring, and drives the pipeline by invoking
// `$BATON_E2E_BATON hook <event>` at scripted intervals.
//
// Sequence (seconds since start):
//
//	0.3  session-start   → Active
//	1.5  notification    → Waiting
//	3.5  stop            → Idle
//	5.5  user-prompt-submit → Active (re-armed)
//	7.5  stop            → Idle
//	then sleep 3600 so baton keeps it alive until test teardown kills it.
const claudeStubScript = `#!/bin/bash
echo "claude-e2e-stub ready"
sleep 0.3
"$BATON_E2E_BATON" hook session-start <<< '{"session_id":"e2e-sess-1","cwd":"/tmp"}'
sleep 1.2
"$BATON_E2E_BATON" hook notification <<< '{"session_id":"e2e-sess-1","message":"Claude needs permission"}'
sleep 2
"$BATON_E2E_BATON" hook stop <<< '{"session_id":"e2e-sess-1"}'
sleep 2
"$BATON_E2E_BATON" hook user-prompt-submit <<< '{"session_id":"e2e-sess-1"}'
sleep 2
"$BATON_E2E_BATON" hook stop <<< '{"session_id":"e2e-sess-1"}'
sleep 3600
`

// TestHookPipeline drives baton through a scripted bash "claude" stub that
// emits each hook kind in turn, and asserts the dashboard bubble transitions
// Active → Waiting → Idle → Active → Idle. This is the end-to-end check that
// the plan calls for: hooks file wiring, socket forwarding, agent status
// transitions, and dashboard rendering all working in concert.
func TestHookPipeline(t *testing.T) {
	// Install the stub as a file named `claude` in a short-path temp dir.
	// The basename must be exactly "claude" so baton's supportsHooks check
	// fires and the agent gets --settings + socket env wired up.
	stubDir, err := os.MkdirTemp("", "bs")
	if err != nil {
		t.Fatalf("mkdir stub dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stubDir) })
	stubPath := filepath.Join(stubDir, "claude")
	if err := os.WriteFile(stubPath, []byte(claudeStubScript), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	s := newSession(t)
	// Point both global and repo config at the stub so whichever baton reads
	// wins, and pass BATON_E2E_BATON through tu so the stub can invoke the
	// hook CLI without needing to know the binary path itself.
	writeJSON(t, filepath.Join(s.home, ".baton", "config.json"), map[string]any{
		"agent_program":      stubPath,
		"bypass_permissions": false,
	})
	writeJSON(t, filepath.Join(s.repoDir, ".baton", "config.json"), map[string]any{
		"agent_program":      stubPath,
		"bypass_permissions": false,
	})
	s.extraEnv = append(s.extraEnv, "BATON_E2E_BATON="+batonBin)
	s.Start()

	s.WaitForText("AGENTS", 10000)
	s.Press("n")
	// After "n", baton spawns the stub and auto-focuses its PTY. The stub
	// prints a greeting; wait for it so we know the process is live before
	// bouncing back to the dashboard to read status symbols.
	s.WaitForText("claude-e2e-stub ready", 10000)
	s.Press("Escape")
	s.WaitForText("navigate", 10000)

	// Active (●) — session-start fires at t≈0.3s.
	if !waitForAgentSymbol(s, "●", 5000) {
		t.Fatalf("never observed Active (●) symbol\nScreen:\n%s", s.Screenshot())
	}
	// Waiting (◐) — notification fires at t≈1.5s.
	if !waitForAgentSymbol(s, "◐", 5000) {
		t.Fatalf("never observed Waiting (◐) symbol\nScreen:\n%s", s.Screenshot())
	}
	// Idle (○) — stop fires at t≈3.5s.
	if !waitForAgentSymbol(s, "○", 5000) {
		t.Fatalf("never observed Idle (○) after Stop\nScreen:\n%s", s.Screenshot())
	}
	// Active (●) again — user-prompt-submit fires at t≈5.5s and re-arms.
	// This is the observable signal that UserPromptSubmit transitioned the
	// bubble back to Active. (The chime re-arm flag is not visible on
	// screen; the second Idle below confirms the manager saw the transition
	// and emitted a status-change event.)
	if !waitForAgentSymbol(s, "●", 5000) {
		t.Fatalf("never observed re-armed Active (●) after UserPromptSubmit\nScreen:\n%s", s.Screenshot())
	}
	// Idle (○) again — stop fires at t≈7.5s. This doubles as a re-arm check:
	// if UserPromptSubmit hadn't re-armed, there'd be no intermediate Active
	// and the second Stop would be a no-op status-wise.
	if !waitForAgentSymbol(s, "○", 5000) {
		t.Fatalf("never observed final Idle (○) after second Stop\nScreen:\n%s", s.Screenshot())
	}
}

// waitForAgentSymbol polls Screenshot until an agent row with the given
// leading status symbol is visible, or timeoutMs elapses.
func waitForAgentSymbol(s *Session, symbol string, timeoutMs int) bool {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		if screenHasAgentSymbol(s.Screenshot(), symbol) {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return screenHasAgentSymbol(s.Screenshot(), symbol)
}

// screenHasAgentSymbol reports whether any line in screen is an agent row
// starting (after whitespace and optional selection arrow) with symbol+space.
// Session-header rows (which contain box-drawing "──") are skipped so a
// session's rolled-up status doesn't mask the agent-row symbol we care about.
func screenHasAgentSymbol(screen, symbol string) bool {
	needle := symbol + " "
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, "──") {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		trimmed = strings.TrimPrefix(trimmed, "▸ ")
		if strings.HasPrefix(trimmed, needle) {
			return true
		}
	}
	return false
}
