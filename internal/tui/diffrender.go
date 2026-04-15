package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	git "github.com/devenjarvis/baton/internal/git"
)

// sideBySideMinWidth is the terminal width threshold below which the detail
// view falls back to unified rendering.
const sideBySideMinWidth = 120

// diffBarWidth is the width of the per-file +/- visual bar in the summary.
const diffBarWidth = 20

// ── Summary ──────────────────────────────────────────────────────────────────

// renderSummary renders the full-width list of changed files. Rows are assumed
// to already be sorted by magnitude by the caller. The selected row is
// highlighted.
func renderSummary(agentName string, files []git.DiffFile, selected, width, height int) string {
	if height < 1 {
		return ""
	}
	if width < 1 {
		width = 1
	}

	var totalIns, totalDel int
	for _, f := range files {
		totalIns += f.Insertions
		totalDel += f.Deletions
	}

	fileCount := fmt.Sprintf("%d file", len(files))
	if len(files) != 1 {
		fileCount += "s"
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	title := StyleTitle.Render(agentName) + "  " +
		mutedStyle.Render(fileCount) + "  " +
		addStyle.Render(fmt.Sprintf("+%d", totalIns)) + "  " +
		delStyle.Render(fmt.Sprintf("-%d", totalDel))

	rowsAvail := height - 2 // title + blank
	if rowsAvail < 1 {
		rowsAvail = 1
	}

	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)
	addStatusStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStatusStyle := lipgloss.NewStyle().Foreground(ColorError)
	modStatusStyle := lipgloss.NewStyle().Foreground(ColorWarning)

	// Compute column widths. Format per row:
	//   "<S>  <path…>  <bar>  +X -Y"
	// Fixed: status(1) + 2 + bar(diffBarWidth) + 2 + counts.
	// Path takes the rest.
	countsWidth := 12 // generous: "+9999 -9999" = 11
	barWidth := diffBarWidth
	fixed := 1 + 2 + barWidth + 2 + countsWidth + 2 // spaces around path too
	pathWidth := width - fixed
	if pathWidth < 8 {
		pathWidth = 8
	}

	// Reserve one slot for the overflow indicator when the list doesn't fit.
	visibleRows := rowsAvail
	hasOverflow := len(files) > rowsAvail
	if hasOverflow {
		visibleRows = rowsAvail - 1
		if visibleRows < 1 {
			visibleRows = 1
		}
	}
	// Compute a window so the selected row stays visible.
	top := 0
	if selected >= visibleRows {
		top = selected - visibleRows + 1
	}
	if top > len(files)-visibleRows {
		top = len(files) - visibleRows
	}
	if top < 0 {
		top = 0
	}
	end := top + visibleRows
	if end > len(files) {
		end = len(files)
	}

	rows := make([]string, 0, rowsAvail)
	for i := top; i < end; i++ {
		f := files[i]
		var statusStr string
		switch f.Status {
		case "A":
			statusStr = addStatusStyle.Render("A")
		case "D":
			statusStr = delStatusStyle.Render("D")
		default:
			statusStr = modStatusStyle.Render("M")
		}

		path := truncatePathLeft(f.Path, pathWidth)
		var pathRendered string
		if i == selected {
			pathRendered = selectedStyle.Render(padRight(path, pathWidth))
		} else {
			pathRendered = normalStyle.Render(padRight(path, pathWidth))
		}

		bar := renderChangeBar(f.Insertions, f.Deletions, barWidth)
		counts := addStyle.Render(fmt.Sprintf("+%d", f.Insertions)) + " " +
			delStyle.Render(fmt.Sprintf("-%d", f.Deletions))

		row := statusStr + "  " + pathRendered + "  " + bar + "  " + counts
		rows = append(rows, row)
	}
	if hasOverflow {
		above := top
		below := len(files) - end
		var label string
		switch {
		case above > 0 && below > 0:
			label = fmt.Sprintf("↑ %d more above  ·  ↓ %d more below", above, below)
		case above > 0:
			label = fmt.Sprintf("↑ %d more above", above)
		default:
			label = fmt.Sprintf("↓ %d more below", below)
		}
		rows = append(rows, mutedStyle.Render(label))
	}
	for len(rows) < rowsAvail {
		rows = append(rows, "")
	}

	return title + "\n\n" + strings.Join(rows, "\n")
}

// renderChangeBar returns a fixed-width bar showing the proportion of
// insertions to deletions. Fills green for additions and red for deletions
// proportional to their count.
func renderChangeBar(ins, del, width int) string {
	if width < 1 {
		return ""
	}
	total := ins + del
	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	if total == 0 {
		return mutedStyle.Render(strings.Repeat("─", width))
	}
	addN := (ins * width) / total
	delN := (del * width) / total
	// Ensure each side gets at least 1 cell if it has any changes.
	if ins > 0 && addN == 0 {
		addN = 1
	}
	if del > 0 && delN == 0 {
		delN = 1
	}
	if addN+delN > width {
		// Trim deletions first to fit.
		delN = width - addN
		if delN < 0 {
			addN = width
			delN = 0
		}
	}
	padN := width - addN - delN
	if padN < 0 {
		padN = 0
	}
	return addStyle.Render(strings.Repeat("+", addN)) +
		delStyle.Render(strings.Repeat("-", delN)) +
		mutedStyle.Render(strings.Repeat(" ", padN))
}

// ── Detail ───────────────────────────────────────────────────────────────────

// renderDetail renders the header and body for one file's diff. Chooses
// side-by-side or unified based on available width.
func renderDetail(agentName string, file git.DiffFile, hunks []git.Hunk, scroll, width, height int) string {
	if height < 1 {
		return ""
	}
	if width < 1 {
		width = 1
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	status := file.Status
	if status == "" {
		status = "M"
	}
	header := StyleTitle.Render(agentName) + "  " +
		mutedStyle.Render(status+" "+file.Path) + "  " +
		addStyle.Render(fmt.Sprintf("+%d", file.Insertions)) + "  " +
		delStyle.Render(fmt.Sprintf("-%d", file.Deletions))

	bodyHeight := height - 2 // header + blank
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	if width >= sideBySideMinWidth {
		body = renderSideBySide(hunks, width, bodyHeight, scroll)
	} else {
		body = renderUnified(hunks, width, bodyHeight, scroll)
	}

	return header + "\n\n" + body
}

// sideBySideRow is a single visual row in the side-by-side body.
type sideBySideRow struct {
	leftKind  git.HunkLineKind
	leftText  string
	leftNum   int // 0 means blank/no number
	rightKind git.HunkLineKind
	rightText string
	rightNum  int
	// hasLeft/hasRight distinguish "blank filler" from "context that happens to be empty".
	hasLeft  bool
	hasRight bool
	// isHunkHeader indicates a row that spans both sides showing the @@ header.
	isHunkHeader bool
	headerText   string
}

// renderSideBySide renders the detail body as two columns.
func renderSideBySide(hunks []git.Hunk, width, height, scroll int) string {
	if height < 1 {
		return ""
	}
	rows := buildSideBySideRows(hunks)

	start := scroll
	if start < 0 {
		start = 0
	}
	if start > len(rows) {
		start = len(rows)
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
	}

	// Column widths: 5-char gutter per side, 1-char separator between gutter and content.
	// Layout: [num:4][space][content] | [num:4][space][content]
	// Total fixed: 4 + 1 (gutter right border space) + 1 (separator) + 1 (gutter left space after sep) + 4 + 1 = 12
	gutterWidth := 4
	sepWidth := 3 // " │ "
	content := (width - 2*gutterWidth - 2 - sepWidth) / 2
	if content < 4 {
		content = 4
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	hunkStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	contextStyle := lipgloss.NewStyle().Foreground(ColorText)

	out := make([]string, 0, height)
	for _, r := range rows[start:end] {
		if r.isHunkHeader {
			line := hunkStyle.Render(truncate(r.headerText, width))
			out = append(out, padRight(line, width))
			continue
		}
		leftNum := renderGutter(r.leftNum, gutterWidth, r.hasLeft, mutedStyle)
		rightNum := renderGutter(r.rightNum, gutterWidth, r.hasRight, mutedStyle)

		leftText := padRight(truncate(r.leftText, content), content)
		rightText := padRight(truncate(r.rightText, content), content)

		if r.hasLeft {
			leftText = styleHunkKind(r.leftKind, leftText, addStyle, delStyle, contextStyle)
		} else {
			leftText = mutedStyle.Render(leftText)
		}
		if r.hasRight {
			rightText = styleHunkKind(r.rightKind, rightText, addStyle, delStyle, contextStyle)
		} else {
			rightText = mutedStyle.Render(rightText)
		}

		sep := mutedStyle.Render(" │ ")
		out = append(out, leftNum+" "+leftText+sep+rightNum+" "+rightText)
	}

	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func renderGutter(num, width int, has bool, style lipgloss.Style) string {
	if !has || num == 0 {
		return style.Render(strings.Repeat(" ", width))
	}
	s := fmt.Sprintf("%*d", width, num)
	return style.Render(s)
}

func styleHunkKind(kind git.HunkLineKind, text string, add, del, ctx lipgloss.Style) string {
	switch kind {
	case git.HunkLineAddition:
		return add.Render(text)
	case git.HunkLineDeletion:
		return del.Render(text)
	default:
		return ctx.Render(text)
	}
}

// buildSideBySideRows walks all hunks and produces visual rows. Deletion+addition
// runs are paired row-by-row.
func buildSideBySideRows(hunks []git.Hunk) []sideBySideRow {
	rows := make([]sideBySideRow, 0, len(hunks))
	for _, h := range hunks {
		rows = append(rows, sideBySideRow{isHunkHeader: true, headerText: h.Header})
		oldNum := h.OldStart
		newNum := h.NewStart
		var pendingDel []git.HunkLine
		var pendingDelNums []int
		var pendingAdd []git.HunkLine
		var pendingAddNums []int

		flush := func() {
			max := len(pendingDel)
			if len(pendingAdd) > max {
				max = len(pendingAdd)
			}
			for i := 0; i < max; i++ {
				var row sideBySideRow
				if i < len(pendingDel) {
					row.leftKind = pendingDel[i].Kind
					row.leftText = pendingDel[i].Text
					row.leftNum = pendingDelNums[i]
					row.hasLeft = true
				}
				if i < len(pendingAdd) {
					row.rightKind = pendingAdd[i].Kind
					row.rightText = pendingAdd[i].Text
					row.rightNum = pendingAddNums[i]
					row.hasRight = true
				}
				rows = append(rows, row)
			}
			pendingDel = pendingDel[:0]
			pendingDelNums = pendingDelNums[:0]
			pendingAdd = pendingAdd[:0]
			pendingAddNums = pendingAddNums[:0]
		}

		for _, l := range h.Lines {
			switch l.Kind {
			case git.HunkLineContext:
				flush()
				rows = append(rows, sideBySideRow{
					leftKind:  git.HunkLineContext,
					leftText:  l.Text,
					leftNum:   oldNum,
					hasLeft:   true,
					rightKind: git.HunkLineContext,
					rightText: l.Text,
					rightNum:  newNum,
					hasRight:  true,
				})
				oldNum++
				newNum++
			case git.HunkLineDeletion:
				pendingDel = append(pendingDel, l)
				pendingDelNums = append(pendingDelNums, oldNum)
				oldNum++
			case git.HunkLineAddition:
				pendingAdd = append(pendingAdd, l)
				pendingAddNums = append(pendingAddNums, newNum)
				newNum++
			}
		}
		flush()
	}
	return rows
}

// renderUnified renders the detail body as a single-column unified diff.
func renderUnified(hunks []git.Hunk, width, height, scroll int) string {
	if height < 1 {
		return ""
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorError)
	hunkStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	contextStyle := lipgloss.NewStyle().Foreground(ColorText)

	lines := make([]string, 0, len(hunks))
	for _, h := range hunks {
		lines = append(lines, hunkStyle.Render(truncate(h.Header, width)))
		for _, l := range h.Lines {
			var prefix, styled string
			switch l.Kind {
			case git.HunkLineAddition:
				prefix = "+"
				styled = addStyle.Render(truncate(prefix+l.Text, width))
			case git.HunkLineDeletion:
				prefix = "-"
				styled = delStyle.Render(truncate(prefix+l.Text, width))
			default:
				prefix = " "
				styled = contextStyle.Render(truncate(prefix+l.Text, width))
			}
			lines = append(lines, styled)
		}
	}

	start := scroll
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, height)
	out = append(out, lines[start:end]...)
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// ── Empty state ──────────────────────────────────────────────────────────────

// renderEmptyState returns a centered "no changes" message.
func renderEmptyState(agentName string, width, height int) string {
	if height < 1 {
		return ""
	}
	if width < 1 {
		width = 1
	}
	iconStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText)
	subtleStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	icon := iconStyle.Render("·")
	title := titleStyle.Render("No changes yet")
	subtitle := subtleStyle.Render(agentName + " hasn't modified any files in its worktree.")

	block := lipgloss.JoinVertical(lipgloss.Center, icon, title, subtitle)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// truncate hard-truncates a string to n display cells, appending "…" if
// truncation occurred. Rune-safe.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	// Keep n-1 runes and append ellipsis.
	count := 0
	end := 0
	for i := range s {
		if count == n-1 {
			end = i
			break
		}
		count++
	}
	return s[:end] + "…"
}

// padRight pads s with spaces on the right to reach n display cells.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// truncatePathLeft truncates a path from the left (preserving the tail) with
// a leading ellipsis when it exceeds n runes.
func truncatePathLeft(path string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(path) <= n {
		return path
	}
	if n == 1 {
		return "…"
	}
	// Keep the last n-1 runes.
	runes := []rune(path)
	return "…" + string(runes[len(runes)-(n-1):])
}
