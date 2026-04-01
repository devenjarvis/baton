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
		{"→", "interact"},
		{"n", "new"},
		{"d", "diff"},
		{"x", "kill"},
		{"m", "merge"},
		{"q", "quit"},
	}

	focusTerminalHints = []keyHint{
		{"enter", "send"},
		{"esc", "back"},
	}

	diffHints = []keyHint{
		{"j/k", "scroll"},
		{"q/esc", "back"},
	}

	reposHints = []keyHint{
		{"j/k", "navigate"},
		{"enter", "select"},
		{"a", "add"},
		{"d", "remove"},
		{"r/esc", "back"},
		{"q", "quit"},
	}

	repoBrowsingHints = []keyHint{
		{"j/k", "navigate"},
		{"enter", "open/select"},
		{"backspace", "up"},
		{"esc", "cancel"},
	}
)

func renderStatusBar(hints []keyHint, width int) string {
	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorText)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	var parts []string
	for _, h := range hints {
		parts = append(parts, keyStyle.Render(h.key)+" "+descStyle.Render(h.desc))
	}

	content := strings.Join(parts, "  ")
	return StyleStatusBar.Width(width).Render(content)
}
