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
	}
	return written, nil
}

// Render returns the full screen as an ANSI-encoded string.
func (t *Terminal) Render() string {
	return t.emu.Render()
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

// Resize changes the terminal dimensions.
func (t *Terminal) Resize(width, height int) {
	t.emu.Resize(width, height)
}

// SendKey forwards a key event to the emulator.
func (t *Terminal) SendKey(key xvt.KeyPressEvent) {
	t.emu.SendKey(key)
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
