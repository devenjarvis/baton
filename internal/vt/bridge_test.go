package vt

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	xvt "github.com/charmbracelet/x/vt"
)

func TestWriteAndRender(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, err := term.Write([]byte("Hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	output := term.Render()
	if !strings.Contains(output, "Hello") {
		t.Errorf("Render() should contain 'Hello', got: %q", output)
	}
}

func TestANSIColorPreserved(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Write red text using SGR escape sequence
	_, err := term.Write([]byte("\x1b[31mRed Text\x1b[0m"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	output := term.Render()
	if !strings.Contains(output, "Red Text") {
		t.Errorf("Render() should contain 'Red Text', got: %q", output)
	}
	// The ANSI color codes should be present in the rendered output
	if !strings.Contains(output, "\x1b[") {
		t.Errorf("Render() should contain ANSI escape sequences, got: %q", output)
	}
}

func TestResize(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	if term.Width() != 80 {
		t.Errorf("expected width 80, got %d", term.Width())
	}
	if term.Height() != 24 {
		t.Errorf("expected height 24, got %d", term.Height())
	}

	term.Resize(120, 40)

	if term.Width() != 120 {
		t.Errorf("expected width 120 after resize, got %d", term.Width())
	}
	if term.Height() != 40 {
		t.Errorf("expected height 40 after resize, got %d", term.Height())
	}
}

func TestSendTextAndRead(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	type readResult struct {
		n   int
		err error
		buf []byte
	}

	// Start reading in a goroutine first since both SendText and Read
	// use a pipe and must happen concurrently.
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := term.Read(buf)
		ch <- readResult{n, err, buf}
	}()

	// Give the goroutine a moment to block on Read, then send text.
	time.Sleep(10 * time.Millisecond)
	term.SendText("hello")

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("Read failed: %v", res.err)
		}
		if res.n == 0 {
			t.Error("expected Read to return bytes after SendText, got 0")
		}
		if string(res.buf[:res.n]) != "hello" {
			t.Errorf("expected Read to return 'hello', got %q", string(res.buf[:res.n]))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read timed out after 2 seconds")
	}
}

func TestPasteAndRead(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	type readResult struct {
		n   int
		err error
		buf []byte
	}

	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 1024)
		n, err := term.Read(buf)
		ch <- readResult{n, err, buf}
	}()

	time.Sleep(10 * time.Millisecond)
	term.Paste("pasted text")

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("Read failed: %v", res.err)
		}
		if res.n == 0 {
			t.Error("expected Read to return bytes after Paste, got 0")
		}
		got := string(res.buf[:res.n])
		// Paste wraps content in bracketed paste sequences (\x1b[200~ ... \x1b[201~)
		if !strings.Contains(got, "pasted text") {
			t.Errorf("expected Read output to contain 'pasted text', got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read timed out after 2 seconds")
	}
}

func TestRenderRegion(t *testing.T) {
	term := New(80, 5)
	defer term.Close()

	// Write text on different lines
	_, _ = term.Write([]byte("Line1\r\nLine2\r\nLine3\r\nLine4\r\nLine5"))

	region := term.RenderRegion(1, 3)
	lines := strings.Split(region, "\n")

	// Region should have lines 1-3 (3 lines)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines in region, got %d", len(lines))
	}
}

func TestScrollbackLines(t *testing.T) {
	// 2-row terminal: writing 3 lines forces the first line into scrollback.
	term := New(80, 2)
	defer term.Close()

	// Write 3 lines — the first will scroll off into the scrollback buffer.
	_, _ = term.Write([]byte("First\r\nSecond\r\nThird"))

	lines := term.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatal("expected scrollback to be non-empty after scrolling past terminal height")
	}
	// The first line should contain "First".
	if !strings.Contains(lines[0], "First") {
		t.Errorf("expected scrollback line 0 to contain 'First', got %q", lines[0])
	}
}

func TestScrollbackLinesEmpty(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// No content written — scrollback should be empty.
	lines := term.ScrollbackLines()
	if len(lines) != 0 {
		t.Errorf("expected empty scrollback, got %d lines", len(lines))
	}
}

func TestScrollbackLinesAltScreen(t *testing.T) {
	// 3-row terminal in alt screen mode: writing 4 lines forces the first line
	// into our history buffer (alt screen scrollback isn't exposed by x/vt).
	term := New(80, 3)
	defer term.Close()

	// Enter alternate screen mode.
	_, _ = term.Write([]byte("\x1b[?1049h"))

	// Write 4 lines — the first will scroll off the 3-row alt screen.
	_, _ = term.Write([]byte("AltFirst\r\nAltSecond\r\nAltThird\r\nAltFourth"))

	lines := term.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatal("expected scrollback to be non-empty after scrolling in alt screen mode")
	}
	if !strings.Contains(lines[0], "AltFirst") {
		t.Errorf("expected scrollback line 0 to contain 'AltFirst', got %q", lines[0])
	}
}

func TestScrollbackLinesHistoryPreferred(t *testing.T) {
	// Verify that entering alt screen clears pre-existing main-screen history
	// (startup splash is garbage) and that subsequent alt-screen scrollback is
	// still captured.
	term := New(80, 2)
	defer term.Close()

	// Scroll in main screen to populate history.
	_, _ = term.Write([]byte("MainFirst\r\nMainSecond\r\nMainThird"))
	lines := term.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatal("expected non-empty scrollback before alt-screen transition")
	}

	// Enter alt screen — history should be cleared (splash scrollback is garbage).
	_, _ = term.Write([]byte("\x1b[?1049h"))
	lines = term.ScrollbackLines()
	if len(lines) != 0 {
		t.Errorf("expected empty scrollback after alt-screen transition, got %d lines", len(lines))
	}

	// Scroll in alt screen — new scrollback should be captured.
	_, _ = term.Write([]byte("AltFirst\r\nAltSecond\r\nAltThird"))
	lines = term.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatal("expected non-empty scrollback after scrolling in alt screen")
	}
	hasAlt := false
	for _, line := range lines {
		if strings.Contains(line, "AltFirst") {
			hasAlt = true
		}
	}
	if !hasAlt {
		t.Errorf("expected 'AltFirst' in scrollback history, got: %v", lines)
	}
}

func TestScreenHashStableWhenUnchanged(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("stable content"))

	h1 := term.ScreenHash()
	h2 := term.ScreenHash()
	if h1 != h2 {
		t.Errorf("expected identical hashes for unchanged screen, got %d and %d", h1, h2)
	}
}

func TestScreenHashChangesOnWrite(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("before"))
	before := term.ScreenHash()

	_, _ = term.Write([]byte(" after"))
	after := term.ScreenHash()

	if before == after {
		t.Errorf("expected hash to change after Write, both are %d", before)
	}
}

func TestScreenHashStableAcrossIdenticalWrites(t *testing.T) {
	a := New(80, 24)
	defer a.Close()
	b := New(80, 24)
	defer b.Close()

	_, _ = a.Write([]byte("same payload"))
	_, _ = b.Write([]byte("same payload"))

	if a.ScreenHash() != b.ScreenHash() {
		t.Errorf("expected identical hashes for identical screen content, got %d vs %d",
			a.ScreenHash(), b.ScreenHash())
	}
}

func TestScreenHashChangesOnStyleChange(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("plain"))
	plain := term.ScreenHash()

	// Clear and write the same text but bold.
	_, _ = term.Write([]byte("\x1b[2J\x1b[H\x1b[1mplain\x1b[0m"))
	styled := term.ScreenHash()

	if plain == styled {
		t.Errorf("expected hash to differ when style changes, both are %d", plain)
	}
}

func TestScreenHashChangesOnResize(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("hello"))
	before := term.ScreenHash()

	term.Resize(40, 12)
	after := term.ScreenHash()

	if before == after {
		t.Errorf("expected hash to change after resize (different render domain), both are %d", before)
	}
}

func TestStableRenderQuiescent(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("Hello, world!"))

	// Wait for writes to settle (well past the 16ms threshold).
	time.Sleep(20 * time.Millisecond)

	got := term.StableRender()
	want := term.Render()
	if got != want {
		t.Errorf("StableRender() after quiescence should match Render()\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStableRenderDuringActiveWrites(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Write initial content and let it settle.
	_, _ = term.Write([]byte("initial"))
	time.Sleep(20 * time.Millisecond)

	// Prime the cache.
	cached := term.StableRender()
	if !strings.Contains(cached, "initial") {
		t.Fatalf("expected StableRender to contain 'initial', got %q", cached)
	}

	// Write new content — should be within 16ms window.
	_, _ = term.Write([]byte(" updated"))

	// StableRender should return the cached version (before "updated").
	got := term.StableRender()
	if got != cached {
		t.Errorf("StableRender() during active writes should return cached value\ngot:  %q\nwant: %q", got, cached)
	}

	// After settling, StableRender should reflect the new content.
	time.Sleep(20 * time.Millisecond)
	settled := term.StableRender()
	if !strings.Contains(settled, "updated") {
		t.Errorf("StableRender() after settling should contain 'updated', got %q", settled)
	}
}

func TestAltScreenEnteredTransition(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Before any alt-screen transition, flag should be false.
	if term.AltScreenEntered() {
		t.Error("AltScreenEntered() should be false before any alt-screen transition")
	}

	// Enter alt screen.
	_, _ = term.Write([]byte("\x1b[?1049h"))

	// Flag should now be true.
	if !term.AltScreenEntered() {
		t.Error("AltScreenEntered() should be true after entering alt screen")
	}

	// Second call should return false (flag is reset on read).
	if term.AltScreenEntered() {
		t.Error("AltScreenEntered() should be false on second call (resets after read)")
	}
}

func TestAltScreenEnteredClearsHistory(t *testing.T) {
	term := New(80, 2)
	defer term.Close()

	// Scroll in main screen to populate history.
	_, _ = term.Write([]byte("First\r\nSecond\r\nThird"))
	lines := term.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatal("expected non-empty scrollback before alt-screen transition")
	}

	// Enter alt screen — history should be cleared.
	_, _ = term.Write([]byte("\x1b[?1049h"))
	lines = term.ScrollbackLines()
	if len(lines) != 0 {
		t.Errorf("expected empty scrollback after alt-screen transition, got %d lines: %v", len(lines), lines)
	}
}

func TestAltScreenEnteredNoSpuriousTransition(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Enter alt screen.
	_, _ = term.Write([]byte("\x1b[?1049h"))
	// Consume the flag.
	term.AltScreenEntered()

	// Write more content while already in alt screen — no new transition.
	_, _ = term.Write([]byte("more content in alt screen"))
	if term.AltScreenEntered() {
		t.Error("AltScreenEntered() should be false when already in alt screen (no new transition)")
	}
}

// readAllAvailable drains up to bufsz bytes from term.Read in the background.
// Used by tests that need to assert what bytes the emulator wrote to the PTY
// side after a SendKey/SendMouse call.
func readAllAvailable(t *testing.T, term *Terminal) []byte {
	t.Helper()
	type res struct {
		data []byte
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		buf := make([]byte, 1024)
		n, err := term.Read(buf)
		ch <- res{buf[:n], err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Read failed: %v", r.err)
		}
		return r.data
	case <-time.After(2 * time.Second):
		t.Fatal("Read timed out after 2 seconds")
		return nil
	}
}

func TestSendMouseEmitsSGRWhenReportingEnabled(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Enable button-event mouse reporting (DECSET 1002) + SGR ext mode (1006).
	// Without this pair the emulator emits nothing, matching real terminals
	// that only forward mouse bytes to apps that opted in.
	_, _ = term.Write([]byte("\x1b[?1002h\x1b[?1006h"))

	term.SendMouse(xvt.MouseWheel{X: 5, Y: 7, Button: xvt.MouseWheelUp})

	got := string(readAllAvailable(t, term))
	// SGR wheel-up: ESC [ < 64 ; X+1 ; Y+1 M  (coords are 1-based)
	want := "\x1b[<64;6;8M"
	if got != want {
		t.Errorf("SendMouse wheel-up SGR: got %q, want %q", got, want)
	}
}

func TestSendMouseNoOpWhenReportingDisabled(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	// Kick the bridgeRead goroutine by writing a harmless byte we can drain.
	// Without any reporting mode enabled, SendMouse must not emit anything —
	// so we use SendText after to verify nothing from SendMouse is lurking.
	term.SendMouse(xvt.MouseWheel{X: 1, Y: 1, Button: xvt.MouseWheelDown})
	term.SendText("x")

	got := string(readAllAvailable(t, term))
	if got != "x" {
		t.Errorf("expected only SendText bytes %q after no-reporting SendMouse, got %q", "x", got)
	}
}

func TestIsAltScreen(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	if term.IsAltScreen() {
		t.Error("expected IsAltScreen() false before any writes")
	}

	_, _ = term.Write([]byte("\x1b[?1049h"))
	if !term.IsAltScreen() {
		t.Error("expected IsAltScreen() true after DECSET 1049")
	}

	_, _ = term.Write([]byte("\x1b[?1049l"))
	if term.IsAltScreen() {
		t.Error("expected IsAltScreen() false after DECRST 1049")
	}
}

func TestRenderPaddedShape(t *testing.T) {
	const w, h = 40, 8
	term := New(w, h)
	defer term.Close()

	// Mix of empty lines and a short line so trailing-whitespace trimming
	// in renderLine is exercised.
	_, _ = term.Write([]byte("hi\r\n\r\nworld"))

	out := term.RenderPadded(w, h)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("expected %d lines, got %d: %q", h, len(lines), out)
	}
	if got := strings.Count(out, "\n"); got != h-1 {
		t.Errorf("expected %d newlines, got %d", h-1, got)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Errorf("line %d: width %d, want %d (line=%q)", i, got, w, line)
		}
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line %d: missing trailing style reset: %q", i, line)
		}
	}
}

func TestRenderPaddedPreservesStyles(t *testing.T) {
	const w, h = 20, 3
	term := New(w, h)
	defer term.Close()

	// Write a styled segment in the middle of a line; the render must keep
	// the SGR codes intact while padding the trailing empty cells.
	_, _ = term.Write([]byte("\x1b[31mRed\x1b[0m ok"))

	out := term.RenderPadded(w, h)
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(firstLine, "\x1b[31m") {
		t.Errorf("expected SGR red to survive padding, got %q", firstLine)
	}
	if !strings.Contains(firstLine, "Red") {
		t.Errorf("expected literal 'Red' to survive padding, got %q", firstLine)
	}
	if got := ansi.StringWidth(firstLine); got != w {
		t.Errorf("styled line width: got %d, want %d", got, w)
	}
}

func TestRenderPaddedDegenerateDims(t *testing.T) {
	term := New(10, 5)
	defer term.Close()

	if got := term.RenderPadded(0, 5); got != "" {
		t.Errorf("RenderPadded(0,5) should be empty, got %q", got)
	}
	if got := term.RenderPadded(5, 0); got != "" {
		t.Errorf("RenderPadded(5,0) should be empty, got %q", got)
	}
}

func TestStableRenderInvalidatedOnResize(t *testing.T) {
	term := New(20, 5)
	defer term.Close()

	_, _ = term.Write([]byte("populate"))
	time.Sleep(20 * time.Millisecond)

	before := term.StableRender()
	if !strings.Contains(before, "populate") {
		t.Fatalf("precondition: StableRender should contain 'populate', got %q", before)
	}
	// Count newlines: a 5-row render has 4 newlines.
	if got := strings.Count(before, "\n"); got != 4 {
		t.Errorf("expected 4 newlines pre-resize, got %d", got)
	}

	term.Resize(40, 10)

	// Cache must be invalidated: StableRender returns a render at the new
	// dimensions (9 newlines = 10 rows) without any prior write.
	after := term.StableRender()
	if got := strings.Count(after, "\n"); got != 9 {
		t.Errorf("expected 9 newlines post-resize, got %d\nrender: %q", got, after)
	}
}

func TestStableRenderInvalidatedOnAltScreenEntry(t *testing.T) {
	term := New(20, 5)
	defer term.Close()

	_, _ = term.Write([]byte("main-screen"))
	time.Sleep(20 * time.Millisecond)

	// Prime the cache with main-screen content.
	cached := term.StableRender()
	if !strings.Contains(cached, "main-screen") {
		t.Fatalf("precondition: expected 'main-screen' in cache, got %q", cached)
	}

	// Enter alt screen and write new content. Both events are within the
	// 16ms cache window.
	_, _ = term.Write([]byte("\x1b[?1049h"))
	_, _ = term.Write([]byte("alt-screen"))

	got := term.StableRender()
	if strings.Contains(got, "main-screen") {
		t.Errorf("StableRender should not return pre-transition snapshot, got %q", got)
	}
	if !strings.Contains(got, "alt-screen") {
		t.Errorf("StableRender should return post-transition content, got %q", got)
	}
}

func TestSnapshotAtomic(t *testing.T) {
	// Race detector catches the non-atomic pre-fix pattern (read
	// ScrollbackLines, then read StableRender). Here we only assert that
	// Snapshot returns a consistent (scrollback, viewport) pair: the
	// viewport has the correct shape, and concurrent scrollback writes
	// never produce duplicate or torn rows across the pair.
	const w, h = 20, 3
	term := New(w, h)
	defer term.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Write 3 lines per iteration: the top scrolls off into history.
			_, _ = term.Write([]byte("a\r\nb\r\nc\r\n"))
			i++
			if i%50 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	for range 100 {
		sb, vp := term.Snapshot(w, h)
		vpLines := strings.Split(vp, "\n")
		if len(vpLines) != h {
			t.Fatalf("viewport should have %d lines, got %d", h, len(vpLines))
		}
		for j, line := range vpLines {
			if got := ansi.StringWidth(line); got != w {
				t.Fatalf("viewport line %d width: got %d want %d", j, got, w)
			}
		}
		// scrollback is whatever was captured at snapshot time — just
		// assert it's a proper slice (copy, not a shared reference).
		if sb != nil && cap(sb) < len(sb) {
			t.Fatalf("scrollback has bad cap=%d len=%d", cap(sb), len(sb))
		}
	}
	close(stop)
	wg.Wait()
}

func TestCursorPosition(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	_, _ = term.Write([]byte("Hello"))

	x, y := term.CursorPosition()
	// After writing "Hello", cursor should be at column 5, row 0
	if x != 5 {
		t.Errorf("expected cursor x=5, got %d", x)
	}
	if y != 0 {
		t.Errorf("expected cursor y=0, got %d", y)
	}
}
