package agent

import (
	"os/exec"
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
