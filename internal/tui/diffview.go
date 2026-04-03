package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	git "github.com/devenjarvis/baton/internal/git"
)

const leftPanelWidth = 28

// diffModel displays a split-pane diff browser.
// Left panel: file list. Right panel: per-file diff.
type diffModel struct {
	agentName   string
	files       []git.DiffFile
	selected    int // index of selected file in left panel
	rightOffset int // scroll offset for right panel
	width       int
	height      int
}

func newDiffModel(agentName string, files []git.DiffFile, width, height int) diffModel {
	return diffModel{
		agentName: agentName,
		files:     files,
		width:     width,
		height:    height,
	}
}

func (d diffModel) Update(msg tea.Msg) (diffModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc":
			return d, func() tea.Msg { return diffCloseMsg{} }
		case "j", "down":
			if d.selected < len(d.files)-1 {
				d.selected++
				d.rightOffset = 0
			}
		case "k", "up":
			if d.selected > 0 {
				d.selected--
				d.rightOffset = 0
			}
		case "d":
			// Page-scroll right panel down.
			d.rightOffset += d.viewHeight() / 2
			if max := d.maxRightOffset(); d.rightOffset > max {
				d.rightOffset = max
			}
		case "u":
			// Page-scroll right panel up.
			d.rightOffset -= d.viewHeight() / 2
			if d.rightOffset < 0 {
				d.rightOffset = 0
			}
		}
	}
	return d, nil
}

type diffCloseMsg struct{}

// viewHeight is the usable content height (total minus title row).
// d.height is already set to terminal height minus the statusbar row.
func (d diffModel) viewHeight() int {
	h := d.height - 1 // minus title row
	if h < 1 {
		return 1
	}
	return h
}

// maxRightOffset returns the maximum scroll offset for the right panel.
func (d diffModel) maxRightOffset() int {
	if len(d.files) == 0 {
		return 0
	}
	lines := d.files[d.selected].Lines
	max := len(lines) - d.viewHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (d diffModel) View() string {
	// ── Title bar ──────────────────────────────────────────────────────────
	fileCount := fmt.Sprintf("%d file", len(d.files))
	if len(d.files) != 1 {
		fileCount += "s"
	}
	title := StyleTitle.Render(d.agentName) + "  " + StyleSubtle.Render(fileCount)

	// ── Left panel: file list ───────────────────────────────────────────────
	addStatusStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStatusStyle := lipgloss.NewStyle().Foreground(ColorError)
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)

	maxPathLen := leftPanelWidth - 2 // "A " prefix is 2 chars (letter + space)

	vh := d.viewHeight()
	var leftLines []string
	for i, f := range d.files {
		// Status letter with color.
		var statusStr string
		switch f.Status {
		case "A":
			statusStr = addStatusStyle.Render("A")
		case "D":
			statusStr = delStatusStyle.Render("D")
		default:
			statusStr = "M"
		}

		// Truncate path to fit.
		path := f.Path
		if len(path) > maxPathLen {
			path = "…" + path[len(path)-maxPathLen+1:]
		}

		// Render path separately to preserve status letter color on selected row.
		if i == d.selected {
			entry := statusStr + " " + selectedStyle.Render(path)
			leftLines = append(leftLines, entry)
		} else {
			entry := statusStr + " " + normalStyle.Render(path)
			leftLines = append(leftLines, entry)
		}
	}

	// Pad left panel to fill viewHeight.
	for len(leftLines) < vh {
		leftLines = append(leftLines, "")
	}

	leftPanel := lipgloss.NewStyle().
		Width(leftPanelWidth).
		Height(vh).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorMuted).
		Render(strings.Join(leftLines, "\n"))

	// ── Right panel: diff of selected file ─────────────────────────────────
	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	hunkStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText)

	var diffLines []string
	if len(d.files) > 0 {
		diffLines = d.files[d.selected].Lines
	}

	end := d.rightOffset + vh
	if end > len(diffLines) {
		end = len(diffLines)
	}
	start := d.rightOffset
	if start > len(diffLines) {
		start = len(diffLines)
	}

	var rendered []string
	for _, line := range diffLines[start:end] {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			rendered = append(rendered, headerStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			rendered = append(rendered, hunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			rendered = append(rendered, addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			rendered = append(rendered, delStyle.Render(line))
		case strings.HasPrefix(line, "diff "):
			rendered = append(rendered, headerStyle.Render(line))
		default:
			rendered = append(rendered, line)
		}
	}

	rightWidth := d.width - leftPanelWidth - 1 // -1 for border
	if rightWidth < 1 {
		rightWidth = 1
	}

	rightPanel := lipgloss.NewStyle().
		Width(rightWidth).
		Height(vh).
		Render(strings.Join(rendered, "\n"))

	// ── Assemble ────────────────────────────────────────────────────────────
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	return title + "\n" + body
}
