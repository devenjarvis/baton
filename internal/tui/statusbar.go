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
		{"s", "settings"},
		{"a", "add repo"},
		{"o", "open branch"},
		{"d", "diff/remove"},
		{"x", "kill agent"},
		{"X", "kill session"},
		{"p", "PR"},
		{"f", "fix checks"},
		{"m", "merge"},
		{"q", "detach"},
		{"Q", "quit"},
	}

	focusTerminalHints = []keyHint{
		{"enter", "send"},
		{"pgup/pgdn", "scroll"},
		{"home", "live"},
		{"esc", "back"},
		{"⇧esc", "interrupt"},
	}

	diffHints = []keyHint{
		{"j/k", "files"},
		{"d/u", "scroll diff"},
		{"q/esc", "back"},
	}

	repoBrowsingHints = []keyHint{
		{"j/k", "navigate"},
		{"enter", "open/select"},
		{"backspace", "up"},
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
