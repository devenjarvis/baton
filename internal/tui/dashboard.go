package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	xvt "github.com/charmbracelet/x/vt"
	"github.com/devenjarvis/baton/internal/agent"
)

// listItemKind distinguishes repo headers, session rows, and agent rows in the dashboard list.
type listItemKind int

const (
	listItemRepo listItemKind = iota
	listItemSession
	listItemAgent
)

// listItem represents one row in the hierarchical dashboard list.
type listItem struct {
	kind     listItemKind
	repoPath string
	repoName string          // set for repo header items
	session  *agent.Session  // set for session and agent items
	agent    *agent.Agent    // set for agent items
}

// dashboardModel shows the hierarchical repo/session/agent list and terminal preview.
type dashboardModel struct {
	items        []listItem
	selected     int
	width        int
	height       int
	panelFocus   panelFocus
	scrollOffset int
}

func newDashboardModel() dashboardModel {
	return dashboardModel{}
}

func (d dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if d.panelFocus == focusTerminal {
			ag := d.selectedAgent()
			switch msg.String() {
			case "ctrl+e", "esc":
				d.panelFocus = focusList
				d.scrollOffset = 0
			case "shift+esc":
				if ag != nil {
					ag.SendKey(xvt.KeyPressEvent{Code: tea.KeyEscape})
				}
			case "enter":
				if ag != nil {
					ag.SendKey(xvt.KeyPressEvent(msg))
				}
			case "pgup":
				if ag != nil {
					sbLen := len(ag.ScrollbackLines())
					vpHeight := d.previewTermHeight()
					step := vpHeight / 2
					if step < 1 {
						step = 1
					}
					d.scrollOffset += step
					maxOffset := sbLen + vpHeight - vpHeight
					if maxOffset < 0 {
						maxOffset = 0
					}
					if d.scrollOffset > maxOffset {
						d.scrollOffset = maxOffset
					}
				}
			case "pgdown":
				step := d.previewTermHeight() / 2
				if step < 1 {
					step = 1
				}
				d.scrollOffset -= step
				if d.scrollOffset < 0 {
					d.scrollOffset = 0
				}
			case "home":
				d.scrollOffset = 0
			default:
				if ag != nil {
					if msg.Text != "" {
						ag.SendText(msg.Text)
					} else {
						ag.SendKey(xvt.KeyPressEvent(msg))
					}
				}
			}
			return d, nil
		}

		// focusList mode
		switch msg.String() {
		case "j", "down":
			for next := d.selected + 1; next < len(d.items); next++ {
				if d.items[next].kind != listItemSession {
					d.selected = next
					d.scrollOffset = 0
					break
				}
			}
		case "k", "up":
			for next := d.selected - 1; next >= 0; next-- {
				if d.items[next].kind != listItemSession {
					d.selected = next
					d.scrollOffset = 0
					break
				}
			}
		case "right", "enter":
			if d.selectedAgent() != nil {
				d.panelFocus = focusTerminal
			}
		}
	}
	return d, nil
}

func (d dashboardModel) View() string {
	if len(d.items) == 0 {
		return d.emptyView()
	}

	listWidth := 30
	previewWidth := d.previewTermWidth()

	list := d.renderList(listWidth)
	preview := d.renderPreview(previewWidth)

	listStyle := lipgloss.NewStyle().
		Width(listWidth).
		Height(d.contentHeight()).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(ColorMuted)

	previewStyle := lipgloss.NewStyle().
		Width(previewWidth).
		Height(d.contentHeight())
	if d.panelFocus == focusTerminal {
		previewStyle = lipgloss.NewStyle().
			Width(previewWidth).
			Height(d.contentHeight() - 2).
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorSecondary)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(list),
		previewStyle.Render(preview),
	)
}

func (d dashboardModel) contentHeight() int {
	return d.height - 2 // statusbar + title
}

// previewTermWidth returns the terminal column count for the preview panel.
func (d dashboardModel) previewTermWidth() int {
	listWidth := 30
	w := d.width - listWidth - 1 // 1 for the list panel's right border
	if d.panelFocus == focusTerminal {
		w -= 2 // NormalBorder consumes 1 col each side
	}
	return w
}

// previewTermHeight returns the terminal row count for the preview panel.
func (d dashboardModel) previewTermHeight() int {
	h := d.contentHeight() - 4 // title + session info + task info + blank line
	if d.panelFocus == focusTerminal {
		h -= 2 // NormalBorder consumes 1 row top and bottom
	}
	return h
}

func (d dashboardModel) emptyView() string {
	title := StyleTitle.Render("Baton")
	subtitle := StyleSubtle.Render("No agents running")
	hint := StyleSubtle.Render("Press n to create a new session")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", subtitle, "", hint)
	return lipgloss.Place(d.width, d.contentHeight(), lipgloss.Center, lipgloss.Center, content)
}

func (d dashboardModel) renderList(width int) string {
	title := StyleTitle.Render("AGENTS")
	sepWidth := width - 2
	if sepWidth < 0 {
		sepWidth = 0
	}
	separator := StyleSubtle.Render(strings.Repeat("─", sepWidth))

	var lines []string
	lines = append(lines, title, separator)

	for i, item := range d.items {
		isSelected := i == d.selected
		prefix := "  "
		if isSelected {
			prefix = StyleActive.Render("▸ ")
		}

		switch item.kind {
		case listItemRepo:
			// Repo header row.
			name := item.repoName
			maxLen := width - 4
			if len(name) > maxLen {
				name = name[:maxLen-1] + "…"
			}
			var repoStyle lipgloss.Style
			if isSelected {
				repoStyle = lipgloss.NewStyle().Foreground(ColorText).Bold(true)
			} else {
				repoStyle = lipgloss.NewStyle().Foreground(ColorMuted)
			}
			lines = append(lines, prefix+repoStyle.Render(name))

		case listItemSession:
			// Session separator header — not selectable.
			sess := item.session
			status := sess.Status()
			symbol := status.Symbol()
			count := fmt.Sprintf("%d agents", sess.AgentCount())

			var symbolStyle lipgloss.Style
			switch status {
			case agent.StatusActive:
				symbolStyle = lipgloss.NewStyle().Foreground(ColorSecondary)
			case agent.StatusDone:
				symbolStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
			case agent.StatusError:
				symbolStyle = lipgloss.NewStyle().Foreground(ColorError)
			default:
				symbolStyle = lipgloss.NewStyle().Foreground(ColorMuted)
			}

			label := fmt.Sprintf(" %s %s (%s) ", symbolStyle.Render(symbol), sess.Name, count)
			labelLen := len(symbol) + 1 + len(sess.Name) + len(count) + 5 // approximate visible length
			padLen := width - 4 - labelLen
			if padLen < 0 {
				padLen = 0
			}
			line := StyleSubtle.Render("  ──") + label + StyleSubtle.Render(strings.Repeat("─", padLen))
			lines = append(lines, line)

		case listItemAgent:
			// Agent row — indented under its session.
			ag := item.agent
			status := ag.Status()
			symbol := status.Symbol()
			elapsed := humanizeElapsed(ag.Elapsed())

			var style lipgloss.Style
			switch status {
			case agent.StatusActive:
				style = lipgloss.NewStyle().Foreground(ColorSecondary)
			case agent.StatusDone:
				style = lipgloss.NewStyle().Foreground(ColorSuccess)
			case agent.StatusError:
				style = lipgloss.NewStyle().Foreground(ColorError)
			case agent.StatusIdle:
				style = lipgloss.NewStyle().Foreground(ColorMuted)
			default:
				style = lipgloss.NewStyle().Foreground(ColorWarning)
			}

			nameWidth := width - 18 // space for indent, symbol, elapsed, padding
			name := ag.GetDisplayName()
			if len(name) > nameWidth {
				name = name[:nameWidth-1] + "…"
			}

			agentPrefix := "      "
			if isSelected {
				agentPrefix = "    " + StyleActive.Render("▸ ")
			}

			line := fmt.Sprintf("%s%s %-*s %5s",
				agentPrefix,
				style.Render(symbol),
				nameWidth,
				name,
				elapsed,
			)
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

func (d dashboardModel) renderPreview(width int) string {
	item := d.selectedItem()
	if item == nil {
		return lipgloss.Place(width, d.contentHeight(), lipgloss.Center, lipgloss.Center,
			StyleSubtle.Render("No agent selected"))
	}

	if item.kind == listItemRepo {
		// Show repo info in the preview panel when a repo header is selected.
		title := StyleTitle.Render(" " + item.repoName + " ")
		pathLine := StyleSubtle.Render(" " + item.repoPath)
		hint := StyleSubtle.Render(" Press n to create a session in this repo")
		return lipgloss.JoinVertical(lipgloss.Left, title, pathLine, "", hint)
	}

	// Agent selected — show terminal preview with session context.
	ag := item.agent
	titleText := " " + ag.GetDisplayName() + " "
	if d.scrollOffset > 0 {
		titleText = fmt.Sprintf(" %s [↑%d] ", ag.GetDisplayName(), d.scrollOffset)
	}
	title := StyleTitle.Render(titleText)

	sessionInfo := ""
	if item.session != nil {
		sessionInfo = StyleSubtle.Render(fmt.Sprintf(" Session: %s  Worktree: %s", item.session.Name, item.session.Worktree.Path))
	}
	taskInfo := StyleSubtle.Render(" Task: " + ag.Task)

	var render string
	if d.scrollOffset > 0 {
		sbLines := ag.ScrollbackLines()
		vpLines := strings.Split(ag.Render(), "\n")
		allLines := append(sbLines, vpLines...)

		vpHeight := d.previewTermHeight()
		end := len(allLines) - d.scrollOffset
		if end < 0 {
			end = 0
		}
		start := end - vpHeight
		if start < 0 {
			start = 0
		}
		render = strings.Join(allLines[start:end], "\n")
	} else {
		render = ag.Render()
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		sessionInfo,
		taskInfo,
		"",
		render,
	)
}

// selectedItem returns the currently selected list item, or nil if the list is empty.
func (d dashboardModel) selectedItem() *listItem {
	if d.selected < 0 || d.selected >= len(d.items) {
		return nil
	}
	return &d.items[d.selected]
}

// selectedAgent returns the currently selected agent, or nil if a repo/session header is selected.
func (d dashboardModel) selectedAgent() *agent.Agent {
	item := d.selectedItem()
	if item == nil || item.kind != listItemAgent {
		return nil
	}
	return item.agent
}

// selectedSession returns the session for the currently selected item.
// Works for both session and agent items.
func (d dashboardModel) selectedSession() *agent.Session {
	item := d.selectedItem()
	if item == nil {
		return nil
	}
	return item.session
}

// selectedRepoPath returns the repo path of the currently selected item.
func (d dashboardModel) selectedRepoPath() string {
	item := d.selectedItem()
	if item == nil {
		return ""
	}
	return item.repoPath
}

// clampToAgent adjusts selected to the nearest non-session row.
// Searches forward first, then backward. Falls through to repo rows if no agents exist.
func (d *dashboardModel) clampToAgent() {
	if len(d.items) == 0 {
		return
	}
	if d.selected >= len(d.items) {
		d.selected = len(d.items) - 1
	}
	if d.selected < 0 {
		d.selected = 0
	}
	if d.items[d.selected].kind != listItemSession {
		return
	}
	// Search forward for an agent or repo.
	for i := d.selected + 1; i < len(d.items); i++ {
		if d.items[i].kind != listItemSession {
			d.selected = i
			return
		}
	}
	// Search backward.
	for i := d.selected - 1; i >= 0; i-- {
		if d.items[i].kind != listItemSession {
			d.selected = i
			return
		}
	}
}

// agentItems returns all agent items from the list (for resize operations).
func (d dashboardModel) agentItems() []*agent.Agent {
	var result []*agent.Agent
	for _, item := range d.items {
		if item.kind == listItemAgent {
			result = append(result, item.agent)
		}
	}
	return result
}

// humanizeElapsed formats a duration for display.
func humanizeElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
