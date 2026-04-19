package agent

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/hook"
)

// TestManagerDispatchesHookEventsToAgent verifies the full path: an external
// process (simulated via hook.SendEvent from this test) writes to the socket
// at <repoPath>/.baton/hook.sock, the Manager's dispatcher routes by agent ID,
// and the agent's state transitions accordingly.
func TestManagerDispatchesHookEventsToAgent(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	if mgr.HookSocketPath() == "" {
		t.Fatal("expected hook socket path to be set after NewManager")
	}

	cfg := Config{Name: "hook-dispatch", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		// Long-running process so the agent doesn't exit mid-test.
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = sess

	// Drain the initial EventCreated event so we can detect EventStatusChanged
	// below deterministically.
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// Send SessionStart and assert it routes to the agent.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:      hook.KindSessionStart,
		AgentID:   ag.ID,
		SessionID: "claude-uuid-42",
	}); err != nil {
		t.Fatalf("SendEvent SessionStart: %v", err)
	}

	// Manager emits EventStatusChanged on each hook-driven status mutation.
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after SessionStart, got %s", ag.Status())
	}
	if got := ag.ClaudeSessionID(); got != "claude-uuid-42" {
		t.Errorf("expected claude session id %q, got %q", "claude-uuid-42", got)
	}

	// Simulate Claude's Stop hook at the end of a turn.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindStop,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent Stop: %v", err)
	}

	if !waitForStatus(t, ag, StatusIdle, 2*time.Second) {
		t.Fatalf("expected Idle after Stop, got %s", ag.Status())
	}
}

// TestManagerDropsUnknownAgentID confirms hook events for an unknown agent are
// silently dropped (e.g. late Stop arriving after a kill).
func TestManagerDropsUnknownAgentID(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	// Send an event for an agent that doesn't exist — must not panic.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindSessionStart,
		AgentID: "does-not-exist",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	// Give the dispatcher time to process the drop.
	time.Sleep(200 * time.Millisecond)
}

// TestSocketPathPerRepo ensures two managers on different repos use distinct sockets.
func TestSocketPathPerRepo(t *testing.T) {
	repo1 := setupTestRepo(t)
	repo2 := setupTestRepo(t)
	mgr1 := NewManager(repo1, defaultTestSettings())
	defer mgr1.Shutdown()
	mgr2 := NewManager(repo2, defaultTestSettings())
	defer mgr2.Shutdown()

	if mgr1.HookSocketPath() == "" || mgr2.HookSocketPath() == "" {
		t.Fatal("expected both managers to have socket paths")
	}
	if mgr1.HookSocketPath() == mgr2.HookSocketPath() {
		t.Errorf("expected distinct socket paths; got %q", mgr1.HookSocketPath())
	}
}

// TestManagerDispatchesNotificationAndStop verifies the Notification hook drives
// the agent to StatusWaiting and a subsequent Stop returns it to StatusIdle.
func TestManagerDispatchesNotificationAndStop(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "notif-dispatch", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}

	// Drain EventCreated.
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// SessionStart → Active.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindSessionStart,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent SessionStart: %v", err)
	}
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after SessionStart, got %s", ag.Status())
	}

	// Notification → Waiting.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindNotification,
		AgentID: ag.ID,
		Message: "Claude needs your permission to use Bash",
	}); err != nil {
		t.Fatalf("SendEvent Notification: %v", err)
	}
	if !waitForStatus(t, ag, StatusWaiting, 2*time.Second) {
		t.Fatalf("expected Waiting after Notification, got %s", ag.Status())
	}

	// Stop → Idle.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindStop,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent Stop: %v", err)
	}
	if !waitForStatus(t, ag, StatusIdle, 2*time.Second) {
		t.Fatalf("expected Idle after Stop, got %s", ag.Status())
	}
}

// TestManagerUserPromptSubmitRearmsChime verifies UserPromptSubmit both
// re-arms the chime flag and transitions Idle→Active.
func TestManagerUserPromptSubmitRearmsChime(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "ups-dispatch", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// Seed: agent is Idle and the chime has already fired this turn.
	ag.mu.Lock()
	ag.status = StatusIdle
	ag.chimedForTurn = true
	ag.mu.Unlock()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent UserPromptSubmit: %v", err)
	}

	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after UserPromptSubmit, got %s", ag.Status())
	}
	if ag.ChimedForTurn() {
		t.Error("expected chimedForTurn to be reset by UserPromptSubmit")
	}
}

// TestDoneAgentIgnoresLateNotification verifies a Done agent stays Done
// when a late Notification event arrives (e.g. race between Claude emitting
// a prompt and the agent process having already been killed/finished).
func TestDoneAgentIgnoresLateNotification(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "done-ignore", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// Force the agent to Done.
	ag.mu.Lock()
	ag.status = StatusDone
	ag.mu.Unlock()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindNotification,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent Notification: %v", err)
	}

	// Give the dispatcher time to process — the status must remain Done.
	time.Sleep(200 * time.Millisecond)
	if got := ag.Status(); got != StatusDone {
		t.Errorf("expected Done to be preserved, got %s", got)
	}
}

// TestDoneAgentIgnoresLateUserPromptSubmit verifies a Done agent stays Done
// (and its chimedForTurn flag is NOT reset) when a stray UserPromptSubmit
// event arrives after the agent has already exited.
func TestDoneAgentIgnoresLateUserPromptSubmit(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "done-ignore-ups", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// Force Done, and set chimedForTurn=true so a silent reset would be
	// observable if the guard regresses.
	ag.mu.Lock()
	ag.status = StatusDone
	ag.chimedForTurn = true
	ag.mu.Unlock()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent UserPromptSubmit: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if got := ag.Status(); got != StatusDone {
		t.Errorf("expected Done to be preserved, got %s", got)
	}
	ag.mu.Lock()
	chimed := ag.chimedForTurn
	ag.mu.Unlock()
	if !chimed {
		t.Error("expected chimedForTurn to stay true on Done agent, got reset")
	}
}

// TestManagerPreToolUseClearsWaiting verifies PreToolUse transitions a Waiting
// agent back to Active — the fix path for approved permission prompts, where
// Claude does not fire UserPromptSubmit but does fire PreToolUse when it
// resumes tool execution.
func TestManagerPreToolUseClearsWaiting(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "pretooluse-dispatch", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// SessionStart → Active.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindSessionStart,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent SessionStart: %v", err)
	}
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after SessionStart, got %s", ag.Status())
	}

	// Notification → Waiting.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindNotification,
		AgentID: ag.ID,
		Message: "Claude needs your permission to use Bash",
	}); err != nil {
		t.Fatalf("SendEvent Notification: %v", err)
	}
	if !waitForStatus(t, ag, StatusWaiting, 2*time.Second) {
		t.Fatalf("expected Waiting after Notification, got %s", ag.Status())
	}

	// Mark chimedForTurn so we can verify PreToolUse does NOT reset it —
	// chime re-arming is a per-turn signal, not a per-tool-call signal.
	ag.mu.Lock()
	ag.chimedForTurn = true
	ag.mu.Unlock()

	// PreToolUse → Active (permission approved, Claude resumed).
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindPreToolUse,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent PreToolUse: %v", err)
	}
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after PreToolUse, got %s", ag.Status())
	}
	if !ag.ChimedForTurn() {
		t.Error("expected chimedForTurn to remain true across PreToolUse (it's per-turn, not per-tool)")
	}
}

// TestDoneAgentIgnoresLatePreToolUse verifies a Done agent stays Done when a
// stray PreToolUse event arrives after the agent has already exited.
func TestDoneAgentIgnoresLatePreToolUse(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "done-ignore-ptu", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	ag.mu.Lock()
	ag.status = StatusDone
	ag.chimedForTurn = true
	ag.mu.Unlock()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindPreToolUse,
		AgentID: ag.ID,
	}); err != nil {
		t.Fatalf("SendEvent PreToolUse: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if got := ag.Status(); got != StatusDone {
		t.Errorf("expected Done to be preserved, got %s", got)
	}
	ag.mu.Lock()
	chimed := ag.chimedForTurn
	ag.mu.Unlock()
	if !chimed {
		t.Error("expected chimedForTurn to stay true on Done agent, got reset")
	}
}

// TestUserPromptSubmitRenamesBranch verifies the first UserPromptSubmit with
// real prompt text renames the session's random branch to a prompt-derived
// slug, and a second prompt is a no-op because HasClaudeName is now set.
func TestUserPromptSubmitRenamesBranch(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "warm-ibis", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	originalBranch := sess.Worktree.Branch

	// Slash-only prompt does not trigger a rename.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "/clear",
	}); err != nil {
		t.Fatalf("SendEvent slash: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if sess.HasClaudeName() {
		t.Error("slash-only prompt should not flip HasClaudeName")
	}
	if sess.Branch() != originalBranch {
		t.Errorf("slash-only prompt should not rename; got %q", sess.Branch())
	}

	// Real prompt triggers rename.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "add dark mode to dashboard",
	}); err != nil {
		t.Fatalf("SendEvent prompt: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.HasClaudeName() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sess.HasClaudeName() {
		t.Fatal("expected HasClaudeName true after prompt")
	}
	if got := sess.Branch(); got != "baton/add-dark-mode-to-dashboard" {
		t.Errorf("expected branch baton/add-dark-mode-to-dashboard, got %q", got)
	}
	if got := sess.CurrentName(); got != "add-dark-mode-to-dashboard" {
		t.Errorf("expected Name add-dark-mode-to-dashboard, got %q", got)
	}

	// Second real prompt is a no-op.
	prevBranch := sess.Branch()
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "now add light mode too",
	}); err != nil {
		t.Fatalf("SendEvent second: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := sess.Branch(); got != prevBranch {
		t.Errorf("second prompt should be no-op; got %q, want %q", got, prevBranch)
	}
}

// TestUserPromptSubmitSlashWithArgRenamesBranch verifies that a skill
// invocation like "/plan-it add dark mode" renames the branch using the
// argument text, while a pure slash command like "/clear" still does not.
func TestUserPromptSubmitSlashWithArgRenamesBranch(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "cold-ferret", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// Slash command with argument triggers rename using the argument text.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "/plan-it add dark mode",
	}); err != nil {
		t.Fatalf("SendEvent slash+arg: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.HasClaudeName() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sess.HasClaudeName() {
		t.Fatal("expected HasClaudeName true after slash+arg prompt")
	}
	if got := sess.Branch(); got != "baton/add-dark-mode" {
		t.Errorf("expected branch baton/add-dark-mode, got %q", got)
	}
}

// waitForStatus polls the agent status up to d for the desired value.
func waitForStatus(t *testing.T, a *Agent, want Status, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if a.Status() == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// waitForClaudeName polls HasClaudeName() up to d for the desired value.
// The rename side-effect runs after OnHookEvent inside the dispatcher goroutine,
// so tests that care about naming must wait on this rather than on status.
func waitForClaudeName(t *testing.T, a *Agent, want bool, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if a.HasClaudeName() == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// sendUserPromptSubmit is a convenience wrapper that dispatches a
// UserPromptSubmit event through the manager's hook socket and waits for
// HasClaudeName() to flip — the only reliable signal that applyAutoName has
// finished running on the dispatcher goroutine.
func sendUserPromptSubmit(t *testing.T, mgr *Manager, a *Agent, prompt string) {
	t.Helper()
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: a.ID,
		Prompt:  prompt,
	}); err != nil {
		t.Fatalf("SendEvent UserPromptSubmit: %v", err)
	}
	if !waitForClaudeName(t, a, true, 2*time.Second) {
		t.Fatalf("HasClaudeName did not flip true after UserPromptSubmit")
	}
}

// TestManagerRenamesOnFirstUserPromptSubmit verifies that the first
// UserPromptSubmit with a non-empty prompt slugifies and applies it as the
// display name on both the agent and its session.
func TestManagerRenamesOnFirstUserPromptSubmit(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-first", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	if sess.HasDisplayName() {
		t.Fatalf("precondition: fresh random-named session should not have a display name")
	}

	sendUserPromptSubmit(t, mgr, ag, "Investigate flaky checkout test!")

	const want = "investigate-flaky-checkout-test"
	if got := ag.GetDisplayName(); got != want {
		t.Errorf("agent display name: got %q, want %q", got, want)
	}
	if got := sess.GetDisplayName(); got != want {
		t.Errorf("session display name: got %q, want %q", got, want)
	}
}

// TestManagerSecondUserPromptSubmitDoesNotRename verifies that once
// HasClaudeName is set (on the first prompt), subsequent UserPromptSubmit
// events leave the display name alone even when the new prompt is different.
func TestManagerSecondUserPromptSubmitDoesNotRename(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-second", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	sendUserPromptSubmit(t, mgr, ag, "first prompt wins")

	const want = "first-prompt-wins"
	if got := ag.GetDisplayName(); got != want {
		t.Fatalf("after first prompt: agent = %q, want %q", got, want)
	}

	// Second prompt must be a no-op. HasClaudeName is already true, so we
	// can't reuse sendUserPromptSubmit's wait — just wait on status as a
	// barrier that the dispatcher processed the event.
	ag.mu.Lock()
	ag.status = StatusIdle
	ag.mu.Unlock()
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "later prompt should be ignored",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after second UserPromptSubmit")
	}

	if got := ag.GetDisplayName(); got != want {
		t.Errorf("agent display name changed: got %q, want %q", got, want)
	}
	if got := sess.GetDisplayName(); got != want {
		t.Errorf("session display name changed: got %q, want %q", got, want)
	}
}

// TestManagerEmptyPromptConsumesOneShotGate verifies the one-shot rule:
// a UserPromptSubmit with an empty prompt still flips HasClaudeName, so a
// subsequent non-empty prompt cannot silently rename the agent.
func TestManagerEmptyPromptConsumesOneShotGate(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-empty", Task: "initial task", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// newAgentWithCommand (used by CreateSessionWithCommand) doesn't set a
	// displayName from cfg.Task the way newAgent does — so the agent's
	// display name falls through to cfg.Name until the first UPS renames it.
	const interim = "rename-empty"
	if got := ag.GetDisplayName(); got != interim {
		t.Fatalf("precondition: agent display name, got %q want %q", got, interim)
	}

	// Empty prompt: must flip HasClaudeName but NOT rename.
	sendUserPromptSubmit(t, mgr, ag, "")
	if got := ag.GetDisplayName(); got != interim {
		t.Errorf("empty-prompt UPS renamed agent: got %q, want %q", got, interim)
	}
	if sess.HasDisplayName() {
		t.Errorf("empty-prompt UPS set a session display name")
	}

	// Follow-up non-empty prompt is ignored (gate already consumed).
	ag.mu.Lock()
	ag.status = StatusIdle
	ag.mu.Unlock()
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "too late",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	if !waitForStatus(t, ag, StatusActive, 2*time.Second) {
		t.Fatalf("expected Active after second UserPromptSubmit")
	}
	if got := ag.GetDisplayName(); got != interim {
		t.Errorf("late prompt renamed agent past the gate: got %q, want %q", got, interim)
	}
}

// TestManagerPreservesSessionDisplayName verifies that when a session already
// has a display name (e.g. derived from an attached branch via
// slugifyBranchName, or restored from persisted state), the first
// UserPromptSubmit still renames the agent but leaves the session name alone.
func TestManagerPreservesSessionDisplayName(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-branch", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	const sessPreset = "add-login"
	sess.SetDisplayName(sessPreset)

	sendUserPromptSubmit(t, mgr, ag, "refactor auth middleware")

	const wantAgent = "refactor-auth-middleware"
	if got := ag.GetDisplayName(); got != wantAgent {
		t.Errorf("agent rename: got %q, want %q", got, wantAgent)
	}
	if got := sess.GetDisplayName(); got != sessPreset {
		t.Errorf("session display name overwritten: got %q, want %q", got, sessPreset)
	}
}

// TestManagerDoneAgentIgnoresLateRename verifies that a stray UserPromptSubmit
// event for a Done/Error agent doesn't auto-rename the terminal row. Mirrors
// the Done-agent guard in OnHookEvent.
func TestManagerDoneAgentIgnoresLateRename(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-done", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	ag.mu.Lock()
	ag.status = StatusDone
	ag.mu.Unlock()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "too late to rename",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	// Give the dispatcher time to process — no observable barrier since the
	// status guard in OnHookEvent short-circuits and HasClaudeName stays false.
	time.Sleep(200 * time.Millisecond)

	if ag.HasClaudeName() {
		t.Error("HasClaudeName flipped true on a Done agent")
	}
	if got := ag.GetDisplayName(); got != "rename-done" {
		t.Errorf("Done agent renamed: got %q, want %q", got, "rename-done")
	}
	if sess.HasDisplayName() {
		t.Error("session renamed by late UPS on Done agent")
	}
}

// TestManagerUnslugifiablePromptLeavesNamesUnchanged verifies a prompt that
// slugifies to empty (e.g. all punctuation) doesn't clear existing names and
// still consumes the one-shot gate.
func TestManagerUnslugifiablePromptLeavesNamesUnchanged(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	cfg := Config{Name: "rename-punct", Task: "keep me", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	// See TestManagerEmptyPromptConsumesOneShotGate — custom-command agents
	// don't derive a display name from cfg.Task, so the baseline is cfg.Name.
	const interim = "rename-punct"
	if got := ag.GetDisplayName(); got != interim {
		t.Fatalf("precondition: agent display name, got %q want %q", got, interim)
	}

	sendUserPromptSubmit(t, mgr, ag, "!!! ??? ...")

	if got := ag.GetDisplayName(); got != interim {
		t.Errorf("agent display name overwritten: got %q, want %q", got, interim)
	}
	if sess.HasDisplayName() {
		t.Errorf("session display name set from unslugifiable prompt")
	}
}

// smartBranchTestSettings returns test settings with SmartBranchNames enabled
// — defaultTestSettings() disables it to keep existing tests on the
// synchronous slugify path.
func smartBranchTestSettings() config.ResolvedSettings {
	r := config.Resolve(nil, nil)
	r.SmartBranchNames = true
	return r
}

// waitForBranch polls Session.Branch() until it matches want or the deadline
// elapses. Returns the last-observed branch on timeout for useful error output.
func waitForBranch(t *testing.T, sess *Session, want string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := sess.Branch(); got == want {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sess.Branch()
}

// waitForBranchChanged polls until Session.Branch() is no longer equal to
// original, or the deadline elapses.
func waitForBranchChanged(t *testing.T, sess *Session, original string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := sess.Branch(); got != original {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sess.Branch()
}

// stubNamer is a BranchNamer that returns a fixed result and counts calls.
type stubNamer struct {
	result string
	err    error
	calls  atomic.Int32
	block  chan struct{} // if non-nil, the namer blocks on receive before returning
}

func (s *stubNamer) fn() BranchNamer {
	return func(ctx context.Context, prompt string) (string, error) {
		s.calls.Add(1)
		if s.block != nil {
			select {
			case <-s.block:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return s.result, s.err
	}
}

// TestSmartBranchRename_HappyPath verifies the stub namer's result is
// slugified and applied to the session's branch when SmartBranchNames=true.
func TestSmartBranchRename_HappyPath(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, smartBranchTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "fix-login-flow"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "smart-happy", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	original := sess.Branch()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "we need to fix the broken login flow asap",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	got := waitForBranchChanged(t, sess, original, 2*time.Second)
	if got != "baton/fix-login-flow" {
		t.Errorf("branch = %q, want baton/fix-login-flow", got)
	}
	if !sess.HasClaudeName() {
		t.Error("HasClaudeName should be true after rename")
	}
	if stub.calls.Load() != 1 {
		t.Errorf("namer call count = %d, want 1", stub.calls.Load())
	}
}

// TestSmartBranchRename_NamerErrorFallsBack verifies that when the namer
// returns an error, we fall back to the synchronous slugify path.
func TestSmartBranchRename_NamerErrorFallsBack(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, smartBranchTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{err: errors.New("claude not found")}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "smart-fallback", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	original := sess.Branch()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "add dark mode to dashboard",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	got := waitForBranchChanged(t, sess, original, 2*time.Second)
	if got != "baton/add-dark-mode-to-dashboard" {
		t.Errorf("fallback branch = %q, want baton/add-dark-mode-to-dashboard", got)
	}
	if stub.calls.Load() != 1 {
		t.Errorf("namer should be called once even on error, got %d", stub.calls.Load())
	}
}

// TestSmartBranchRename_EmptyAfterErrorLeavesGate verifies that if the namer
// errors AND the prompt slugifies to empty, hasClaudeName stays false so a
// later prompt retries.
func TestSmartBranchRename_EmptyAfterErrorLeavesGate(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, smartBranchTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{err: errors.New("haiku down")}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "smart-retry", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	originalBranch := sess.Branch()

	// First prompt: slash-only, so suffixFromPrompt returns empty.
	// Smart path still enters — but the namer errors, and the fallback is
	// also empty, so the gate must stay open.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "/clear",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	// Give the goroutine a moment to run and release the gate.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stub.calls.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Wait a little longer for finishRename to run.
	time.Sleep(100 * time.Millisecond)

	if sess.HasClaudeName() {
		t.Error("HasClaudeName should stay false after empty-result rename attempt")
	}
	if got := sess.Branch(); got != originalBranch {
		t.Errorf("branch should be unchanged, got %q", got)
	}

	// Swap in a successful stub and send a real prompt — the second attempt
	// should succeed.
	stub2 := &stubNamer{result: "second-attempt"}
	mgr.SetBranchNamer(stub2.fn())

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "this is the real prompt",
	}); err != nil {
		t.Fatalf("SendEvent 2: %v", err)
	}

	got := waitForBranch(t, sess, "baton/second-attempt", 2*time.Second)
	if got != "baton/second-attempt" {
		t.Errorf("second branch = %q, want baton/second-attempt", got)
	}
	if stub2.calls.Load() != 1 {
		t.Errorf("second namer call count = %d, want 1", stub2.calls.Load())
	}
}

// TestSmartBranchRename_DoubleDispatchGated verifies that a second
// UserPromptSubmit arriving while the first Haiku call is still running does
// not dispatch a second namer invocation.
func TestSmartBranchRename_DoubleDispatchGated(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, smartBranchTestSettings())
	defer mgr.Shutdown()

	release := make(chan struct{})
	stub := &stubNamer{result: "slow-result", block: release}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "smart-gate", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "first prompt",
	}); err != nil {
		t.Fatalf("SendEvent 1: %v", err)
	}

	// Wait for the first call to actually enter the stub.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && stub.calls.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if stub.calls.Load() != 1 {
		t.Fatal("first namer call did not start")
	}

	// Send a second prompt — it must NOT trigger a second namer call.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "second prompt (should be gated)",
	}); err != nil {
		t.Fatalf("SendEvent 2: %v", err)
	}

	// Give the dispatcher enough time to try to re-enter.
	time.Sleep(200 * time.Millisecond)
	if got := stub.calls.Load(); got != 1 {
		t.Errorf("namer call count = %d, want 1 (second prompt should be gated)", got)
	}

	// Release the first call and ensure the rename completes.
	close(release)

	got := waitForBranch(t, sess, "baton/slow-result", 2*time.Second)
	if got != "baton/slow-result" {
		t.Errorf("branch = %q, want baton/slow-result", got)
	}

	// Even after completion, total call count must still be 1 — the second
	// prompt must never have invoked the namer.
	if n := stub.calls.Load(); n != 1 {
		t.Errorf("final namer call count = %d, want 1", n)
	}
}

// TestSmartBranchRename_DisabledUsesSyncSlugify verifies that when
// SmartBranchNames=false, the legacy synchronous slugify path is used and the
// namer is never invoked.
func TestSmartBranchRename_DisabledUsesSyncSlugify(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings()) // SmartBranchNames=false
	defer mgr.Shutdown()

	stub := &stubNamer{result: "should-not-appear"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "smart-off", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	sendUserPromptSubmit(t, mgr, ag, "please add dark mode")

	if stub.calls.Load() != 0 {
		t.Errorf("namer should not be called with SmartBranchNames=false, got %d", stub.calls.Load())
	}
	if got := sess.Branch(); got != "baton/please-add-dark-mode" {
		t.Errorf("branch = %q, want baton/please-add-dark-mode", got)
	}
}

// TestSmartBranchRename_ShutdownCancelsInflight verifies that Manager.Shutdown
// cancels the in-flight rename goroutine so it doesn't outlive the manager.
func TestSmartBranchRename_ShutdownCancelsInflight(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, smartBranchTestSettings())

	// Block the namer forever — only ctx cancellation should release it.
	stub := &stubNamer{result: "never-returns", block: make(chan struct{})}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "shutdown-cancel", Task: "test", Rows: 24, Cols: 80}
	_, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
		return exec.Command("bash", "-c", "sleep 5")
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-mgr.Events():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for EventCreated")
	}

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "trigger a rename that will block",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	// Wait for the goroutine to actually enter the namer.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && stub.calls.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if stub.calls.Load() < 1 {
		t.Fatal("namer did not run before shutdown")
	}

	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer once.Do(func() { close(done) })
		mgr.Shutdown()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return — rename goroutine likely leaked")
	}
}
