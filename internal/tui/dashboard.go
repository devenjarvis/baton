package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	xvt "github.com/charmbracelet/x/vt"
	"github.com/devenjarvis/baton/internal/agent"
	"github.com/devenjarvis/baton/internal/config"
	"github.com/devenjarvis/baton/internal/github"
	"github.com/devenjarvis/baton/internal/vt"
)

// truncateVisible returns s truncated to n display cells with an ellipsis.
// ANSI-aware; avoids the naive byte-slice truncation that can cut multi-byte
// runes in half or miscount ANSI escape sequences.
func truncateVisible(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

// ColorWaiting is the accent used for agents in StatusWaiting (permission
// prompts, input blocks). Scoped to the dashboard because no other view
// needs it today — add to theme.go if another view ever surfaces waiting.
var ColorWaiting = lipgloss.Color("#D946EF")

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
	items           []listItem
	selected        int
	width           int
	height          int
	sidebarWidth    int // resolved global SidebarWidth; 0 means use DefaultSidebarWidth
	panelFocus      panelFocus
	scrollOffset    int
	diffStats       *diffSummaryData           // nil when no session selected or no data
	prCache         map[string]*prCacheEntry   // keyed by session ID, passed from App
	prPollStates    map[string]*prSessionState // keyed by session ID, passed from App
	closingAgents   map[string]bool            // keyed by agent ID, passed from App
	closingSessions map[string]bool            // keyed by session ID, passed from App

	// prSectionY is the content-relative row index (0-indexed, after the AGENTS
	// title and separator) where the PR checks summary begins, or -1 when absent.
	prSectionY int

	// Repo config form shown in the right panel when focusConfig is active.
	repoConfigForm *configForm
	configRepoPath string // path of the repo being configured

	// Mouse text selection state in VT-cell coordinates, bound to a specific
	// agent so a sidebar selection change clears it cleanly.
	selection selection
}

// selection tracks an in-progress or completed mouse drag selection inside the
// agent VT viewport. Coordinates are zero-based cell indices within the
// agent's viewport (0..fixedTermWidth, 0..fixedTermHeight).
type selection struct {
	anchorX, anchorY int
	cursorX, cursorY int
	active           bool   // a click has seeded an in-flight or completed selection
	dragSeen         bool   // mouse moved away from the anchor; distinguishes drag from plain click
	agentID          string // agent.Agent.ID() the selection is bound to
}

func newDashboardModel() dashboardModel {
	return dashboardModel{prSectionY: -1}
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

// listWidth returns the configured sidebar width, falling back to the default
// when sidebarWidth has not yet been plumbed in. Both View() and
// fixedTermWidth() must return the same value on any given frame, otherwise
// the sidebar and the agent VT will disagree about column counts.
func (d dashboardModel) listWidth() int {
	if d.sidebarWidth > 0 {
		return d.sidebarWidth
	}
	return config.DefaultSidebarWidth
}

func (d dashboardModel) View() string {
	if len(d.items) == 0 {
		return d.emptyView()
	}

	listWidth := d.listWidth()
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
		// Size the bordered box so its inner content height matches the
		// number of rows we actually render: metadata rows + the full VT
		// viewport. Deriving it this way keeps the VT unclipped when no PR
		// row is present and makes the 1-row clip when PR is visible
		// deterministic, instead of relying on lipgloss's implicit
		// truncation to hide the mismatch.
		previewStyle = lipgloss.NewStyle().
			Width(d.fixedTermWidth()).
			Height(d.previewMetadataRows() + d.fixedTermHeight()).
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorSecondary)
	}

	out := lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(list),
		previewStyle.Render(preview),
	)
	if path := os.Getenv("BATON_E2E_DEBUG_DUMP"); path != "" {
		_ = os.WriteFile(path, []byte(out), 0o644)
	}
	return out
}

func (d dashboardModel) contentHeight() int {
	return d.height - 2 // statusbar + title
}

// fixedTermWidth returns the terminal column count that all agents should use.
// This is always the focusTerminal width (deducting the border) regardless of
// the current panelFocus, so that focus switches never trigger a resize.
func (d dashboardModel) fixedTermWidth() int {
	return d.width - d.listWidth() - 1 - 2 // list border + focusTerminal border
}

// fixedTermHeight returns the terminal row count that all agents should use.
// This is always the focusTerminal height (deducting the border) regardless of
// the current panelFocus. It intentionally does NOT deduct the PR line —
// accepting 1 row of clipping when PR is visible is better than per-session
// resize churn.
func (d dashboardModel) fixedTermHeight() int {
	return d.contentHeight() - 4 - 2 // metadata rows + focusTerminal border
}

// previewMetadataRows returns the number of non-VT rows rendered above the
// terminal viewport in the preview panel: title, sessionInfo, taskInfo, and
// the blank separator — plus one row for the PR info line when the selected
// session has an open PR. Mouse coordinate translation in app.go consumes
// this via screenToTermCell so wheel/click/drag stay aligned with the
// viewport when the PR row appears or disappears.
func (d dashboardModel) previewMetadataRows() int {
	rows := 4 // title, sessionInfo, taskInfo, blank
	if sess := d.selectedSession(); sess != nil {
		if entry := d.prCache[sess.ID]; entry != nil && entry.pr != nil {
			rows++
		}
	}
	return rows
}

// previewTermWidth returns the terminal column count for the preview panel.
func (d dashboardModel) previewTermWidth() int {
	return d.fixedTermWidth()
}

// previewTermHeight returns the terminal row count for the preview panel.
func (d dashboardModel) previewTermHeight() int {
	return d.fixedTermHeight()
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
			name := truncateVisible(item.repoName, width-4)
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
			closing := d.closingSessions != nil && d.closingSessions[sess.ID]
			var symbolStyle lipgloss.Style
			// StatusWaiting is intentionally absent: Session.Status() rolls
			// Waiting up to Active at the session level, so this switch only
			// ever sees Active/Starting/Idle/Done/Error.
			switch {
			case closing:
				symbolStyle = lipgloss.NewStyle().Foreground(ColorMuted)
			default:
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
			}

			displayName := sess.GetDisplayName()

			// Build rename-in-flight indicator suffix. Skipped while closing.
			var renameSuffix string
			var renameSuffixLen int
			if !closing && sess.IsRenaming() {
				renameSuffix = " ⏏"
				renameSuffixLen = 2 // space + glyph
			}

			// Build PR indicator suffix for the session row. Skipped while
			// closing so the " closing…" tag doesn't fight with a PR badge
			// for limited header width.
			var prSuffix string
			var prSuffixLen int
			if !closing {
				if entry := d.prCache[sess.ID]; entry != nil && entry.pr != nil {
					prSuffix = " " + prIndicator(entry)
					// Approximate visible length: space + #N + space + symbol + optional " Ready"
					prSuffixLen = 1 + len(fmt.Sprintf("#%d", entry.pr.Number))
					if entry.checks != nil {
						prSuffixLen += 2 // space + check symbol
					}
					if isMergeReady(entry) {
						prSuffixLen += 6 // " Ready"
					}
				}
			}

			// Reserve width for " closing…" suffix when closing.
			closingTag := ""
			closingTagLen := 0
			if closing {
				closingTag = " closing…"
				closingTagLen = 9
			}

			// 4 for "  ──", 3 for " symbol ", 1 trailing space, plus some padding chars
			maxNameLen := width - 10 - renameSuffixLen - prSuffixLen - closingTagLen
			if maxNameLen < 5 {
				maxNameLen = 5
			}
			displayName = truncateVisible(displayName, maxNameLen)

			nameStyle := lipgloss.NewStyle()
			if closing {
				nameStyle = StyleSubtle
			}
			label := fmt.Sprintf(" %s %s", symbolStyle.Render(symbol), nameStyle.Render(displayName))
			if closing {
				label += StyleSubtle.Render(closingTag)
			}
			label += renameSuffix + " "
			labelLen := ansi.StringWidth(symbol) + 1 + ansi.StringWidth(displayName) + 2 + renameSuffixLen + prSuffixLen + closingTagLen
			padLen := width - 4 - labelLen
			if padLen < 0 {
				padLen = 0
			}

			// Use flash color for separator dashes if the session has an active flash.
			sepStyle := StyleSubtle
			if !closing {
				if ps := d.prPollStates[sess.ID]; ps != nil && time.Now().Before(ps.flashUntil) {
					switch ps.flashColor {
					case "success":
						sepStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
					case "error":
						sepStyle = lipgloss.NewStyle().Foreground(ColorError)
					}
				}
			}

			line := sepStyle.Render("  ──") + label + prSuffix + sepStyle.Render(strings.Repeat("─", padLen))
			lines = append(lines, line)

		case listItemAgent:
			// Agent row — indented under its session.
			ag := item.agent
			status := ag.Status()
			symbol := status.Symbol()
			closing := d.closingAgents != nil && d.closingAgents[ag.ID]

			if ag.IsShell {
				symbol = "$"
			}

			var style lipgloss.Style
			switch {
			case closing:
				style = lipgloss.NewStyle().Foreground(ColorMuted)
			case ag.IsShell:
				style = lipgloss.NewStyle().Foreground(ColorMuted)
			default:
				switch status {
				case agent.StatusActive:
					style = lipgloss.NewStyle().Foreground(ColorSecondary)
				case agent.StatusWaiting:
					style = lipgloss.NewStyle().Foreground(ColorWaiting)
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

			// Suffix: elapsed time in the normal case, "closing…" when dispatched.
			// Width budgeting keeps the "closing…" tag inside the same column so
			// the layout doesn't shift while the kill is in flight.
			suffix := humanizeElapsed(ag.Elapsed())
			suffixWidth := 5
			if closing {
				suffix = "closing…"
				suffixWidth = 9 // visual width of the suffix including ellipsis
			}

			nameWidth := width - 13 - suffixWidth
			if nameWidth < 1 {
				nameWidth = 1
			}
			name := truncateVisible(ag.GetDisplayName(), nameWidth)

			agentPrefix := "      "
			if isSelected {
				agentPrefix = "    " + StyleActive.Render("▸ ")
			}

			renderedSuffix := suffix
			if closing {
				renderedSuffix = StyleSubtle.Render(suffix)
			}
			nameRendered := name
			if closing {
				nameRendered = StyleSubtle.Render(name)
			}
			padName := nameWidth - ansi.StringWidth(name)
			if padName < 0 {
				padName = 0
			}

			line := fmt.Sprintf("%s%s %s%s %s",
				agentPrefix,
				style.Render(symbol),
				nameRendered,
				strings.Repeat(" ", padName),
				renderedSuffix,
			)
			lines = append(lines, line)
		}
	}

	// Render PR checks summary and diff stats at the bottom of the left panel.
	// Guarantee a minimum height for the PR panel so it stays visible even
	// when the agent list is long — truncating the list with a "+N more"
	// indicator if necessary.
	contentH := d.contentHeight()
	var bottomLines []string

	var prEntry *prCacheEntry
	if sess := d.selectedSession(); sess != nil {
		if e := d.prCache[sess.ID]; e != nil && e.pr != nil {
			prEntry = e
		}
	}

	// Reserve height for the PR panel up front so it survives a crowded
	// agent list. Capped at half of contentH on narrow terminals to avoid
	// starving the agent list entirely.
	prBudget := 0
	if prEntry != nil {
		prBudget = 6
		if half := contentH / 2; prBudget > half {
			prBudget = half
		}
	}

	// If the agent list + reserved PR budget would exceed contentH, truncate
	// the list and replace the overflow with a "+N more" marker so the PR
	// panel still renders.
	if prBudget > 0 && len(lines) > contentH-prBudget {
		maxList := contentH - prBudget
		if maxList < 1 {
			maxList = 1
		}
		// Keep room for the "+N more" marker itself.
		if maxList < len(lines) {
			kept := maxList - 1
			if kept < 0 {
				kept = 0
			}
			hidden := len(lines) - kept
			truncated := make([]string, 0, kept+1)
			truncated = append(truncated, lines[:kept]...)
			truncated = append(truncated, StyleSubtle.Render(fmt.Sprintf("  +%d more", hidden)))
			lines = truncated
		}
	}

	if prEntry != nil {
		avail := contentH - len(lines)
		if avail > prBudget && prBudget > 0 {
			// Cap at the reserved budget so diffStats has room below.
			if avail > prBudget*2 {
				avail = prBudget * 2
			}
		}
		if maxCheck := contentH / 3; avail > maxCheck && maxCheck >= prBudget {
			avail = maxCheck
		}
		if avail >= 2 {
			bottomLines = append(bottomLines, d.renderChecksSummary(prEntry, width, avail)...)
		}
	}
	if d.diffStats != nil {
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
		sessionInfo = StyleSubtle.Render(fmt.Sprintf(" Session: %s  Branch: %s  Worktree: %s",
			item.session.GetDisplayName(), item.session.Branch(), item.session.Worktree.Path))
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
			prInfo = StyleSubtle.Render(" PR: ") + StyleLink.Render(fmt.Sprintf("#%d", pr.Number)) + StyleSubtle.Render(fmt.Sprintf(" (%s)%s  %s", pr.State, checkStr, pr.URL))
		}
	}
	var taskInfo string
	if ag.IsShell {
		taskInfo = StyleSubtle.Render(" Shell — " + ag.WorktreePath)
	} else {
		taskInfo = StyleSubtle.Render(" Task: " + ag.Task)
	}

	var render string
	vpWidth := d.previewTermWidth()
	vpHeight := d.previewTermHeight()
	if d.scrollOffset > 0 {
		sbLines, viewport := ag.Snapshot(vpWidth, vpHeight)
		vpLines := strings.Split(viewport, "\n")
		allLines := append(sbLines, vpLines...)

		end := len(allLines) - d.scrollOffset
		if end < 0 {
			end = 0
		}
		start := end - vpHeight
		if start < 0 {
			start = 0
		}
		render = strings.Join(allLines[start:end], "\n")
	} else if d.selection.active && d.selection.dragSeen && d.selection.agentID == ag.ID {
		sx, sy, ex, ey, _ := d.selectionRect()
		render = ag.RenderPaddedWithSelection(vpWidth, vpHeight, vt.SelectionRect{
			StartX: sx, StartY: sy, EndX: ex, EndY: ey, Active: true,
		})
	} else {
		render = ag.RenderPadded(vpWidth, vpHeight)
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

// clearSelection resets the mouse text-selection state. Safe to call when no
// selection is active.
func (d *dashboardModel) clearSelection() {
	d.selection = selection{}
}

// selectionRect returns the active selection as a normalized rectangle in
// VT-cell coordinates. Normalization is by row first, so for a multi-row
// reverse drag (anchor row > cursor row) the returned startX/endX may be
// "out of order" relative to a Cartesian rect — that asymmetry is intentional
// and matches the per-line membership rule in vt.SelectionRect.inSelection:
// startX picks where the start row begins, endX picks where the end row ends,
// and the X axis is independent on each row. ok is false when there is no
// drag-confirmed selection to render or copy from.
func (d dashboardModel) selectionRect() (startX, startY, endX, endY int, ok bool) {
	if !d.selection.active || !d.selection.dragSeen {
		return 0, 0, 0, 0, false
	}
	startX, endX = d.selection.anchorX, d.selection.cursorX
	startY, endY = d.selection.anchorY, d.selection.cursorY
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}
	return startX, startY, endX, endY, true
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

// renderChecksSummary renders a rich PR checks section for the left panel bottom.
// It shows per-check rows with status, name, and duration, plus review status.
func (d dashboardModel) renderChecksSummary(entry *prCacheEntry, width, maxHeight int) []string {
	pr := entry.pr

	// Header line: "── PR #42 ── Ready ──────"
	prLabel := fmt.Sprintf("── PR #%d ──", pr.Number)
	var badge string
	var badgeLen int
	if isMergeReady(entry) {
		badge = " " + lipgloss.NewStyle().Foreground(ColorSuccess).Render("Ready") + " "
		badgeLen = 7 // " Ready "
	}

	sepWidth := width - 2
	if sepWidth < 0 {
		sepWidth = 0
	}
	padLen := sepWidth - len(prLabel) - badgeLen
	if padLen < 0 {
		padLen = 0
	}
	header := StyleSubtle.Render("── PR ") + StyleLink.Render(fmt.Sprintf("#%d", pr.Number)) + StyleSubtle.Render(" ──") + badge + StyleSubtle.Render(strings.Repeat("─", padLen))

	var lines []string
	lines = append(lines, header)

	// Review status line.
	if entry.reviews != nil && maxHeight > 2 {
		lines = append(lines, d.renderReviewLine(entry.reviews, width))
	}

	// Per-check rows.
	if entry.checks != nil && len(entry.checks.Runs) > 0 {
		availRows := maxHeight - len(lines)
		lines = append(lines, d.renderCheckRows(entry.checks.Runs, width, availRows)...)
	}

	return lines
}

// renderReviewLine renders the review status line.
func (d dashboardModel) renderReviewLine(reviews *github.ReviewStatus, width int) string {
	var symbol string
	var style lipgloss.Style
	var text string
	switch reviews.State {
	case "approved":
		symbol = "\u2713"
		style = lipgloss.NewStyle().Foreground(ColorSuccess)
		text = fmt.Sprintf("%d approved", reviews.Approved)
	case "changes_requested":
		symbol = "\u2717"
		style = lipgloss.NewStyle().Foreground(ColorError)
		text = fmt.Sprintf("%d changes requested", reviews.ChangesRequested)
	default:
		symbol = "\u25cb"
		style = lipgloss.NewStyle().Foreground(ColorWarning)
		text = "pending"
		if reviews.Approved > 0 {
			text = fmt.Sprintf("%d approved, pending", reviews.Approved)
		}
	}
	return fmt.Sprintf("  Reviews: %s %s", style.Render(symbol), text)
}

// renderCheckRows renders individual check run rows sorted: failed, pending, passed.
func (d dashboardModel) renderCheckRows(runs []github.CheckRun, width, availRows int) []string {
	if availRows <= 0 {
		return nil
	}

	// Sort: failed first, then pending/in_progress, then passed.
	sorted := make([]github.CheckRun, len(runs))
	copy(sorted, runs)
	sort.Slice(sorted, func(i, j int) bool {
		return checkSortOrder(sorted[i]) < checkSortOrder(sorted[j])
	})

	showMore := false
	visible := len(sorted)
	if visible > availRows {
		visible = availRows - 1 // leave room for "+N more"
		showMore = true
	}

	var lines []string
	for i := 0; i < visible; i++ {
		run := sorted[i]
		var symbol string
		var style lipgloss.Style
		switch {
		case run.Status != "completed":
			symbol = "\u25cb"
			style = lipgloss.NewStyle().Foreground(ColorWarning)
		case run.Conclusion == "success" || run.Conclusion == "skipped" || run.Conclusion == "neutral":
			symbol = "\u2713"
			style = lipgloss.NewStyle().Foreground(ColorSuccess)
		default:
			symbol = "\u2717"
			style = lipgloss.NewStyle().Foreground(ColorError)
		}

		// Duration or "running" indicator.
		var durStr string
		if run.Status != "completed" {
			durStr = "running"
		} else {
			durStr = formatCheckDuration(run.Duration)
		}
		durLen := len(durStr)

		// "  ✓ name       12s"
		nameWidth := width - 4 - durLen - 1 // "  S " prefix + duration + space
		if nameWidth < 1 {
			nameWidth = 1
		}
		name := run.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}
		padName := nameWidth - len(name)
		if padName < 0 {
			padName = 0
		}

		line := fmt.Sprintf("  %s %s%s%s", style.Render(symbol), name, strings.Repeat(" ", padName), StyleSubtle.Render(durStr))
		lines = append(lines, line)
	}

	if showMore {
		remaining := len(sorted) - visible
		lines = append(lines, StyleSubtle.Render(fmt.Sprintf("  +%d more", remaining)))
	}

	return lines
}

// checkSortOrder returns a sort key: 0=failed, 1=pending, 2=passed.
func checkSortOrder(run github.CheckRun) int {
	switch {
	case run.Status == "completed" && run.Conclusion != "success" && run.Conclusion != "skipped" && run.Conclusion != "neutral":
		return 0 // failed
	case run.Status != "completed":
		return 1 // pending/running
	default:
		return 2 // passed
	}
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
