package diffmodel

// Row is one side-by-side visual row with independent left and right cells.
// A row with Blank* flags on a side has no line number and no content —
// used to pad asymmetric del/add runs so the central separator stays put.
// The renderer treats HunkHeader rows separately.
type Row struct {
	LeftKind  LineKind
	LeftNum   int
	LeftText  string
	LeftBlank bool

	RightKind  LineKind
	RightNum   int
	RightText  string
	RightBlank bool

	// HunkHeader is true for a row that spans the full width (the @@ banner).
	// When set, HeaderText holds the banner; everything else is zero-valued.
	HunkHeader bool
	HeaderText string
}

// LayoutHunks builds the side-by-side row sequence for a set of hunks.
//
// Pairing rule: within a hunk, walk lines in order. A contiguous run of
// deletions followed by a contiguous run of additions is paired index-by-index;
// the shorter side is padded with blank rows. Deletions without any following
// additions pair against blank rows (and vice versa). Context lines always
// emit on both sides.
//
// contentWidth is accepted for caller convenience (e.g. letting callers fuse
// layout+wrap in one pass) but is not used here — the function is purely
// structural.
func LayoutHunks(hunks []Hunk, contentWidth int) []Row {
	_ = contentWidth
	rows := make([]Row, 0)
	for _, h := range hunks {
		rows = append(rows, Row{HunkHeader: true, HeaderText: h.Header})

		var pendingDel, pendingAdd []Line

		flush := func() {
			n := len(pendingDel)
			if len(pendingAdd) > n {
				n = len(pendingAdd)
			}
			for i := 0; i < n; i++ {
				var row Row
				if i < len(pendingDel) {
					l := pendingDel[i]
					row.LeftKind = LineDelete
					row.LeftNum = l.OldNum
					row.LeftText = l.Text
				} else {
					row.LeftBlank = true
				}
				if i < len(pendingAdd) {
					l := pendingAdd[i]
					row.RightKind = LineAdd
					row.RightNum = l.NewNum
					row.RightText = l.Text
				} else {
					row.RightBlank = true
				}
				rows = append(rows, row)
			}
			pendingDel = pendingDel[:0]
			pendingAdd = pendingAdd[:0]
		}

		for _, l := range h.Lines {
			switch l.Kind {
			case LineContext:
				flush()
				rows = append(rows, Row{
					LeftKind:  LineContext,
					LeftNum:   l.OldNum,
					LeftText:  l.Text,
					RightKind: LineContext,
					RightNum:  l.NewNum,
					RightText: l.Text,
				})
			case LineDelete:
				pendingDel = append(pendingDel, l)
			case LineAdd:
				pendingAdd = append(pendingAdd, l)
			}
		}
		flush()
	}
	return rows
}
