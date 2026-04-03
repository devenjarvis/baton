package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewPromptModel_DefaultBypassTrue(t *testing.T) {
	p := newPromptModel(true)
	if !p.bypassPerms {
		t.Error("newPromptModel(true) should set bypassPerms=true")
	}
}

func TestNewPromptModel_DefaultBypassFalse(t *testing.T) {
	p := newPromptModel(false)
	if p.bypassPerms {
		t.Error("newPromptModel(false) should set bypassPerms=false")
	}
}

func TestPromptModel_TabCyclesThreeFields(t *testing.T) {
	p := newPromptModel(true)
	if p.focused != 0 {
		t.Fatalf("expected focused=0 initially, got %d", p.focused)
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.focused != 1 {
		t.Errorf("after tab 1: expected focused=1, got %d", p.focused)
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.focused != 2 {
		t.Errorf("after tab 2: expected focused=2, got %d", p.focused)
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.focused != 0 {
		t.Errorf("after tab 3 (wrap): expected focused=0, got %d", p.focused)
	}
}

func TestPromptModel_ShiftTabCyclesBackward(t *testing.T) {
	p := newPromptModel(true)
	// Start at 0, shift+tab should go to 2
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if p.focused != 2 {
		t.Errorf("shift+tab from 0: expected focused=2, got %d", p.focused)
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if p.focused != 1 {
		t.Errorf("shift+tab from 2: expected focused=1, got %d", p.focused)
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if p.focused != 0 {
		t.Errorf("shift+tab from 1: expected focused=0, got %d", p.focused)
	}
}

func TestPromptModel_SpaceTogglesWhenFocusedOnBypass(t *testing.T) {
	p := newPromptModel(true)
	// Tab twice to reach bypass field (focused=2)
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if p.focused != 2 {
		t.Fatalf("expected focused=2, got %d", p.focused)
	}
	if !p.bypassPerms {
		t.Fatal("expected bypassPerms=true initially")
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if p.bypassPerms {
		t.Error("space should toggle bypassPerms to false")
	}

	p, _ = p.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !p.bypassPerms {
		t.Error("space should toggle bypassPerms back to true")
	}
}

func TestPromptModel_SpaceDoesNotToggleWhenFocusedOnName(t *testing.T) {
	p := newPromptModel(true)
	// focused=0 (name field)
	p, _ = p.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	// Space in name field should append to name, not toggle bypass
	if !p.bypassPerms {
		t.Error("space in name field should not toggle bypassPerms")
	}
	if p.name != " " {
		t.Errorf("space in name field should append to name, got %q", p.name)
	}
}

func TestPromptModel_EnterSubmitsWithBypassValue(t *testing.T) {
	p := newPromptModel(false)
	// Type name
	for _, ch := range "myagent" {
		p, _ = p.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	// Tab to task
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	// Type task
	for _, ch := range "do work" {
		p, _ = p.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	// Tab to bypass (focused=2)
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	// Toggle bypass on
	p, _ = p.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if !p.bypassPerms {
		t.Fatal("expected bypassPerms=true after toggle")
	}

	// Enter should submit
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter with valid name+task should produce a cmd")
	}
	msg := cmd()
	result, ok := msg.(promptResult)
	if !ok {
		t.Fatalf("expected promptResult, got %T", msg)
	}
	if result.name != "myagent" {
		t.Errorf("result.name = %q, want myagent", result.name)
	}
	if result.task != "do work" {
		t.Errorf("result.task = %q, want do work", result.task)
	}
	if !result.bypassPerms {
		t.Error("result.bypassPerms should be true")
	}
}

func TestPromptModel_ViewShowsCheckbox(t *testing.T) {
	p := newPromptModel(true)
	p.width = 80
	p.height = 24
	view := p.View()

	if !strings.Contains(view, "Bypass permissions") {
		t.Error("View() should contain 'Bypass permissions'")
	}
	if !strings.Contains(view, "[x]") {
		t.Error("View() should show [x] when bypassPerms=true")
	}

	// Uncheck
	p.bypassPerms = false
	view = p.View()
	if !strings.Contains(view, "[ ]") {
		t.Error("View() should show [ ] when bypassPerms=false")
	}
}
