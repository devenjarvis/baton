package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorPrimary   = lipgloss.Color("#7C3AED")
	ColorSecondary = lipgloss.Color("#06B6D4")
	ColorSuccess   = lipgloss.Color("#10B981")
	ColorWarning   = lipgloss.Color("#F59E0B")
	ColorError     = lipgloss.Color("#EF4444")
	ColorMuted     = lipgloss.Color("#6B7280")
	ColorText      = lipgloss.Color("#F9FAFB")
	ColorBg        = lipgloss.Color("#111827")

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	StyleSubtle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorError)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleActive = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	StyleLink = lipgloss.NewStyle().
			Underline(true).
			Foreground(ColorSecondary)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(ColorText).
			Background(lipgloss.Color("#1F2937")).
			Padding(0, 1)
)
