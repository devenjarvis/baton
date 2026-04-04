package tui

import (
	"os"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/agent"
)

// createAgent presses 'n' and executes the async create cmd, returning the updated app.
// If the terminal panel is already focused it presses Esc first so the 'n' key isn't
// forwarded to the agent.
func createAgent(t *testing.T, app App) App {
	t.Helper()

	// Return to list focus if terminal has focus so 'n' is handled by the app.
	if app.dashboard.panelFocus == focusTerminal {
		model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		app = model.(App)
	}

	model, cmd := app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	app = model.(App)

	if cmd == nil {
		t.Fatal("Expected cmd from 'n', got nil")
	}

	msg := cmd()
	model, _ = app.Update(msg)
	app = model.(App)

	return app
}

// addAgentToSession presses 'c' and executes the async add cmd, returning the updated app.
func addAgentToSession(t *testing.T, app App) App {
	t.Helper()

	// Return to list focus if terminal has focus so 'c' is handled by the app.
	if app.dashboard.panelFocus == focusTerminal {
		model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		app = model.(App)
	}

	model, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	app = model.(App)

	if cmd == nil {
		t.Fatal("Expected cmd from 'c', got nil")
	}

	msg := cmd()
	model, _ = app.Update(msg)
	app = model.(App)

	return app
}

func TestCreateAgentViaN(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-tui-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	app = createAgent(t, app)

	t.Logf("After creation: view=%v, err=%q, agents=%d, dashboard=%d, focus=%v",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()), app.dashboard.panelFocus)

	if app.view != ViewDashboard {
		t.Errorf("Expected ViewDashboard, got %v", app.view)
	}
	if app.err != "" {
		t.Errorf("Error: %s", app.err)
	}
	if mgr.AgentCount() != 1 {
		t.Errorf("Expected 1 agent, got %d", mgr.AgentCount())
	}
	if len(app.dashboard.agentItems()) != 1 {
		t.Errorf("Expected 1 dashboard agent, got %d", len(app.dashboard.agentItems()))
	}
	// After creation the terminal panel is auto-focused.
	if app.dashboard.panelFocus != focusTerminal {
		t.Errorf("Expected focusTerminal after creation, got %v", app.dashboard.panelFocus)
	}
	// Session should be present.
	sessions := mgr.ListSessions()
	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}
}

func TestCreateMultipleAgentsViaTUI(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-tui-multi-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	// Create first session+agent
	t.Log("=== Creating session 1 ===")
	app = createAgent(t, app)
	t.Logf("After session1: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Session 1 error: %s", app.err)
	}
	if mgr.AgentCount() != 1 {
		t.Fatalf("Expected 1 agent, got %d", mgr.AgentCount())
	}

	// Create second session+agent
	t.Log("=== Creating session 2 ===")
	app = createAgent(t, app)
	t.Logf("After session2: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Session 2 error: %s", app.err)
	}
	if mgr.AgentCount() != 2 {
		t.Fatalf("Expected 2 agents, got %d", mgr.AgentCount())
	}
	if len(app.dashboard.agentItems()) != 2 {
		t.Fatalf("Expected 2 dashboard agents, got %d", len(app.dashboard.agentItems()))
	}

	// Create third session+agent
	t.Log("=== Creating session 3 ===")
	app = createAgent(t, app)
	t.Logf("After session3: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Session 3 error: %s", app.err)
	}
	if mgr.AgentCount() != 3 {
		t.Fatalf("Expected 3 agents, got %d", mgr.AgentCount())
	}
	if len(app.dashboard.agentItems()) != 3 {
		t.Fatalf("Expected 3 dashboard agents, got %d", len(app.dashboard.agentItems()))
	}

	// Should have 3 sessions.
	sessions := mgr.ListSessions()
	if len(sessions) != 3 {
		t.Fatalf("Expected 3 sessions, got %d", len(sessions))
	}

	t.Logf("SUCCESS: Created %d sessions with %d agents", len(sessions), len(app.dashboard.agentItems()))
}

func TestAddAgentToSessionViaC(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-tui-addagent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	// Create first session with 'n'.
	app = createAgent(t, app)
	if app.err != "" {
		t.Fatalf("Error creating session: %s", app.err)
	}
	if mgr.AgentCount() != 1 {
		t.Fatalf("Expected 1 agent, got %d", mgr.AgentCount())
	}

	sessions := mgr.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}

	// Navigate to the session row or agent row (either works for 'c').
	// The agent row should be selected already after creation + esc.

	// Add second agent with 'c'.
	app = addAgentToSession(t, app)
	if app.err != "" {
		t.Fatalf("Error adding agent: %s", app.err)
	}

	if mgr.AgentCount() != 2 {
		t.Fatalf("Expected 2 agents, got %d", mgr.AgentCount())
	}
	// Should still be the same single session.
	sessions = mgr.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("Expected still 1 session after 'c', got %d", len(sessions))
	}
	if sessions[0].AgentCount() != 2 {
		t.Fatalf("Expected 2 agents in session, got %d", sessions[0].AgentCount())
	}
}

func TestPanelFocusSwitching(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-focus-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	app = createAgent(t, app)
	if len(app.dashboard.agentItems()) == 0 {
		t.Fatal("Expected at least one agent")
	}

	// After creation the terminal is auto-focused.
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after creation, got %v", app.dashboard.panelFocus)
	}

	// Esc returns to focusList.
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
	}

	// Right arrow enters focusTerminal.
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after →, got %v", app.dashboard.panelFocus)
	}

	// Enter stays in focusTerminal (it forwards the key to the agent).
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal to persist after enter, got %v", app.dashboard.panelFocus)
	}
}

func TestActionKeysBlockedInFocusTerminal(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-block-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	app = createAgent(t, app)

	// After creation the terminal is already focused.
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after creation, got %v", app.dashboard.panelFocus)
	}

	// Press "n" — should be forwarded to agent, NOT create a new agent.
	// panelFocus must stay focusTerminal and view must stay ViewDashboard.
	model, _ := app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	app = model.(App)
	if app.view != ViewDashboard {
		t.Fatalf("Expected ViewDashboard (n forwarded to agent, not new-agent), got %v", app.view)
	}
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal to persist after 'n', got %v", app.dashboard.panelFocus)
	}
}

func TestMouseClickSelectsListItem(t *testing.T) {
	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39

	// Directly populate the list with fake items (no real processes needed).
	app.dashboard.items = []listItem{
		{kind: listItemAgent, repoPath: "/fake/repo"},
		{kind: listItemAgent, repoPath: "/fake/repo"},
		{kind: listItemAgent, repoPath: "/fake/repo"},
	}

	if app.dashboard.selected != 0 {
		t.Fatalf("Expected selected=0 initially, got %d", app.dashboard.selected)
	}

	// Click item 1: Y = dashboardTopY(0) + 2 header rows + 1 = 3
	model, _ := app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 3})
	app = model.(App)
	if app.dashboard.selected != 1 {
		t.Fatalf("Expected selected=1 after click, got %d", app.dashboard.selected)
	}
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after list click, got %v", app.dashboard.panelFocus)
	}

	// Click item 2: Y=4
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 4})
	app = model.(App)
	if app.dashboard.selected != 2 {
		t.Fatalf("Expected selected=2 after click, got %d", app.dashboard.selected)
	}

	// Click on title row (Y=0) — ignored (itemIndex = -2), selection unchanged.
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 0})
	app = model.(App)
	if app.dashboard.selected != 2 {
		t.Fatalf("Expected selected unchanged (=2) after title click, got %d", app.dashboard.selected)
	}

	// Click on separator row (Y=1) — ignored (itemIndex = -1), selection unchanged.
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 1})
	app = model.(App)
	if app.dashboard.selected != 2 {
		t.Fatalf("Expected selected unchanged (=2) after separator click, got %d", app.dashboard.selected)
	}

	// Right-click on item 0 — ignored (not MouseLeft), selection unchanged.
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 5, Y: 2})
	app = model.(App)
	if app.dashboard.selected != 2 {
		t.Fatalf("Expected selected unchanged (=2) after right-click, got %d", app.dashboard.selected)
	}

	// With an active error banner (dashboardTopY=1), item 0 is now at Y=2+1=3.
	// Click Y=3 should still select item 0, not item 1.
	app.dashboard.selected = 2
	app.setError("test error")
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 3})
	app = model.(App)
	if app.dashboard.selected != 0 {
		t.Fatalf("Expected selected=0 with error banner offset (Y=3 → item 0), got %d", app.dashboard.selected)
	}

	// With confirmQuit=true (dashboardTopY=1 when no error), item 1 is at Y=3+1=4.
	app.err = ""
	app.errTicks = 0
	app.dashboard.selected = 0
	app.confirmQuit = true
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 4})
	app = model.(App)
	if app.dashboard.selected != 1 {
		t.Fatalf("Expected selected=1 with confirmQuit offset (Y=4 → item 1), got %d", app.dashboard.selected)
	}
	// Mouse click should also clear confirmQuit.
	if app.confirmQuit {
		t.Fatalf("Expected confirmQuit=false after mouse click, got true")
	}
}

func TestMouseClickPreviewEntersFocus(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-mouse-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	app = createAgent(t, app)
	if len(app.dashboard.agentItems()) == 0 {
		t.Fatal("Expected at least one agent")
	}

	// After creation the terminal is auto-focused; press Esc to return to list.
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
	}

	// Click the preview panel (X >= 32) — should enter focusTerminal.
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 60, Y: 10})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after preview click, got %v", app.dashboard.panelFocus)
	}

	// Esc returns to focusList.
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
	}
}

func TestMouseWheelScrollInFocusTerminal(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-wheel-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir)
	defer mgr.Shutdown()

	// Create a session with an agent that writes 40 lines.
	sess, ag, err := mgr.CreateSessionWithCommand(agent.Config{
		Name:     "wheel-test",
		Task:     "test",
		RepoPath: dir,
		Rows:     24,
		Cols:     80,
	}, func(_ string) *exec.Cmd {
		return exec.Command("bash", "-c", "for i in $(seq 1 40); do echo Line $i; done; sleep 10")
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = sess // used for session creation

	// Wait for bash output to be processed into scrollback.
	time.Sleep(300 * time.Millisecond)

	if len(ag.ScrollbackLines()) == 0 {
		t.Fatal("Expected scrollback lines after bash output")
	}

	// Build an app with this agent directly in dashboard items.
	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir
	app.dashboard.items = []listItem{
		{kind: listItemAgent, repoPath: dir, session: sess, agent: ag},
	}
	app.dashboard.panelFocus = focusTerminal

	// a. WheelUp in focusTerminal increases scrollOffset by 3.
	app.dashboard.scrollOffset = 0
	model, _ := app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	app = model.(App)
	if app.dashboard.scrollOffset != 3 {
		t.Fatalf("Expected scrollOffset=3 after WheelUp, got %d", app.dashboard.scrollOffset)
	}

	// b. WheelDown in focusTerminal decreases scrollOffset (clamped to 0).
	app.dashboard.scrollOffset = 3
	model, _ = app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	app = model.(App)
	if app.dashboard.scrollOffset != 0 {
		t.Fatalf("Expected scrollOffset=0 after WheelDown from 3, got %d", app.dashboard.scrollOffset)
	}

	// Another WheelDown should not go negative.
	model, _ = app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	app = model.(App)
	if app.dashboard.scrollOffset != 0 {
		t.Fatalf("Expected scrollOffset=0 after WheelDown from 0 (no negative), got %d", app.dashboard.scrollOffset)
	}

	// a2. WheelUp ceiling clamp: offset above sbLen is clamped to sbLen.
	app.dashboard.panelFocus = focusTerminal
	sbLen := len(ag.ScrollbackLines())
	app.dashboard.scrollOffset = sbLen + 100
	model, _ = app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	app = model.(App)
	if app.dashboard.scrollOffset != sbLen {
		t.Fatalf("Expected scrollOffset clamped to %d, got %d", sbLen, app.dashboard.scrollOffset)
	}

	// c. WheelUp in focusList is a no-op.
	app.dashboard.panelFocus = focusList
	app.dashboard.scrollOffset = 0
	model, _ = app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	app = model.(App)
	if app.dashboard.scrollOffset != 0 {
		t.Fatalf("Expected scrollOffset=0 (no-op in focusList), got %d", app.dashboard.scrollOffset)
	}
}

func TestErrorPersistsAcrossTicks(t *testing.T) {
	app := NewApp()
	app.width = 120
	app.height = 40

	// Set an error
	app.setError("test error")

	if app.err != "test error" {
		t.Fatalf("Expected error 'test error', got %q", app.err)
	}
	if app.errTicks != 30 {
		t.Fatalf("Expected 30 ticks, got %d", app.errTicks)
	}

	// Simulate a few ticks — error should persist
	for i := 0; i < 5; i++ {
		model, _ := app.Update(tickMsg{})
		app = model.(App)
	}

	if app.err != "test error" {
		t.Fatalf("Error should persist after 5 ticks, got %q", app.err)
	}
	if app.errTicks != 25 {
		t.Fatalf("Expected 25 ticks remaining, got %d", app.errTicks)
	}

	// Simulate remaining ticks
	for i := 0; i < 25; i++ {
		model, _ := app.Update(tickMsg{})
		app = model.(App)
	}

	if app.err != "" {
		t.Fatalf("Error should be cleared after 30 ticks, got %q", app.err)
	}
}
