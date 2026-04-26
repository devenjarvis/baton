// Package vt provides a bridge wrapper around a virtual terminal emulator.
// It receives raw PTY output and maintains screen state, exposing a simple
// API for reading terminal content and sending input.
package vt

import (
	"bytes"
	"hash/fnv"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	xvt "github.com/charmbracelet/x/vt"
)

const maxHistoryLines = 5000

// Terminal wraps a virtual terminal emulator, providing a simplified interface
// for feeding PTY output and reading screen state.
type Terminal struct {
	emu *xvt.SafeEmulator

	// Bridge pipe: emu.Read → pw → pr → our Read.
	// Allows Close to unblock Read without touching the Emulator directly,
	// avoiding a data race with SafeEmulator's internal mutex.
	pr *io.PipeReader
	pw *io.PipeWriter

	closeOnce sync.Once

	// history captures lines that scrolled off the top of the screen.
	// The x/vt emulator only exposes main-screen scrollback, so apps using
	// alternate screen mode (like Claude Code) would otherwise lose history.
	// We detect scroll events by checking if the cursor is at the bottom row
	// before writing a newline, then capturing the top line before it's lost.
	history   []string
	historyMu sync.RWMutex

	// lastWriteNano is the UnixNano timestamp of the last Write() completion.
	// Atomic because Write() (agent.readLoop goroutine) and StableRender()
	// (Bubble Tea main goroutine) access it from different goroutines.
	lastWriteNano atomic.Int64

	// stableRender caches the last Render() snapshot for StableRender().
	// Protected by stableRenderMu: read/written by StableRender() (Bubble Tea
	// main goroutine) and invalidated by Resize() and Write()'s alt-screen
	// transition (agent.readLoop goroutine).
	stableRender   string
	stableRenderMu sync.Mutex

	// wasAltScreen tracks the previous alt-screen state for transition detection.
	// Only accessed from Write() (agent.readLoop goroutine), no mutex needed.
	wasAltScreen bool

	// altScreenEntered is set to true on a false->true alt-screen transition.
	// Written by Write() (agent.readLoop), read by AltScreenEntered() (Bubble Tea).
	// Protected by historyMu.
	altScreenEntered bool
}

// New creates a new Terminal with the given dimensions.
func New(width, height int) *Terminal {
	emu := xvt.NewSafeEmulator(width, height)
	pr, pw := io.Pipe()
	t := &Terminal{
		emu: emu,
		pr:  pr,
		pw:  pw,
	}
	go t.bridgeRead()
	return t
}

// bridgeRead continuously reads from the emulator and writes to our pipe.
func (t *Terminal) bridgeRead() {
	buf := make([]byte, 256)
	for {
		n, err := t.emu.Read(buf)
		if n > 0 {
			if _, werr := t.pw.Write(buf[:n]); werr != nil {
				return // pipe closed
			}
		}
		if err != nil {
			t.pw.CloseWithError(err)
			return
		}
	}
}

// Write feeds raw PTY output into the emulator and captures any lines that
// scroll off the top into the history buffer.
//
// The input is processed line-by-line (splitting on LF). Before writing each
// LF-terminated chunk, we check whether the cursor is at the bottom row of
// the scroll region. If so, the write will cause the top line to scroll off,
// and we capture it before it's lost. This works for both main-screen and
// alternate-screen terminals.
//
// Write must not be called concurrently. The three SafeEmulator reads
// (CursorPosition, Height, Render) and the subsequent Write each acquire the
// emulator lock independently, which is safe because agent.readLoop is the
// only caller and never calls Write from more than one goroutine.
func (t *Terminal) Write(p []byte) (int, error) {
	written := 0
	remaining := p
	for len(remaining) > 0 {
		// Split on the first LF to process one line boundary at a time.
		idx := bytes.IndexByte(remaining, '\n')
		var chunk []byte
		if idx < 0 {
			chunk = remaining
			remaining = nil
		} else {
			chunk = remaining[:idx+1]
			remaining = remaining[idx+1:]
		}

		// If the cursor is at the bottom row and this chunk includes a LF,
		// the current top line is about to scroll off — capture it first.
		// Note: this assumes a full-height scroll region (the default). Apps
		// that set a DECSTBM sub-region would need the scroll region bottom
		// instead of Height()-1, but SafeEmulator does not expose that.
		if idx >= 0 {
			pos := t.emu.CursorPosition()
			if pos.Y >= t.emu.Height()-1 {
				topLine := strings.SplitN(t.emu.Render(), "\n", 2)[0]
				t.historyMu.Lock()
				t.history = append(t.history, topLine)
				if len(t.history) > maxHistoryLines {
					trimmed := make([]string, maxHistoryLines)
					copy(trimmed, t.history[len(t.history)-maxHistoryLines:])
					t.history = trimmed
				}
				t.historyMu.Unlock()
			}
		}

		n, err := t.emu.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}

		// Detect false->true alt-screen transition.
		isAlt := t.emu.IsAltScreen()
		if isAlt && !t.wasAltScreen {
			t.historyMu.Lock()
			t.altScreenEntered = true
			t.history = nil
			t.historyMu.Unlock()
			// Invalidate the StableRender cache so the first post-transition
			// read surfaces the new alt-screen content, not the pre-transition
			// snapshot. Done with a separate lock because stableRender is
			// independently serialized from historyMu.
			t.stableRenderMu.Lock()
			t.stableRender = ""
			t.stableRenderMu.Unlock()
		}
		t.wasAltScreen = isAlt
	}
	t.lastWriteNano.Store(time.Now().UnixNano())
	return written, nil
}

// Render returns the full screen as an ANSI-encoded string.
func (t *Terminal) Render() string {
	return t.emu.Render()
}

// StableRender returns a cached render if the terminal was written to within the
// last 16ms (mid-repaint), otherwise snapshots a fresh Render(). This avoids
// showing torn/partial frames during rapid emulator updates. The cache is
// invalidated on Resize() and on alt-screen entry so transitions never leak a
// snapshot at the wrong dimensions.
func (t *Terminal) StableRender() string {
	t.stableRenderMu.Lock()
	defer t.stableRenderMu.Unlock()
	lastWrite := t.lastWriteNano.Load()
	if t.stableRender != "" && lastWrite > 0 && time.Since(time.Unix(0, lastWrite)) < 16*time.Millisecond {
		return t.stableRender
	}
	t.stableRender = t.emu.Render()
	return t.stableRender
}

// RenderPadded returns exactly `height` lines, each exactly `width` display
// cells wide, separated by `\n`. Each line ends with an ANSI style reset so
// trailing cells don't carry a lingering SGR into the next line. The emulator's
// renderLine trims trailing empty cells, which is the root cause of preview
// artifacts — this method re-adds those trailing spaces so every frame fully
// overwrites the previous one.
func (t *Terminal) RenderPadded(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	raw := t.emu.Render()
	return padFrame(raw, width, height)
}

// SelectionRect is a viewport-local cell rectangle used by the selection-aware
// render and extract helpers. Coordinates are zero-based cell indices, and
// the rect is inclusive on both ends; per-row span is "from StartX on row
// StartY through EndX on row EndY" — see RenderPaddedWithSelection for the
// exact membership rule. When Active is false, the helpers behave as if no
// selection were present.
type SelectionRect struct {
	StartX, StartY int
	EndX, EndY     int
	Active         bool
}

// inSelection reports whether (x, y) falls inside sel under the per-line rule:
// rows strictly inside the [StartY, EndY] band are fully selected; the start
// row is selected from StartX to end-of-row; the end row is selected from
// start-of-row to EndX; a single-row selection uses [StartX, EndX].
func (sel SelectionRect) inSelection(x, y int) bool {
	if !sel.Active || y < sel.StartY || y > sel.EndY {
		return false
	}
	if y > sel.StartY && y < sel.EndY {
		return true
	}
	if sel.StartY == sel.EndY {
		return x >= sel.StartX && x <= sel.EndX
	}
	if y == sel.StartY {
		return x >= sel.StartX
	}
	return x <= sel.EndX
}

// RenderPaddedWithSelection returns a render identical in shape to RenderPadded
// (width × height cells, lines separated by `\n`, each line ending with a
// style reset) but with the cells inside sel rendered in reverse video so the
// underlying foreground/background are preserved. When sel.Active is false,
// this delegates to RenderPadded.
//
// Iteration goes cell-by-cell so the lipgloss border around the viewport is
// excluded by construction — the selection rect lives in VT-cell coordinates,
// not screen pixels, so the frame can never appear inside the highlight.
func (t *Terminal) RenderPaddedWithSelection(width, height int, sel SelectionRect) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if !sel.Active {
		return t.RenderPadded(width, height)
	}

	var b strings.Builder
	b.Grow(height * (width + 16))
	for y := 0; y < height; y++ {
		t.renderLineWithSelection(&b, width, y, sel)
		b.WriteString("\x1b[0m")
		if y < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderLineWithSelection emits one row of width cells into b, honoring cell
// styles and toggling SGR reverse video at selection boundaries. Wide cells
// (Width == 2) emit their content once and consume two columns; if either
// column is inside the selection, the cell is treated as selected.
func (t *Terminal) renderLineWithSelection(b *strings.Builder, width, y int, sel SelectionRect) {
	var (
		pen     uv.Style
		penSet  bool // false until we have written a real Style transition this line
		reverse bool // SGR 7 currently on
	)
	for x := 0; x < width; x++ {
		cell := t.emu.CellAt(x, y)
		// Wide-cell trailing column: defensive skip. The lead branch below
		// advances x past the trailing column, so this normally never fires
		// — but if the emulator ever surfaces a stray Width==0 cell without
		// a preceding lead, skipping it is the right behavior.
		if cell != nil && cell.Width == 0 {
			continue
		}

		var (
			content string
			cellW   int
			style   uv.Style
		)
		if cell == nil {
			content = " "
			cellW = 1
		} else {
			content = cell.Content
			if content == "" {
				content = " "
			}
			cellW = cell.Width
			if cellW < 1 {
				cellW = 1
			}
			style = cell.Style
		}

		// Selection membership for this cell: a wide cell is "in" if either
		// of its columns is in the selection.
		inSel := sel.inSelection(x, y)
		if cellW == 2 && !inSel {
			inSel = sel.inSelection(x+1, y)
		}

		// Style transition. Always emit on the first cell of the line so any
		// leftover SGR state from a prior line's reset is overwritten cleanly.
		styleChanged := !penSet || !style.Equal(&pen)
		if styleChanged {
			diff := style.Diff(&pen)
			if !penSet && diff == "" {
				// First cell with zero style: still emit a reset so we don't
				// inherit any pen the previous line might have left set.
				b.WriteString("\x1b[0m")
			} else {
				b.WriteString(diff)
			}
			pen = style
			penSet = true
		}

		// Reverse-video state. style.Diff may emit \x1b[0m, which clears all
		// attributes including reverse — re-emit \x1b[7m after a style change
		// while we're still inside the selection so the highlight survives.
		switch {
		case inSel && (!reverse || styleChanged):
			b.WriteString("\x1b[7m")
			reverse = true
		case !inSel && reverse:
			b.WriteString("\x1b[27m")
			reverse = false
		}

		b.WriteString(content)
		// Skip the trailing column of a wide cell so we don't double-emit.
		if cellW == 2 {
			x++
		}
	}
	if reverse {
		b.WriteString("\x1b[27m")
	}
}

// ExtractText returns the plain text inside rect, joining rows with "\n" and
// trimming trailing whitespace from each row. No styles or hyperlink metadata
// are emitted. Wide cells contribute their content once (on the lead column)
// and the trailing column is skipped; a wide cell is included if either of
// its columns is inside the selection — matches RenderPaddedWithSelection.
// An empty or inactive rect returns "".
func (t *Terminal) ExtractText(rect SelectionRect) string {
	if !rect.Active {
		return ""
	}
	width := t.emu.Width()
	var rows []string
	for y := rect.StartY; y <= rect.EndY; y++ {
		var line strings.Builder
		for x := 0; x < width; x++ {
			cell := t.emu.CellAt(x, y)
			if cell != nil && cell.Width == 0 {
				// Trailing column of a wide cell — handled at lead.
				continue
			}
			cellW := 1
			if cell != nil && cell.Width > 1 {
				cellW = cell.Width
			}
			inSel := rect.inSelection(x, y)
			if cellW == 2 && !inSel {
				inSel = rect.inSelection(x+1, y)
			}
			if !inSel {
				if cellW == 2 {
					x++
				}
				continue
			}
			if cell == nil || cell.Content == "" {
				line.WriteByte(' ')
			} else {
				line.WriteString(cell.Content)
			}
			if cellW == 2 {
				x++
			}
		}
		rows = append(rows, strings.TrimRight(line.String(), " \t"))
	}
	return strings.Join(rows, "\n")
}

// padFrame right-pads every line in `raw` (a `\n`-separated render) to `width`
// cells and forces the output to exactly `height` lines.
func padFrame(raw string, width, height int) string {
	lines := strings.Split(raw, "\n")
	var b strings.Builder
	b.Grow(height * (width + 8))
	pad := func(n int) {
		if n <= 0 {
			return
		}
		b.WriteString(strings.Repeat(" ", n))
	}
	for i := 0; i < height; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(line)
		visible := ansi.StringWidth(line)
		pad(width - visible)
		b.WriteString("\x1b[0m")
		if i < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Snapshot returns a consistent view of scrollback and the current viewport
// under a single lock on the history buffer. Callers that splice scrollback
// and viewport together (e.g., dashboard preview during pgup scroll) must use
// this to avoid racing the Write goroutine between the two reads.
//
// Lock order: historyMu.RLock is held across emu.Render(). Safe because
// emu.Render() acquires only the emulator's internal lock, and Write() always
// calls emu.Write() before acquiring historyMu — so there is no cycle between
// historyMu and the emulator lock.
func (t *Terminal) Snapshot(width, height int) (scrollback []string, viewport string) {
	t.historyMu.RLock()
	defer t.historyMu.RUnlock()
	if len(t.history) > 0 {
		scrollback = make([]string, len(t.history))
		copy(scrollback, t.history)
	}
	viewport = padFrame(t.emu.Render(), width, height)
	return scrollback, viewport
}

// AltScreenEntered returns true if a false->true alt-screen transition has been
// detected since the last call. The flag is reset on each read. This allows the
// TUI layer to detect when an agent enters alt-screen mode (e.g., Claude Code
// starting its interactive UI).
func (t *Terminal) AltScreenEntered() bool {
	t.historyMu.Lock()
	entered := t.altScreenEntered
	t.altScreenEntered = false
	t.historyMu.Unlock()
	return entered
}

// ScreenHash returns a deterministic hash of the current visible screen content.
// Identical rendered screens produce identical hashes; any visible change
// (rune, color, style, cursor on a filled cell) produces a different hash.
// Safe to call concurrently with Write/Render — delegates to Render which
// acquires the emulator lock.
func (t *Terminal) ScreenHash() uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(t.emu.Render()))
	return h.Sum64()
}

// RenderRegion returns a subset of rows from startRow to endRow (inclusive)
// as an ANSI-encoded string. Rows are zero-indexed.
func (t *Terminal) RenderRegion(startRow, endRow int) string {
	full := t.emu.Render()
	lines := strings.Split(full, "\n")

	if startRow < 0 {
		startRow = 0
	}
	if endRow >= len(lines) {
		endRow = len(lines) - 1
	}
	if startRow > endRow {
		return ""
	}

	return strings.Join(lines[startRow:endRow+1], "\n")
}

// Resize changes the terminal dimensions. Invalidates the StableRender cache
// so the next read returns a fresh snapshot at the new dimensions.
func (t *Terminal) Resize(width, height int) {
	t.emu.Resize(width, height)
	t.stableRenderMu.Lock()
	t.stableRender = ""
	t.stableRenderMu.Unlock()
	t.lastWriteNano.Store(0)
}

// SendKey forwards a key event to the emulator.
func (t *Terminal) SendKey(key xvt.KeyPressEvent) {
	t.emu.SendKey(key)
}

// SendMouse forwards a mouse event to the emulator. The emulator only emits
// encoded bytes when the app has enabled mouse reporting (DECSET 1000/1002/1003
// plus an extended-encoding mode like SGR 1006); otherwise this is a no-op —
// which is the desired behavior for agents that haven't opted into mouse input.
func (t *Terminal) SendMouse(m xvt.Mouse) {
	t.emu.SendMouse(m)
}

// IsAltScreen reports whether the emulator is currently in alternate-screen
// mode. Safe to call concurrently with Write.
func (t *Terminal) IsAltScreen() bool {
	return t.emu.IsAltScreen()
}

// SendText forwards text input to the emulator.
func (t *Terminal) SendText(text string) {
	t.emu.SendText(text)
}

// Paste forwards a paste event to the emulator.
func (t *Terminal) Paste(text string) {
	t.emu.Paste(text)
}

// Read reads escape sequences generated by SendKey/SendText from the emulator.
// Reads from a bridged pipe so that Close can unblock it without data races.
func (t *Terminal) Read(buf []byte) (int, error) {
	return t.pr.Read(buf)
}

// CursorPosition returns the current cursor position (x, y) where x is the
// column and y is the row, both zero-indexed.
func (t *Terminal) CursorPosition() (x, y int) {
	pos := t.emu.CursorPosition()
	return pos.X, pos.Y
}

// Width returns the terminal width in columns.
func (t *Terminal) Width() int {
	return t.emu.Width()
}

// Height returns the terminal height in rows.
func (t *Terminal) Height() int {
	return t.emu.Height()
}

// ScrollbackLines returns scrolled-off lines as ANSI-encoded strings, oldest
// first. The history buffer is populated by Write's scroll detection and
// captures scrollback for both main-screen and alternate-screen terminals.
// Returns nil if no lines have scrolled off yet.
func (t *Terminal) ScrollbackLines() []string {
	t.historyMu.RLock()
	defer t.historyMu.RUnlock()
	if len(t.history) == 0 {
		return nil
	}
	result := make([]string, len(t.history))
	copy(result, t.history)
	return result
}

// Close releases resources and unblocks any pending Read calls.
// Safe to call concurrently and multiple times.
func (t *Terminal) Close() {
	t.closeOnce.Do(func() {
		// Close our pipe to unblock any Read callers.
		// The bridgeRead goroutine will exit on next pw.Write error.
		_ = t.pr.Close()
		t.pw.CloseWithError(io.ErrClosedPipe)
	})
}
