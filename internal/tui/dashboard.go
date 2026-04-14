package tui

import (
	"fmt"
	"path/filepath"
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
	repoName string         // set for repo header items
	session  *agent.Session // set for session and agent items
	agent    *agent.Agent   // set for agent items
}

// diffSummaryData holds cached diff stats for rendering in the dashboard.
type diffSummaryData struct {
	Files     []diffFileStat
	Aggregate diffAggregateStats
}

type diffFileStat struct {
	Path       string
	Status     string // "A", "M", or "D"
	Insertions int
	Deletions  int
}

type diffAggregateStats struct {
	Files      int
	Insertions int
	Deletions  int
}

// dashboardModel shows the hierarchical repo/session/agent list and terminal preview.
type dashboardModel struct {
	items        []listItem
	selected     int
	width        int
	height       int
	panelFocus   panelFocus
	scrollOffset int
	diffStats    *diffSummaryData         // nil when no session selected or no data
	prCache      map[string]*prCacheEntry // keyed by session ID, passed from App

	// Repo config form shown in the right panel when focusConfig is active.
	repoConfigForm *configForm
	configRepoPath string // path of the repo being configured
}

func newDashboardModel() dashboardModel {
	return dashboardModel{}
}

func (d dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if d.panelFocus == focusTerminal {
			if ag := d.selectedAgent(); ag != nil {
				ag.Paste(msg.Content)
			}
		}
		return d, nil
	case tea.KeyPressMsg:
		// Config panel focus: delegate to form.
		if d.panelFocus == focusConfig && d.repoConfigForm != nil {
			cmd := d.repoConfigForm.Update(msg)
			return d, cmd
		}

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
			// Repo config entry is handled at the app level (updateDashboard)
			// so it has access to settings.
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
	if d.panelFocus == focusTerminal || d.panelFocus == focusConfig {
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

// fixedTermWidth returns the terminal column count that all agents should use.
// This is always the focusTerminal width (deducting the border) regardless of
// the current panelFocus, so that focus switches never trigger a resize.
func (d dashboardModel) fixedTermWidth() int {
	listWidth := 30
	return d.width - listWidth - 1 - 2 // list border + focusTerminal border
}

// fixedTermHeight returns the terminal row count that all agents should use.
// This is always the focusTerminal height (deducting the border) regardless of
// the current panelFocus. It intentionally does NOT deduct the PR line —
// accepting 1 row of clipping when PR is visible is better than per-session
// resize churn.
func (d dashboardModel) fixedTermHeight() int {
	return d.contentHeight() - 4 - 2 // metadata rows + focusTerminal border
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
	// Account for PR info line if present.
	if sess := d.selectedSession(); sess != nil {
		if entry := d.prCache[sess.ID]; entry != nil && entry.pr != nil {
			h-- // extra line for PR info
		}
	}
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

			displayName := sess.GetDisplayName()

			// Build PR indicator suffix for the session row.
			var prSuffix string
			var prSuffixLen int
			if entry := d.prCache[sess.ID]; entry != nil && entry.pr != nil {
				prSuffix = " " + prIndicator(entry.pr, entry.checks)
				// Approximate visible length: space + #N + space + symbol
				prSuffixLen = 1 + len(fmt.Sprintf("#%d", entry.pr.Number))
				if entry.checks != nil {
					prSuffixLen += 2 // space + check symbol
				}
			}

			// 4 for "  ──", 3 for " symbol ", 1 trailing space, plus some padding chars
			maxNameLen := width - 10 - prSuffixLen
			if maxNameLen < 5 {
				maxNameLen = 5
			}
			if len(displayName) > maxNameLen {
				displayName = displayName[:maxNameLen-1] + "…"
			}

			label := fmt.Sprintf(" %s %s ", symbolStyle.Render(symbol), displayName)
			labelLen := len(symbol) + 1 + len(displayName) + 2 + prSuffixLen
			padLen := width - 4 - labelLen
			if padLen < 0 {
				padLen = 0
			}
			line := StyleSubtle.Render("  ──") + label + prSuffix + StyleSubtle.Render(strings.Repeat("─", padLen))
			lines = append(lines, line)

		case listItemAgent:
			// Agent row — indented under its session.
			ag := item.agent
			status := ag.Status()
			symbol := status.Symbol()
			elapsed := humanizeElapsed(ag.Elapsed())

			if ag.IsShell {
				symbol = "$"
			}

			var style lipgloss.Style
			if ag.IsShell {
				style = lipgloss.NewStyle().Foreground(ColorMuted)
			} else {
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

	// Render PR summary and diff stats at the bottom of the left panel.
	var bottomLines []string
	if sess := d.selectedSession(); sess != nil {
		if entry := d.prCache[sess.ID]; entry != nil && entry.pr != nil {
			bottomLines = append(bottomLines, d.renderPRSummary(entry, width))
		}
	}
	if d.diffStats != nil {
		contentH := d.contentHeight()
		agentListHeight := len(lines) + len(bottomLines)
		maxDiffHeight := contentH / 3
		availHeight := contentH - agentListHeight
		if availHeight > maxDiffHeight {
			availHeight = maxDiffHeight
		}
		if availHeight >= 2 { // need at least separator + one line
			diffLines := d.renderDiffSummary(width, availHeight)
			bottomLines = append(bottomLines, diffLines...)
		}
	}
	if len(bottomLines) > 0 {
		contentH := d.contentHeight()
		padding := contentH - len(lines) - len(bottomLines)
		for i := 0; i < padding; i++ {
			lines = append(lines, "")
		}
		lines = append(lines, bottomLines...)
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
		if d.panelFocus == focusConfig && d.repoConfigForm != nil {
			// Show repo config form.
			title := StyleTitle.Render(" " + item.repoName + " Settings ")
			pathLine := StyleSubtle.Render(" " + item.repoPath)
			formView := d.repoConfigForm.View()
			return lipgloss.JoinVertical(lipgloss.Left, title, pathLine, "", formView)
		}
		// Show repo info in the preview panel when a repo header is selected.
		title := StyleTitle.Render(" " + item.repoName + " ")
		pathLine := StyleSubtle.Render(" " + item.repoPath)
		hint := StyleSubtle.Render(" Press enter to configure this repo")
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
		sessionInfo = StyleSubtle.Render(fmt.Sprintf(" Session: %s  Worktree: %s", item.session.GetDisplayName(), item.session.Worktree.Path))
	}
	var prInfo string
	if item.session != nil {
		if entry := d.prCache[item.session.ID]; entry != nil && entry.pr != nil {
			pr := entry.pr
			checkStr := ""
			if entry.checks != nil {
				switch entry.checks.State {
				case "success":
					checkStr = fmt.Sprintf(" -- %d/%d checks "+StyleSuccess.Render("\u2713"), entry.checks.Passed, entry.checks.Total)
				case "failure":
					checkStr = fmt.Sprintf(" -- %d/%d checks "+StyleError.Render("\u2717"), entry.checks.Passed, entry.checks.Total)
				case "pending":
					checkStr = fmt.Sprintf(" -- %d/%d checks "+StyleWarning.Render("\u25cb"), entry.checks.Passed, entry.checks.Total)
				}
			}
			prInfo = StyleSubtle.Render(fmt.Sprintf(" PR: #%d (%s)%s  %s", pr.Number, pr.State, checkStr, pr.URL))
		}
	}
	var taskInfo string
	if ag.IsShell {
		taskInfo = StyleSubtle.Render(" Shell — " + ag.WorktreePath)
	} else {
		taskInfo = StyleSubtle.Render(" Task: " + ag.Task)
	}

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

	previewParts := []string{title, sessionInfo}
	if prInfo != "" {
		previewParts = append(previewParts, prInfo)
	}
	previewParts = append(previewParts, taskInfo, "", render)
	return lipgloss.JoinVertical(lipgloss.Left, previewParts...)
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

// renderDiffSummary renders the CHANGES section for the diff summary panel.
// It returns a slice of lines that fit within the given height.
func (d dashboardModel) renderDiffSummary(width, maxHeight int) []string {
	stats := d.diffStats

	// Build the header line: "── CHANGES ── 3 files +47 -12"
	agg := stats.Aggregate
	headerStats := fmt.Sprintf(" %d files ", agg.Files)
	headerStats += StyleSuccess.Render(fmt.Sprintf("+%d", agg.Insertions))
	headerStats += " "
	headerStats += StyleError.Render(fmt.Sprintf("-%d", agg.Deletions))

	sepWidth := width - 2
	if sepWidth < 0 {
		sepWidth = 0
	}
	headerLabel := "── CHANGES ──"
	padLen := sepWidth - len(headerLabel) - len(fmt.Sprintf(" %d files +%d -%d", agg.Files, agg.Insertions, agg.Deletions))
	if padLen < 0 {
		padLen = 0
	}
	header := StyleSubtle.Render(headerLabel) + headerStats + StyleSubtle.Render(strings.Repeat("─", padLen))

	var lines []string
	lines = append(lines, header)

	if len(stats.Files) == 0 {
		lines = append(lines, StyleSubtle.Render("  No changes"))
		return lines
	}

	// Determine how many file rows we can show.
	availRows := maxHeight - 1 // subtract header
	fileCount := len(stats.Files)
	showMore := false
	visibleFiles := fileCount
	if fileCount > availRows {
		visibleFiles = availRows - 1 // leave room for "+N more" indicator
		showMore = true
	}

	statusStyle := func(status string) lipgloss.Style {
		switch status {
		case "A":
			return lipgloss.NewStyle().Foreground(ColorSuccess)
		case "D":
			return lipgloss.NewStyle().Foreground(ColorError)
		default: // "M"
			return lipgloss.NewStyle().Foreground(ColorSecondary)
		}
	}

	for i := 0; i < visibleFiles; i++ {
		f := stats.Files[i]
		styledStatus := statusStyle(f.Status).Render(f.Status)
		ins := StyleSuccess.Render(fmt.Sprintf("+%d", f.Insertions))
		del := StyleError.Render(fmt.Sprintf("-%d", f.Deletions))
		statsText := fmt.Sprintf(" %s %s", ins, del)
		// Visible length of stats: " +N -N"
		statsLen := 2 + len(fmt.Sprintf("+%d", f.Insertions)) + 1 + len(fmt.Sprintf("-%d", f.Deletions))

		name := filepath.Base(f.Path)
		// "  S name    +N -N" — 4 chars prefix ("  S "), stats at end
		maxNameLen := width - 4 - statsLen
		if maxNameLen < 1 {
			maxNameLen = 1
		}
		if len(name) > maxNameLen {
			name = name[:maxNameLen-1] + "…"
		}
		padName := maxNameLen - len(name)
		if padName < 0 {
			padName = 0
		}
		line := fmt.Sprintf("  %s %s%s%s", styledStatus, name, strings.Repeat(" ", padName), statsText)
		lines = append(lines, line)
	}

	if showMore {
		remaining := fileCount - visibleFiles
		lines = append(lines, StyleSubtle.Render(fmt.Sprintf("  +%d more files", remaining)))
	}

	return lines
}

// renderPRSummary renders a compact PR status line for the left panel bottom section.
func (d dashboardModel) renderPRSummary(entry *prCacheEntry, width int) string {
	pr := entry.pr
	checks := entry.checks

	prLabel := fmt.Sprintf("── PR #%d ──", pr.Number)
	var checkInfo string
	if checks != nil {
		checkInfo = fmt.Sprintf(" %d/%d ", checks.Passed, checks.Total)
		switch checks.State {
		case "success":
			checkInfo += StyleSuccess.Render("\u2713")
		case "failure":
			checkInfo += StyleError.Render("\u2717")
		case "pending":
			checkInfo += StyleWarning.Render("\u25cb")
		}
	}

	sepWidth := width - 2
	if sepWidth < 0 {
		sepWidth = 0
	}
	// Visible length of label + checkInfo (approximate).
	visLen := len(prLabel)
	if checks != nil {
		visLen += 1 + len(fmt.Sprintf("%d/%d", checks.Passed, checks.Total)) + 2
	}
	padLen := sepWidth - visLen
	if padLen < 0 {
		padLen = 0
	}

	return StyleSubtle.Render(prLabel) + checkInfo + StyleSubtle.Render(strings.Repeat("─", padLen))
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
