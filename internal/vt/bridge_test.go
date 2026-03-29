package vt

import (
	"strings"
	"testing"
	"time"
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

func TestRenderRegion(t *testing.T) {
	term := New(80, 5)
	defer term.Close()

	// Write text on different lines
	term.Write([]byte("Line1\r\nLine2\r\nLine3\r\nLine4\r\nLine5"))

	region := term.RenderRegion(1, 3)
	lines := strings.Split(region, "\n")

	// Region should have lines 1-3 (3 lines)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines in region, got %d", len(lines))
	}
}

func TestCursorPosition(t *testing.T) {
	term := New(80, 24)
	defer term.Close()

	term.Write([]byte("Hello"))

	x, y := term.CursorPosition()
	// After writing "Hello", cursor should be at column 5, row 0
	if x != 5 {
		t.Errorf("expected cursor x=5, got %d", x)
	}
	if y != 0 {
		t.Errorf("expected cursor y=0, got %d", y)
	}
}
