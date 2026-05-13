package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFeedbackNoteModal_SubmitReturnsNote(t *testing.T) {
	m := newFeedbackNoteModal()
	m.SetSize(100, 40)
	_ = m.Open("comment:42", "old note")

	if !m.Active() {
		t.Fatal("expected modal to be active after Open")
	}
	if m.ta.Value() != "old note" {
		t.Errorf("textarea value = %q, want %q", m.ta.Value(), "old note")
	}

	// Pressing enter should submit.
	cmd, submitted, note := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !submitted {
		t.Error("expected submitted=true on enter")
	}
	if note != "old note" {
		t.Errorf("note = %q, want %q", note, "old note")
	}
	if m.Active() {
		t.Error("expected modal inactive after submit")
	}
	_ = cmd
}

func TestFeedbackNoteModal_EscCancels(t *testing.T) {
	m := newFeedbackNoteModal()
	m.SetSize(100, 40)
	_ = m.Open("comment:42", "existing")

	cmd, submitted, note := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if submitted {
		t.Error("expected submitted=false on esc")
	}
	if note != "" {
		t.Errorf("note = %q, want empty on cancel", note)
	}
	if m.Active() {
		t.Error("expected modal inactive after esc")
	}
	_ = cmd
}

func TestFeedbackNoteModal_InactiveNoop(t *testing.T) {
	m := newFeedbackNoteModal()
	cmd, submitted, note := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if submitted || note != "" || cmd != nil {
		t.Errorf("inactive modal should be a noop; got submitted=%v note=%q cmd=%v", submitted, note, cmd)
	}
}
