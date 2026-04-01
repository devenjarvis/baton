package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/devenjarvis/baton/internal/git"
)

// fileBrowserSelectMsg is emitted when the user selects a git repo directory.
type fileBrowserSelectMsg struct{ path string }

// fileBrowserCancelMsg is emitted when the user cancels the file browser.
type fileBrowserCancelMsg struct{}

// fileBrowserModel is a sub-component for browsing and selecting git repo directories.
type fileBrowserModel struct {
	currentDir string
	entries    []os.DirEntry // only dirs, no hidden
	selected   int
	width      int
	height     int
}

// newFileBrowserModel creates a fileBrowserModel starting at the user's home directory.
func newFileBrowserModel() fileBrowserModel {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	m := fileBrowserModel{
		currentDir: home,
	}
	m.entries = loadEntries(home)
	return m
}

// loadEntries reads subdirectories (no files, no hidden dirs) from dir.
func loadEntries(dir string) []os.DirEntry {
	all, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var entries []os.DirEntry
	for _, e := range all {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// Update handles key events for the file browser.
func (m fileBrowserModel) Update(msg tea.Msg) (fileBrowserModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.selected < len(m.entries)-1 {
				m.selected++
			}
		case "k", "up":
			if m.selected > 0 {
				m.selected--
			}
		case "enter":
			if len(m.entries) == 0 {
				break
			}
			entry := m.entries[m.selected]
			path := m.currentDir + string(os.PathSeparator) + entry.Name()
			if git.IsRepo(path) {
				return m, func() tea.Msg { return fileBrowserSelectMsg{path: path} }
			}
			// Descend into the directory.
			m.currentDir = path
			m.entries = loadEntries(path)
			m.selected = 0
		case "backspace":
			parent := parentDir(m.currentDir)
			if parent != m.currentDir {
				m.currentDir = parent
				m.entries = loadEntries(parent)
				m.selected = 0
			}
		case "esc":
			return m, func() tea.Msg { return fileBrowserCancelMsg{} }
		}
	}
	return m, nil
}

// View renders the two-panel file browser.
func (m fileBrowserModel) View() string {
	leftWidth := m.width / 3
	if leftWidth < 20 {
		leftWidth = 20
	}
	rightWidth := m.width - leftWidth - 1 // 1 for border

	left := m.renderDirList(leftWidth)
	right := m.renderDetails(rightWidth)

	leftStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Height(m.height).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(ColorMuted)

	rightStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Height(m.height)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(left),
		rightStyle.Render(right),
	)
}

// renderDirList renders the left panel with the directory listing.
func (m fileBrowserModel) renderDirList(width int) string {
	title := StyleTitle.Render("DIRECTORIES")
	separator := StyleSubtle.Render(strings.Repeat("─", width-2))

	var lines []string
	lines = append(lines, title, separator)
	lines = append(lines, StyleSubtle.Render(m.currentDir))
	lines = append(lines, "")

	if len(m.entries) == 0 {
		lines = append(lines, StyleSubtle.Render("  (empty)"))
	}

	for i, e := range m.entries {
		prefix := "  "
		if i == m.selected {
			prefix = StyleActive.Render("▸ ")
		}

		name := e.Name()
		maxLen := width - 4
		if len(name) > maxLen {
			name = name[:maxLen-1] + "…"
		}

		lines = append(lines, prefix+name)
	}

	return strings.Join(lines, "\n")
}

// renderDetails renders the right panel with info about the selected directory.
func (m fileBrowserModel) renderDetails(width int) string {
	title := StyleTitle.Render("DETAILS")
	separator := StyleSubtle.Render(strings.Repeat("─", width-1))

	var lines []string
	lines = append(lines, title, separator)

	if len(m.entries) == 0 {
		lines = append(lines, StyleSubtle.Render("No subdirectories"))
		return strings.Join(lines, "\n")
	}

	entry := m.entries[m.selected]
	path := m.currentDir + string(os.PathSeparator) + entry.Name()

	lines = append(lines, "")
	lines = append(lines, StyleTitle.Render(entry.Name()))
	lines = append(lines, StyleSubtle.Render(path))
	lines = append(lines, "")

	if git.IsRepo(path) {
		branch, err := git.BaseBranch(path)
		if err != nil {
			branch = "(unknown)"
		}
		lines = append(lines, StyleSuccess.Render("git repo"))
		lines = append(lines, StyleSubtle.Render("branch: ")+branch)
		lines = append(lines, "")
		lines = append(lines, StyleSubtle.Render("Press enter to select"))
	} else {
		lines = append(lines, StyleSubtle.Render("not a git repo"))
		lines = append(lines, "")
		lines = append(lines, StyleSubtle.Render("Press enter to open"))
	}

	return strings.Join(lines, "\n")
}

// parentDir returns the parent directory of dir, or dir if already at root.
func parentDir(dir string) string {
	if dir == "" || dir == "/" || dir == "." {
		return dir
	}
	// Walk back to the last separator.
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == os.PathSeparator {
			return dir[:i]
		}
	}
	return dir
}
