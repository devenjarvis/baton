package agent

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// TestUserPromptSubmitRenamesBranch verifies the first actionable
// UserPromptSubmit drives the namer-based rename to completion, and a second
// prompt is a no-op because HasClaudeName is now set on the session.
func TestUserPromptSubmitRenamesBranch(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "add-dark-mode-to-dashboard"}
	mgr.SetBranchNamer(stub.fn())

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

	// Slash-only prompt is non-actionable: no rename, namer never invoked.
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
	if stub.calls.Load() != 0 {
		t.Errorf("namer should not be called for slash-only prompt, got %d", stub.calls.Load())
	}

	// Real prompt triggers rename via the stub namer.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "add dark mode to dashboard",
	}); err != nil {
		t.Fatalf("SendEvent prompt: %v", err)
	}

	got := waitForBranch(t, sess, "baton/add-dark-mode-to-dashboard", 2*time.Second)
	if got != "baton/add-dark-mode-to-dashboard" {
		t.Errorf("expected branch baton/add-dark-mode-to-dashboard, got %q", got)
	}
	if !sess.HasClaudeName() {
		t.Fatal("expected HasClaudeName true after prompt")
	}
	if got := sess.CurrentName(); got != "add-dark-mode-to-dashboard" {
		t.Errorf("expected Name add-dark-mode-to-dashboard, got %q", got)
	}

	// Second real prompt is a no-op (gate already consumed at the session level).
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
	if stub.calls.Load() != 1 {
		t.Errorf("namer should only be called once total, got %d", stub.calls.Load())
	}
}

// TestUserPromptSubmitSlashWithArgRenamesBranch verifies that a skill
// invocation like "/plan-it add dark mode" is treated as actionable and
// reaches the namer (which sees the full prompt; how it summarizes is up to
// the model). A bare "/plan-it" with no args remains non-actionable.
func TestUserPromptSubmitSlashWithArgRenamesBranch(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "add-dark-mode"}
	mgr.SetBranchNamer(stub.fn())

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

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "/plan-it add dark mode",
	}); err != nil {
		t.Fatalf("SendEvent slash+arg: %v", err)
	}

	got := waitForBranch(t, sess, "baton/add-dark-mode", 2*time.Second)
	if got != "baton/add-dark-mode" {
		t.Errorf("expected branch baton/add-dark-mode, got %q", got)
	}
	if !sess.HasClaudeName() {
		t.Fatal("expected HasClaudeName true after slash+arg prompt")
	}
	if stub.calls.Load() != 1 {
		t.Errorf("namer call count = %d, want 1", stub.calls.Load())
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
// Agent.HasClaudeName() to flip — the only reliable signal that the
// dispatcher goroutine has processed the rename request. The caller must
// pass an actionable prompt (non-empty, not a bare slash command); empty or
// slash-only prompts skip the rename flow and never flip the flag.
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
// actionable UserPromptSubmit drives the namer-based rename and applies the
// resulting slug as the display name on both the agent and its session.
func TestManagerRenamesOnFirstUserPromptSubmit(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "investigate-flaky-checkout-test"}
	mgr.SetBranchNamer(stub.fn())

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
	// The display-name update happens in the rename goroutine after the
	// namer returns; the agent display name is the last write so wait on it.
	if got := waitForAgentDisplayName(t, ag, want, 2*time.Second); got != want {
		t.Fatalf("agent display name: got %q, want %q", got, want)
	}
	if got := sess.Branch(); got != "baton/"+want {
		t.Errorf("branch: got %q, want baton/%s", got, want)
	}
	if got := sess.GetDisplayName(); got != want {
		t.Errorf("session display name: got %q, want %q", got, want)
	}
}

// TestManagerSecondUserPromptSubmitDoesNotRename verifies that once the
// session's HasClaudeName is set (after a successful first rename), subsequent
// UserPromptSubmit events do not invoke the namer or change display names.
func TestManagerSecondUserPromptSubmitDoesNotRename(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "first-prompt-wins"}
	mgr.SetBranchNamer(stub.fn())

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
	if got := waitForAgentDisplayName(t, ag, want, 2*time.Second); got != want {
		t.Fatalf("after first prompt: agent = %q, want %q", got, want)
	}
	if got := sess.Branch(); got != "baton/"+want {
		t.Fatalf("after first prompt: branch = %q, want baton/%s", got, want)
	}

	// Second prompt must be a no-op. HasClaudeName is already true on the
	// session, so we use status as a barrier confirming the dispatcher
	// processed the event before we assert.
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
	if stub.calls.Load() != 1 {
		t.Errorf("namer call count = %d, want 1", stub.calls.Load())
	}
}

// TestManagerEmptyPromptDoesNotConsumeGate verifies the new flow: an empty
// UserPromptSubmit skips the rename pipeline entirely (no namer call, gate
// stays open), so a follow-up non-empty prompt still renames normally.
func TestManagerEmptyPromptDoesNotConsumeGate(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "real-prompt-result"}
	mgr.SetBranchNamer(stub.fn())

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

	const interim = "rename-empty"
	if got := ag.GetDisplayName(); got != interim {
		t.Fatalf("precondition: agent display name, got %q want %q", got, interim)
	}

	// Empty prompt: namer never invoked, gate stays open.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "",
	}); err != nil {
		t.Fatalf("SendEvent empty: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if ag.HasClaudeName() {
		t.Error("empty prompt should not flip Agent.HasClaudeName")
	}
	if sess.HasClaudeName() {
		t.Error("empty prompt should not flip Session.HasClaudeName")
	}
	if got := ag.GetDisplayName(); got != interim {
		t.Errorf("empty-prompt UPS renamed agent: got %q, want %q", got, interim)
	}
	if sess.HasDisplayName() {
		t.Errorf("empty-prompt UPS set a session display name")
	}
	if stub.calls.Load() != 0 {
		t.Errorf("namer should not be called for empty prompt, got %d", stub.calls.Load())
	}

	// Follow-up actionable prompt: gate is still open, rename succeeds.
	sendUserPromptSubmit(t, mgr, ag, "the real prompt")
	if got := waitForBranch(t, sess, "baton/real-prompt-result", 2*time.Second); got != "baton/real-prompt-result" {
		t.Errorf("retry rename: branch = %q, want baton/real-prompt-result", got)
	}
}

// TestManagerDoneAgentIgnoresLateRename verifies that a stray UserPromptSubmit
// event for a Done/Error agent doesn't auto-rename the terminal row. Mirrors
// the Done-agent guard in maybeRenameFromPrompt.
func TestManagerDoneAgentIgnoresLateRename(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "should-not-be-called"}
	mgr.SetBranchNamer(stub.fn())

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
	// Give the dispatcher time to process — the status guard short-circuits
	// before SetClaudeName, so HasClaudeName stays false.
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
	if stub.calls.Load() != 0 {
		t.Errorf("namer should not be called for Done agent, got %d", stub.calls.Load())
	}
}

// TestManagerNamerErrorLeavesNamesUnchanged verifies that when the namer
// returns an error, the agent and session display names stay at their
// pre-prompt values and the random branch is preserved. Agent.HasClaudeName
// still flips so the resume restore path can distinguish "naming chance was
// taken" from "fresh placeholder".
func TestManagerNamerErrorLeavesNamesUnchanged(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{err: errors.New("haiku unavailable")}
	mgr.SetBranchNamer(stub.fn())

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

	const interim = "rename-punct"
	originalBranch := sess.Worktree.Branch

	sendUserPromptSubmit(t, mgr, ag, "!!! ??? ...")

	// Wait for the rename goroutine to finish so we don't race the assertions.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && stub.calls.Load() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	if got := ag.GetDisplayName(); got != interim {
		t.Errorf("agent display name overwritten on namer error: got %q, want %q", got, interim)
	}
	if sess.HasDisplayName() {
		t.Errorf("session display name set despite namer error")
	}
	if sess.HasClaudeName() {
		t.Error("Session.HasClaudeName should stay false when namer errors (so retries can fire)")
	}
	if got := sess.Branch(); got != originalBranch {
		t.Errorf("branch should be unchanged on namer error: got %q, want %q", got, originalBranch)
	}
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

// waitForAgentDisplayName polls until Agent.GetDisplayName() returns want or
// the deadline elapses. The async rename goroutine updates the branch and
// the display name in close succession but not atomically, so tests that
// check both should wait on the display name (the later update).
func waitForAgentDisplayName(t *testing.T, a *Agent, want string, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := a.GetDisplayName(); got == want {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	return a.GetDisplayName()
}

// stubNamer is a BranchNamer that returns a fixed result and counts calls.
// lastInstruction captures the most recent rendered template the manager
// passed in so tests can assert template substitution.
type stubNamer struct {
	result          string
	err             error
	calls           atomic.Int32
	block           chan struct{} // if non-nil, the namer blocks on receive before returning
	mu              sync.Mutex
	lastInstruction string
}

func (s *stubNamer) fn() BranchNamer {
	return func(ctx context.Context, instruction string) (string, error) {
		s.mu.Lock()
		s.lastInstruction = instruction
		s.mu.Unlock()
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

func (s *stubNamer) instruction() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastInstruction
}

// TestSmartBranchRename_HappyPath verifies the stub namer's result is
// applied to the session's branch through the Haiku path.
func TestSmartBranchRename_HappyPath(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
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

// TestSmartBranchRename_NamerErrorRetriesNextPrompt verifies the new
// no-fallback contract: when the namer errors, the random branch persists
// and Session.HasClaudeName stays false so the next prompt can retry.
func TestSmartBranchRename_NamerErrorRetriesNextPrompt(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	failing := &stubNamer{err: errors.New("haiku unavailable")}
	mgr.SetBranchNamer(failing.fn())

	cfg := Config{Name: "namer-error", Task: "test", Rows: 24, Cols: 80}
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

	// First prompt: namer errors → no rename, gate stays open.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "first attempt with broken namer",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && failing.calls.Load() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	// Wait for finishRename to clear the in-flight flag.
	time.Sleep(100 * time.Millisecond)

	if sess.HasClaudeName() {
		t.Error("Session.HasClaudeName should stay false after namer error")
	}
	if got := sess.Branch(); got != originalBranch {
		t.Errorf("branch should be unchanged after namer error: got %q, want %q", got, originalBranch)
	}

	// Swap in a successful stub and send another prompt — retry succeeds.
	good := &stubNamer{result: "second-attempt"}
	mgr.SetBranchNamer(good.fn())

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "this is the real prompt",
	}); err != nil {
		t.Fatalf("SendEvent retry: %v", err)
	}

	got := waitForBranch(t, sess, "baton/second-attempt", 2*time.Second)
	if got != "baton/second-attempt" {
		t.Errorf("retry branch = %q, want baton/second-attempt", got)
	}
	if good.calls.Load() != 1 {
		t.Errorf("retry namer call count = %d, want 1", good.calls.Load())
	}
}

// TestSmartBranchRename_SlashOnlyDoesNotInvokeNamer verifies that "/clear"-
// style prompts skip the rename pipeline entirely without consuming the
// in-flight gate, and a real follow-up prompt still renames.
func TestSmartBranchRename_SlashOnlyDoesNotInvokeNamer(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "real-result"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "slash-skip", Task: "test", Rows: 24, Cols: 80}
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

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "/clear",
	}); err != nil {
		t.Fatalf("SendEvent /clear: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	if stub.calls.Load() != 0 {
		t.Errorf("namer should not be called for slash-only prompt, got %d", stub.calls.Load())
	}
	if sess.HasClaudeName() {
		t.Error("Session.HasClaudeName should stay false after slash-only prompt")
	}
	if got := sess.Branch(); got != originalBranch {
		t.Errorf("slash-only prompt should not rename: got %q", got)
	}

	// Real prompt: rename succeeds, gate consumed.
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "now rename for real",
	}); err != nil {
		t.Fatalf("SendEvent retry: %v", err)
	}

	got := waitForBranch(t, sess, "baton/real-result", 2*time.Second)
	if got != "baton/real-result" {
		t.Errorf("retry branch = %q, want baton/real-result", got)
	}
	if stub.calls.Load() != 1 {
		t.Errorf("namer call count = %d, want 1", stub.calls.Load())
	}
}

// TestSmartBranchRename_DoubleDispatchGated verifies that a second
// UserPromptSubmit arriving while the first Haiku call is still running does
// not dispatch a second namer invocation.
func TestSmartBranchRename_DoubleDispatchGated(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
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

// TestSmartBranchRename_ShutdownCancelsInflight verifies that Manager.Shutdown
// cancels the in-flight rename goroutine so it doesn't outlive the manager.
func TestSmartBranchRename_ShutdownCancelsInflight(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())

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

// TestSmartBranchRename_EmitsBranchRenamedEvent asserts that a successful
// rename via applyRename emits an EventBranchRenamed carrying the new branch
// name. The TUI scheduler uses this event to burst-refresh PR state and
// recover from the rename/PR race.
func TestSmartBranchRename_EmitsBranchRenamedEvent(t *testing.T) {
	repo := setupTestRepo(t)
	mgr := NewManager(repo, defaultTestSettings())
	defer mgr.Shutdown()

	stub := &stubNamer{result: "add-dark-mode"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "rename-event", Task: "test", Rows: 24, Cols: 80}
	sess, ag, err := mgr.CreateSessionWithCommand(cfg, func(name string) *exec.Cmd {
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

	original := sess.Branch()

	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  "add dark mode to dashboard",
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	got := waitForBranchChanged(t, sess, original, 2*time.Second)
	if got != "baton/add-dark-mode" {
		t.Fatalf("branch = %q, want baton/add-dark-mode", got)
	}

	// Drain events until we see EventBranchRenamed. Other events
	// (EventStatusChanged, etc.) may interleave; skip them.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Type == EventBranchRenamed {
				if ev.SessionID != sess.ID {
					t.Errorf("SessionID = %q, want %q", ev.SessionID, sess.ID)
				}
				if ev.Branch != got {
					t.Errorf("Branch = %q, want %q", ev.Branch, got)
				}
				return
			}
		case <-deadline:
			t.Fatal("did not receive EventBranchRenamed within 2s")
		}
	}
}

// TestSmartBranchRename_CustomTemplateRendered verifies that a user-provided
// BranchNamePrompt has the {prompt} token substituted with the user's prompt
// and the rendered string is what the namer receives.
func TestSmartBranchRename_CustomTemplateRendered(t *testing.T) {
	repo := setupTestRepo(t)
	settings := defaultTestSettings()
	settings.BranchNamePrompt = "You are naming a git branch. Use 2 words. {prompt} -- end"
	mgr := NewManager(repo, settings)
	defer mgr.Shutdown()

	stub := &stubNamer{result: "fix-login"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "tmpl-render", Task: "test", Rows: 24, Cols: 80}
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

	const userPrompt = "fix the login redirect bug"
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  userPrompt,
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	got := waitForBranch(t, sess, "baton/fix-login", 2*time.Second)
	if got != "baton/fix-login" {
		t.Fatalf("branch = %q, want baton/fix-login", got)
	}

	const wantInstruction = "You are naming a git branch. Use 2 words. fix the login redirect bug -- end"
	if got := stub.instruction(); got != wantInstruction {
		t.Errorf("rendered instruction = %q, want %q", got, wantInstruction)
	}
}

// TestSmartBranchRename_TemplateWithoutPlaceholderAppendsPrompt verifies the
// defensive forgiveness: if a custom BranchNamePrompt forgets the {prompt}
// token, the user's prompt is appended on its own paragraph rather than
// silently dropped.
func TestSmartBranchRename_TemplateWithoutPlaceholderAppendsPrompt(t *testing.T) {
	repo := setupTestRepo(t)
	settings := defaultTestSettings()
	settings.BranchNamePrompt = "Header without placeholder"
	mgr := NewManager(repo, settings)
	defer mgr.Shutdown()

	stub := &stubNamer{result: "ok"}
	mgr.SetBranchNamer(stub.fn())

	cfg := Config{Name: "tmpl-append", Task: "test", Rows: 24, Cols: 80}
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

	const userPrompt = "do the thing"
	if err := hook.SendEvent(mgr.HookSocketPath(), hook.Event{
		Kind:    hook.KindUserPromptSubmit,
		AgentID: ag.ID,
		Prompt:  userPrompt,
	}); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	if got := waitForBranch(t, sess, "baton/ok", 2*time.Second); got != "baton/ok" {
		t.Fatalf("branch = %q, want baton/ok", got)
	}

	const wantInstruction = "Header without placeholder\n\ndo the thing"
	if got := stub.instruction(); got != wantInstruction {
		t.Errorf("rendered instruction = %q, want %q", got, wantInstruction)
	}
}
