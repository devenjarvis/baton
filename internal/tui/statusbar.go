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
		{"p", "open PR"},
		{"click PR#", "open"},
		{"f", "focus mode"},
		{"s", "settings"},
		{"a", "add repo"},
		{"o", "open branch"},
		{"d", "diff/remove"},
		{"x", "kill agent"},
		{"X", "kill session"},
		{"q", "quit"},
	}

	focusTerminalHints = []keyHint{
		{"enter", "send"},
		{"pgup/pgdn", "scroll"},
		{"home", "live"},
		{"esc", "back"},
		{"⇧esc", "interrupt"},
	}

	diffHints = []keyHint{
		{"j/k", "tree"},
		{"h/l", "fold/open"},
		{"enter", "view"},
		{"d/u", "scroll"},
		{"s", "side-by-side"},
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
		{"←/→", "select"},
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

	focusModeHints = []keyHint{
		{"m", "mark ready"},
		{"r", "review"},
		{"n", "new"},
		{"N", "change repo"},
		{"space", "interact"},
		{"f", "exit focus"},
	}

	focusLaunchHints = []keyHint{
		{"f/esc", "back to focus"},
		{"enter", "send"},
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
