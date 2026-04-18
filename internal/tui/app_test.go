package tui

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/config"
)

func requireClaude(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH")
	}
}

// createAgent presses 'n' and executes the async create cmd, returning the updated app.
// If the terminal panel is already focused it presses Ctrl+E first so the 'n' key isn't
// forwarded to the agent.
func createAgent(t *testing.T, app App) App {
	t.Helper()

	// Return to list focus if terminal has focus so 'n' is handled by the app.
	if app.dashboard.panelFocus == focusTerminal {
		model, _ := app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
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
// If the terminal panel is already focused it presses Ctrl+E first so the 'c' key isn't
// forwarded to the agent.
func addAgentToSession(t *testing.T, app App) App {
	t.Helper()

	// Return to list focus if terminal has focus so 'c' is handled by the app.
	if app.dashboard.panelFocus == focusTerminal {
		model, _ := app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-tui-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-tui-multi-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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

	// Create second session+agent (createAgent presses Ctrl+E first to exit focusTerminal)
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-tui-addagent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-focus-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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

	// Ctrl+E returns to focusList.
	model, _ := app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after ctrl+e, got %v", app.dashboard.panelFocus)
	}

	// Right arrow enters focusTerminal.
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after →, got %v", app.dashboard.panelFocus)
	}

	// Esc returns to focusList.
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
	}

	// Right arrow enters focusTerminal again.
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-block-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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

func TestShiftEscForwardsEscapeToAgent(t *testing.T) {
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-shiftesc-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	app = createAgent(t, app)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after creation, got %v", app.dashboard.panelFocus)
	}

	// Press shift+esc — should stay in focusTerminal (not exit).
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModShift})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after shift+esc (should forward, not exit), got %v", app.dashboard.panelFocus)
	}

	// Press plain esc — should exit to focusList.
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
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
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-mouse-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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

	// After creation the terminal is auto-focused; press Ctrl+E to return to list.
	model, _ := app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after ctrl+e, got %v", app.dashboard.panelFocus)
	}

	// Click the preview panel (X >= 32) — should enter focusTerminal.
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 60, Y: 10})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after preview click, got %v", app.dashboard.panelFocus)
	}

	// Ctrl+E returns to focusList.
	model, _ = app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after ctrl+e, got %v", app.dashboard.panelFocus)
	}
}

func TestMouseWheelScrollInFocusTerminal(t *testing.T) {
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-wheel-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
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

// waitForAltScreen polls ag.IsAltScreen() until true or the timeout expires.
func waitForAltScreen(t *testing.T, ag *agent.Agent) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ag.IsAltScreen() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("agent did not enter alt-screen within timeout")
}

// altScreenBashCmd emits DECSET 1049 (alt-screen) + 1002 (button-event mouse)
// + 1006 (SGR ext encoding) so the agent both enters alt-screen AND accepts
// SendMouse events, then sleeps so the process stays alive.
func altScreenBashCmd(_ string) *exec.Cmd {
	return exec.Command("bash", "-c", `printf '\033[?1049h\033[?1002h\033[?1006h'; sleep 10`)
}

func TestMouseWheelForwardsInAltScreen(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-alt-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
	defer mgr.Shutdown()

	sess, ag, err := mgr.CreateSessionWithCommand(agent.Config{
		Name: "alt-wheel", Task: "test", RepoPath: dir, Rows: 24, Cols: 80,
	}, altScreenBashCmd)
	if err != nil {
		t.Fatal(err)
	}
	waitForAltScreen(t, ag)

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

	// Set a non-zero offset so we can tell the wheel branch didn't mutate it.
	app.dashboard.scrollOffset = 5
	model, _ := app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 40, Y: 10})
	app = model.(App)
	if app.dashboard.scrollOffset != 5 {
		t.Fatalf("expected scrollOffset untouched (=5) when agent is in alt-screen, got %d", app.dashboard.scrollOffset)
	}

	model, _ = app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 40, Y: 10})
	app = model.(App)
	if app.dashboard.scrollOffset != 5 {
		t.Fatalf("expected scrollOffset untouched on WheelDown in alt-screen, got %d", app.dashboard.scrollOffset)
	}
}

func TestScrollOffsetResetsOnAltScreenEntry(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-alt-reset-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
	defer mgr.Shutdown()

	sess, ag, err := mgr.CreateSessionWithCommand(agent.Config{
		Name: "alt-reset", Task: "test", RepoPath: dir, Rows: 24, Cols: 80,
	}, altScreenBashCmd)
	if err != nil {
		t.Fatal(err)
	}
	waitForAltScreen(t, ag)

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
	app.dashboard.selected = 0
	app.dashboard.scrollOffset = 42

	// A tickMsg drives the alt-screen-entered consumer. Since selected==ag,
	// the transition should reset scrollOffset to 0.
	model, _ := app.Update(tickMsg(time.Now()))
	app = model.(App)
	if app.dashboard.scrollOffset != 0 {
		t.Fatalf("expected scrollOffset reset to 0 after alt-screen entry tick, got %d", app.dashboard.scrollOffset)
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

func TestNavigationSkipsSessionRows(t *testing.T) {
	sess := &agent.Session{Name: "test-session"}
	ag1 := &agent.Agent{Name: "agent-1"}
	ag2 := &agent.Agent{Name: "agent-2"}

	d := newDashboardModel()
	d.width = 120
	d.height = 39
	d.items = []listItem{
		{kind: listItemRepo, repoPath: "/fake/repo", repoName: "repo"},
		{kind: listItemSession, repoPath: "/fake/repo", session: sess},
		{kind: listItemAgent, repoPath: "/fake/repo", session: sess, agent: ag1},
		{kind: listItemSession, repoPath: "/fake/repo", session: sess},
		{kind: listItemAgent, repoPath: "/fake/repo", session: sess, agent: ag2},
	}
	d.selected = 0 // repo row

	// j from repo should skip session at index 1, land on agent at index 2.
	d, _ = d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if d.selected != 2 {
		t.Fatalf("Expected selected=2 (agent), got %d", d.selected)
	}

	// j from agent at 2 should skip session at 3, land on agent at 4.
	d, _ = d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if d.selected != 4 {
		t.Fatalf("Expected selected=4 (agent), got %d", d.selected)
	}

	// k from agent at 4 should skip session at 3, land on agent at 2.
	d, _ = d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if d.selected != 2 {
		t.Fatalf("Expected selected=2 (agent), got %d", d.selected)
	}

	// k from agent at 2 should skip session at 1, land on repo at 0.
	d, _ = d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if d.selected != 0 {
		t.Fatalf("Expected selected=0 (repo), got %d", d.selected)
	}
}

func TestMouseClickSessionSnapsToAgent(t *testing.T) {
	requireClaude(t)
	dir, err := os.MkdirTemp("", "baton-snap-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
	defer mgr.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir

	// Create a session with an agent.
	app = createAgent(t, app)
	if app.err != "" {
		t.Fatalf("Error creating session: %s", app.err)
	}

	// Return to list focus.
	model, _ := app.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	app = model.(App)

	// Find the session row index.
	sessionIdx := -1
	for i, item := range app.dashboard.items {
		if item.kind == listItemSession {
			sessionIdx = i
			break
		}
	}
	if sessionIdx < 0 {
		t.Fatal("No session row found in dashboard items")
	}

	// Click the session header row (Y = 2 header rows + sessionIdx).
	model, _ = app.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 2 + sessionIdx})
	app = model.(App)

	// Should snap from session to the nearest agent.
	if app.dashboard.items[app.dashboard.selected].kind == listItemSession {
		t.Fatalf("Expected selection to snap away from session row, but selected=%d is a session", app.dashboard.selected)
	}
}

// TestKillAgentAsyncMarksClosing verifies that pressing 'x' marks the agent in
// closingAgents and returns a non-nil Cmd without having called KillAgent
// synchronously — so the UI stays responsive while the teardown runs in a
// goroutine.
func TestKillAgentAsyncMarksClosing(t *testing.T) {
	dir, err := os.MkdirTemp("", "baton-killasync-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "commit.gpgsign", "false")
	run("git", "commit", "--allow-empty", "-m", "init")

	mgr := agent.NewManager(dir, config.Resolve(nil, nil))
	defer mgr.Shutdown()

	// Create a long-running bash agent (sleep 999) so KillAgent has real work
	// to do and the async path is exercised.
	sess, ag, err := mgr.CreateSessionWithCommand(
		agent.Config{Name: "kill-async", Task: "test", Rows: 24, Cols: 80},
		func(_ string) *exec.Cmd { return exec.Command("bash", "-c", "sleep 999") },
	)
	if err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir] = mgr
	app.activeRepo = dir
	app.refreshAgentList()

	// Select the agent row.
	for i, it := range app.dashboard.items {
		if it.kind == listItemAgent && it.agent != nil && it.agent.ID == ag.ID {
			app.dashboard.selected = i
			break
		}
	}

	// Press 'x' — should mark closing and return a non-nil cmd. The agent
	// must still be present in the manager because the kill is now async.
	model, cmd := app.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	app = model.(App)

	if cmd == nil {
		t.Fatal("Expected non-nil cmd from 'x' (async kill), got nil")
	}
	if !app.closingAgents[ag.ID] {
		t.Fatalf("Expected closingAgents[%s]=true, got %v", ag.ID, app.closingAgents)
	}
	// Dashboard should see the closing flag too.
	if !app.dashboard.closingAgents[ag.ID] {
		t.Fatalf("Expected dashboard.closingAgents[%s]=true", ag.ID)
	}
	// The manager still has the agent because the goroutine hasn't run yet.
	if mgr.Get(ag.ID) == nil {
		t.Fatalf("Expected agent still present in manager before cmd runs, got nil")
	}

	// Second press on the same agent is a no-op: must return nil cmd so we
	// don't double-dispatch.
	_, cmd2 := app.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd2 != nil {
		t.Fatal("Expected nil cmd from second 'x' on same agent (no double-dispatch)")
	}

	// Run the kill cmd; it should return a killResultMsg.
	msg := cmd()
	kr, ok := msg.(killResultMsg)
	if !ok {
		t.Fatalf("Expected killResultMsg from cmd, got %T", msg)
	}
	if kr.scope != killScopeAgent || kr.agentID != ag.ID || kr.sessionID != sess.ID {
		t.Fatalf("Unexpected killResultMsg: %+v", kr)
	}

	// Feed the killResultMsg back into the app — closing set should clear
	// and refreshAgentList should drop the agent from dashboard items.
	model, _ = app.Update(kr)
	app = model.(App)
	if app.closingAgents[ag.ID] {
		t.Fatalf("Expected closingAgents[%s] cleared after killResultMsg, still set", ag.ID)
	}
	for _, it := range app.dashboard.items {
		if it.kind == listItemAgent && it.agent != nil && it.agent.ID == ag.ID {
			t.Fatalf("Expected agent %s removed from dashboard after killResultMsg", ag.ID)
		}
	}
}

// TestKillResultMsgClearsClosingSet verifies the session-scope killResultMsg
// path: closingSessions and closingAgents are both cleared, diff stats cache
// is invalidated, and refreshAgentList runs.
func TestKillResultMsgClearsClosingSet(t *testing.T) {
	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39

	// Pre-populate closing sets and diff cache as if 'X' had dispatched.
	app.closingSessions["sess-1"] = true
	app.closingAgents["agent-a"] = true
	app.closingAgents["agent-b"] = true
	app.lastKnownStatus["agent-a"] = agent.StatusActive
	app.lastKnownStatus["agent-b"] = agent.StatusActive
	app.diffStatsCache["sess-1"] = &diffStatsEntry{}

	model, _ := app.Update(killResultMsg{
		scope:     killScopeSession,
		sessionID: "sess-1",
		agentIDs:  []string{"agent-a", "agent-b"},
	})
	app = model.(App)

	if app.closingSessions["sess-1"] {
		t.Fatal("Expected closingSessions[sess-1] cleared")
	}
	if app.closingAgents["agent-a"] || app.closingAgents["agent-b"] {
		t.Fatal("Expected closingAgents cleared for both agents")
	}
	if _, ok := app.diffStatsCache["sess-1"]; ok {
		t.Fatal("Expected diffStatsCache[sess-1] removed")
	}
	if _, ok := app.lastKnownStatus["agent-a"]; ok {
		t.Fatal("Expected lastKnownStatus cleared for agent-a")
	}
	// Dashboard should also see the cleared maps (refreshAgentList was called).
	if app.dashboard.closingSessions == nil {
		t.Fatal("Expected dashboard.closingSessions wired up after refresh")
	}
	if app.dashboard.closingSessions["sess-1"] {
		t.Fatal("Expected dashboard.closingSessions[sess-1] cleared after refresh")
	}
}

// TestKillResultMsgClearsClosingSetOnError verifies that if KillAgent returns
// an error, the closing-set entry is still cleared (so the row doesn't get
// stuck rendering "closing…") and the error is surfaced via setError.
func TestKillResultMsgClearsClosingSetOnError(t *testing.T) {
	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39

	app.closingAgents["agent-x"] = true

	model, _ := app.Update(killResultMsg{
		scope:     killScopeAgent,
		sessionID: "sess-1",
		agentID:   "agent-x",
		err:       errors.New("kill failed"),
	})
	app = model.(App)

	if app.closingAgents["agent-x"] {
		t.Fatal("Expected closingAgents[agent-x] cleared even on error")
	}
	if app.err != "kill failed" {
		t.Fatalf("Expected err %q, got %q", "kill failed", app.err)
	}
}

// TestRefreshAgentListRepoAffinity verifies that after killing the last agent
// in a repo's session, the cursor lands on that repo's header — not on an item
// in a different repo.
func TestRefreshAgentListRepoAffinity(t *testing.T) {
	requireClaude(t)
	// Set up two temp repos with git init.
	dir1, err := os.MkdirTemp("", "baton-repo1-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir1) }()
	dir2, err := os.MkdirTemp("", "baton-repo2-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir2) }()

	initRepo := func(dir string) {
		for _, args := range [][]string{
			{"git", "init"},
			{"git", "config", "commit.gpgsign", "false"},
			{"git", "commit", "--allow-empty", "-m", "init"},
		} {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("cmd %v in %s: %v\n%s", args, dir, err, out)
			}
		}
	}
	initRepo(dir1)
	initRepo(dir2)

	mgr1 := agent.NewManager(dir1, config.Resolve(nil, nil))
	defer mgr1.Shutdown()
	mgr2 := agent.NewManager(dir2, config.Resolve(nil, nil))
	defer mgr2.Shutdown()

	app := NewApp()
	app.width = 120
	app.height = 40
	app.dashboard.width = 120
	app.dashboard.height = 39
	app.managers[dir1] = mgr1
	app.managers[dir2] = mgr2
	app.activeRepo = dir1

	app.cfg = &config.Config{
		Repos: []config.Repo{
			{Path: dir1, Name: "repo1"},
			{Path: dir2, Name: "repo2"},
		},
	}

	// Create an agent under repo1 (the first repo).
	sess1, _, err := mgr1.CreateSessionWithCommand(
		agent.Config{Name: "test-agent", Task: "test"},
		func(name string) *exec.Cmd { return exec.Command("bash", "-c", "sleep 999") },
	)
	if err != nil {
		t.Fatal(err)
	}

	app.refreshAgentList()
	// List should be: [repo1, session1, agent1, repo2]
	// Select the agent in repo1 (index 2).
	for i, it := range app.dashboard.items {
		if it.kind == listItemAgent && it.repoPath == dir1 {
			app.dashboard.selected = i
			break
		}
	}
	if app.dashboard.items[app.dashboard.selected].repoPath != dir1 {
		t.Fatalf("setup: expected selected item in repo1")
	}

	// Now kill the session, simulating what happens when 'X' is pressed.
	_ = mgr1.KillSession(sess1.ID)
	// Give the agent time to exit.
	time.Sleep(200 * time.Millisecond)
	// Refresh the list — list becomes [repo1, repo2].
	// Without the fix, selected=2 clamps to 1 which is repo2 (wrong repo).
	app.refreshAgentList()

	// After refresh, the cursor should still be on a repo1 item (the repo header).
	if len(app.dashboard.items) == 0 {
		t.Fatal("expected non-empty items after refresh")
	}
	selected := app.dashboard.items[app.dashboard.selected]
	if selected.repoPath != dir1 {
		t.Errorf("cursor jumped to repo %q, want %q (selected=%d, kind=%v)",
			selected.repoPath, dir1, app.dashboard.selected, selected.kind)
	}
	if selected.kind != listItemRepo {
		t.Errorf("expected cursor on repo header, got kind=%v", selected.kind)
	}
}
