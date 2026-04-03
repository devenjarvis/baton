package tui

import (
	"os"
	"os/exec"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/devenjarvis/baton/internal/agent"
)

func createAgentViaPrompt(t *testing.T, app App, name, task string) App {
	t.Helper()

	// Press "n" to open prompt
	model, _ := app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	app = model.(App)

	if app.view != ViewPrompt {
		t.Fatalf("Expected ViewPrompt after 'n', got %v", app.view)
	}

	// Type name
	for _, ch := range name {
		model, _ = app.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		app = model.(App)
	}

	// Tab to task
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	app = model.(App)

	// Type task
	for _, ch := range task {
		model, _ = app.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		app = model.(App)
	}

	// Enter — produces promptResult cmd
	model, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app = model.(App)

	if cmd == nil {
		t.Fatal("Expected cmd from Enter, got nil")
	}

	// Execute promptResult cmd
	msg := cmd()
	model, cmd = app.Update(msg)
	app = model.(App)

	// If there's a follow-up cmd (createResultMsg from async creation), execute it
	if cmd != nil {
		msg = cmd()
		model, _ = app.Update(msg)
		app = model.(App)
	}

	return app
}

func TestPromptCreatesAgent(t *testing.T) {
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

	t.Logf("Initial view: %v, manager: %v", app.view, app.managers[dir] != nil)

	// Create agent via prompt
	app = createAgentViaPrompt(t, app, "test1", "do stuff")

	t.Logf("After creation: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

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

	// Create first agent
	t.Log("=== Creating agent 1 ===")
	app = createAgentViaPrompt(t, app, "agent1", "task one")
	t.Logf("After agent1: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Agent 1 error: %s", app.err)
	}
	if mgr.AgentCount() != 1 {
		t.Fatalf("Expected 1 agent, got %d", mgr.AgentCount())
	}

	// Create second agent
	t.Log("=== Creating agent 2 ===")
	app = createAgentViaPrompt(t, app, "agent2", "task two")
	t.Logf("After agent2: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Agent 2 error: %s", app.err)
	}
	if mgr.AgentCount() != 2 {
		t.Fatalf("Expected 2 agents, got %d", mgr.AgentCount())
	}
	if len(app.dashboard.agentItems()) != 2 {
		t.Fatalf("Expected 2 dashboard agents, got %d", len(app.dashboard.agentItems()))
	}

	// Create third agent
	t.Log("=== Creating agent 3 ===")
	app = createAgentViaPrompt(t, app, "agent3", "task three")
	t.Logf("After agent3: view=%v, err=%q, agents=%d, dashboard=%d",
		app.view, app.err, mgr.AgentCount(), len(app.dashboard.agentItems()))

	if app.err != "" {
		t.Fatalf("Agent 3 error: %s", app.err)
	}
	if mgr.AgentCount() != 3 {
		t.Fatalf("Expected 3 agents, got %d", mgr.AgentCount())
	}
	if len(app.dashboard.agentItems()) != 3 {
		t.Fatalf("Expected 3 dashboard agents, got %d", len(app.dashboard.agentItems()))
	}

	t.Logf("SUCCESS: Created %d agents", len(app.dashboard.agentItems()))
	for _, a := range app.dashboard.agentItems() {
		t.Logf("  %s: status=%v", a.Name, a.Status())
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

	app = createAgentViaPrompt(t, app, "focus-test", "do stuff")
	if len(app.dashboard.agentItems()) == 0 {
		t.Fatal("Expected at least one agent")
	}

	// Initially in focusList
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList initially, got %v", app.dashboard.panelFocus)
	}

	// Right arrow enters focusTerminal
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after →, got %v", app.dashboard.panelFocus)
	}

	// Esc returns to focusList
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	app = model.(App)
	if app.dashboard.panelFocus != focusList {
		t.Fatalf("Expected focusList after esc, got %v", app.dashboard.panelFocus)
	}

	// Right arrow again, then enter stays in focusTerminal (enter forwards to agent, esc exits)
	model, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	app = model.(App)
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

	app = createAgentViaPrompt(t, app, "block-test", "do stuff")

	// Enter focusTerminal
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	app = model.(App)
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal after →")
	}

	// Press "n" — should be forwarded to agent, NOT open prompt overlay
	// panelFocus must stay focusTerminal (n is not enter/esc)
	model, _ = app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	app = model.(App)
	if app.view != ViewDashboard {
		t.Fatalf("Expected ViewDashboard (n forwarded to agent, not prompt), got %v", app.view)
	}
	if app.dashboard.panelFocus != focusTerminal {
		t.Fatalf("Expected focusTerminal to persist after 'n', got %v", app.dashboard.panelFocus)
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
