package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// diffModel displays a scrollable colored diff.
type diffModel struct {
	lines  []string
	offset int
	width  int
	height int
}

func newDiffModel(rawDiff string, width, height int) diffModel {
	lines := strings.Split(rawDiff, "\n")
	return diffModel{
		lines:  lines,
		width:  width,
		height: height,
	}
}

func (d diffModel) Update(msg tea.Msg) (diffModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc":
			return d, func() tea.Msg { return diffCloseMsg{} }
		case "j", "down":
			if d.offset < d.maxOffset() {
				d.offset++
			}
		case "k", "up":
			if d.offset > 0 {
				d.offset--
			}
		case "d":
			// Page down.
			d.offset += d.viewHeight() / 2
			if d.offset > d.maxOffset() {
				d.offset = d.maxOffset()
			}
		case "u":
			// Page up.
			d.offset -= d.viewHeight() / 2
			if d.offset < 0 {
				d.offset = 0
			}
		}
	}
	return d, nil
}

type diffCloseMsg struct{}

func (d diffModel) viewHeight() int {
	return d.height - 3 // title + statusbar + border
}

func (d diffModel) maxOffset() int {
	max := len(d.lines) - d.viewHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (d diffModel) View() string {
	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	hunkStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText)

	vh := d.viewHeight()
	end := d.offset + vh
	if end > len(d.lines) {
		end = len(d.lines)
	}

	var rendered []string
	for _, line := range d.lines[d.offset:end] {
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

	title := StyleTitle.Render("Diff View")
	scrollInfo := StyleSubtle.Render(
		strings.Repeat(" ", 2) + "Lines " +
			strconv.Itoa(d.offset+1) + "-" + strconv.Itoa(end) +
			" of " + strconv.Itoa(len(d.lines)))

	header := title + "  " + scrollInfo
	body := strings.Join(rendered, "\n")

	return header + "\n" + body
}


