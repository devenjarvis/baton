package tui

import (
	"sort"

	tea "charm.land/bubbletea/v2"
	git "github.com/devenjarvis/baton/internal/git"
)

// diffMode selects between the summary and detail views.
type diffMode int

const (
	summaryMode diffMode = iota
	detailMode
)

// diffModel drives the two-mode diff browser.
type diffModel struct {
	agentName string
	files     []git.DiffFile
	hunks     [][]git.Hunk // parallel to files; pre-parsed once
	mode      diffMode
	selected  int // index of selected file in summary
	scroll    int // scroll offset in detail mode
	width     int
	height    int
}

type diffCloseMsg struct{}

// newDiffModel constructs a diffModel. Files are sorted by total change
// magnitude (descending, tie-break on path ascending) and hunks are
// pre-parsed for each file.
func newDiffModel(agentName string, files []git.DiffFile, width, height int) diffModel {
	sorted := make([]git.DiffFile, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(i, j int) bool {
		mi := sorted[i].Insertions + sorted[i].Deletions
		mj := sorted[j].Insertions + sorted[j].Deletions
		if mi != mj {
			return mi > mj
		}
		return sorted[i].Path < sorted[j].Path
	})

	hunks := make([][]git.Hunk, len(sorted))
	for i, f := range sorted {
		hunks[i] = git.ParseHunks(f)
	}

	return diffModel{
		agentName: agentName,
		files:     sorted,
		hunks:     hunks,
		mode:      summaryMode,
		width:     width,
		height:    height,
	}
}

// Mode exposes the current mode to callers (e.g., status-bar hint selection).
func (d diffModel) Mode() diffMode {
	return d.mode
}

func (d diffModel) Update(msg tea.Msg) (diffModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if d.mode == detailMode {
			return d.updateDetail(msg)
		}
		return d.updateSummary(msg)
	}
	return d, nil
}

func (d diffModel) updateSummary(msg tea.KeyPressMsg) (diffModel, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return d, func() tea.Msg { return diffCloseMsg{} }
	}
	if len(d.files) == 0 {
		return d, nil
	}
	switch msg.String() {
	case "j", "down":
		if d.selected < len(d.files)-1 {
			d.selected++
		}
	case "k", "up":
		if d.selected > 0 {
			d.selected--
		}
	case "g":
		d.selected = 0
	case "G":
		d.selected = len(d.files) - 1
	case "enter", "l", "right":
		d.mode = detailMode
		d.scroll = 0
	}
	return d, nil
}

func (d diffModel) updateDetail(msg tea.KeyPressMsg) (diffModel, tea.Cmd) {
	switch msg.String() {
	case "q":
		return d, func() tea.Msg { return diffCloseMsg{} }
	case "esc", "h", "left":
		d.mode = summaryMode
		d.scroll = 0
		return d, nil
	}
	if len(d.files) == 0 {
		return d, nil
	}
	switch msg.String() {
	case "j", "down":
		d.scroll++
		d.clampScroll()
	case "k", "up":
		d.scroll--
		if d.scroll < 0 {
			d.scroll = 0
		}
	case "d", "ctrl+d":
		d.scroll += d.detailBodyHeight() / 2
		d.clampScroll()
	case "u", "ctrl+u":
		d.scroll -= d.detailBodyHeight() / 2
		if d.scroll < 0 {
			d.scroll = 0
		}
	case "n":
		if d.selected < len(d.files)-1 {
			d.selected++
			d.scroll = 0
		}
	case "p":
		if d.selected > 0 {
			d.selected--
			d.scroll = 0
		}
	}
	return d, nil
}

// detailBodyHeight returns the approximate height available for the detail
// body (total minus header + blank).
func (d diffModel) detailBodyHeight() int {
	h := d.height - 2
	if h < 1 {
		return 1
	}
	return h
}

// clampScroll keeps scroll within the bounds of the current file's rendered
// detail body. It accounts for the current render mode (side-by-side vs
// unified), since side-by-side pairs del/add runs into fewer rows.
func (d *diffModel) clampScroll() {
	if len(d.files) == 0 || d.selected < 0 || d.selected >= len(d.hunks) {
		d.scroll = 0
		return
	}
	var total int
	if d.width >= sideBySideMinWidth {
		total = len(buildSideBySideRows(d.hunks[d.selected]))
	} else {
		for _, h := range d.hunks[d.selected] {
			total += 1 + len(h.Lines) // header + lines
		}
	}
	max := total - d.detailBodyHeight()
	if max < 0 {
		max = 0
	}
	if d.scroll > max {
		d.scroll = max
	}
}

func (d diffModel) View() string {
	if len(d.files) == 0 {
		return renderEmptyState(d.agentName, d.width, d.height)
	}
	if d.mode == detailMode {
		return renderDetail(
			d.agentName,
			d.files[d.selected],
			d.hunks[d.selected],
			d.scroll,
			d.width,
			d.height,
		)
	}
	return renderSummary(d.agentName, d.files, d.selected, d.width, d.height)
}
