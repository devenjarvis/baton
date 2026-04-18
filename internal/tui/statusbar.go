package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type keyHint struct {
	key  string
	desc string
}

var (
	dashboardHints = []keyHint{
		{"j/k", "navigate"},
		{"⏎/→", "interact"},
		{"n", "new session"},
		{"c", "add agent"},
		{"t", "shell"},
		{"e", "open in IDE"},
		{"s", "settings"},
		{"a", "add repo"},
		{"o", "open branch"},
		{"d", "diff/remove"},
		{"x", "kill agent"},
		{"X", "kill session"},
		{"f", "fix checks"},
		{"q", "quit"},
	}

	focusTerminalHints = []keyHint{
		{"enter", "send"},
		{"drag", "select text"},
		{"pgup/pgdn", "scroll"},
		{"home", "live"},
		{"esc", "back"},
		{"⇧esc", "interrupt"},
	}

	diffSummaryHints = []keyHint{
		{"j/k", "files"},
		{"enter", "open"},
		{"g/G", "top/bot"},
		{"q", "back"},
	}

	diffDetailHints = []keyHint{
		{"j/k", "scroll"},
		{"d/u", "page"},
		{"n/p", "file"},
		{"esc", "summary"},
		{"q", "back"},
	}

	repoBrowsingHints = []keyHint{
		{"j/k", "navigate"},
		{"type", "filter"},
		{"enter", "open/select"},
		{"backspace", "up"},
		{".", "hidden"},
		{"esc", "cancel"},
	}

	repoConfigHints = []keyHint{
		{"j/k", "navigate"},
		{"enter", "edit/toggle"},
		{"ctrl+s", "save"},
		{"esc", "back"},
	}

	branchPickerHints = []keyHint{
		{"j/k", "navigate"},
		{"enter", "select"},
		{"type", "filter"},
		{"backspace", "clear filter"},
		{"esc", "cancel"},
	}
)

func renderStatusBar(hints []keyHint, width int) string {
	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, keyStyle.Render(h.key)+" "+descStyle.Render(h.desc))
	}

	content := strings.Join(parts, "  ")
	return StyleStatusBar.Width(width).Render(content)
}
