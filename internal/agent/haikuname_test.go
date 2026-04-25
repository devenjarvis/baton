package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeClaude writes a shell script named "claude" into dir and marks it
// executable. It echoes the given stdout content and exits with exitCode.
// On non-unix platforms the test is skipped by the caller.
func writeFakeClaude(t *testing.T, dir, stdout string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude uses /bin/sh; skip on windows")
	}
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" + // drain stdin so the caller doesn't get SIGPIPE
		"printf %s " + shellSingleQuote(stdout) + "\n"
	if exitCode != 0 {
		script += "exit " + itoa(exitCode) + "\n"
	}
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
}

// writeSlowClaude writes a fake claude that sleeps for sleepSecs before
// responding — used to verify context cancellation.
func writeSlowClaude(t *testing.T, dir string, sleepSecs int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude uses /bin/sh; skip on windows")
	}
	script := "#!/bin/sh\ncat >/dev/null\nsleep " + itoa(sleepSecs) + "\necho done\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write slow claude: %v", err)
	}
}

// writeStdinCapturingClaude writes a fake claude that copies stdin to
// stdinFile and then prints stdout. Used to assert the namer pipes the
// rendered instruction verbatim.
func writeStdinCapturingClaude(t *testing.T, dir, stdinFile, stdout string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude uses /bin/sh; skip on windows")
	}
	script := "#!/bin/sh\n" +
		"cat > " + shellSingleQuote(stdinFile) + "\n" +
		"printf %s " + shellSingleQuote(stdout) + "\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write capturing claude: %v", err)
	}
}

// withPATH prepends dir to PATH for the duration of the test.
func withPATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestDefaultBranchNamer_Success(t *testing.T) {
	dir := t.TempDir()
	writeFakeClaude(t, dir, "fix login flow", 0)
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slug, err := namer(ctx, "Summarize this task: we need to fix the login flow")
	if err != nil {
		t.Fatalf("namer returned error: %v", err)
	}
	if slug != "fix-login-flow" {
		t.Errorf("slug = %q, want fix-login-flow", slug)
	}
}

func TestDefaultBranchNamer_PipesInstructionVerbatim(t *testing.T) {
	dir := t.TempDir()
	stdinFile := filepath.Join(dir, "stdin.txt")
	writeStdinCapturingClaude(t, dir, stdinFile, "the-result")
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const instruction = "Custom prompt header.\n\nuser-prompt-text"
	if _, err := namer(ctx, instruction); err != nil {
		t.Fatalf("namer returned error: %v", err)
	}

	got, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(got) != instruction {
		t.Errorf("stdin = %q, want %q", string(got), instruction)
	}
}

func TestDefaultBranchNamer_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	writeFakeClaude(t, dir, "whatever", 1)
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := namer(ctx, "hello")
	if err == nil {
		t.Fatal("expected error from nonzero-exit claude")
	}
}

func TestDefaultBranchNamer_EmptyStdout(t *testing.T) {
	dir := t.TempDir()
	writeFakeClaude(t, dir, "", 0)
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := namer(ctx, "hello")
	if err == nil {
		t.Fatal("expected error when stdout slugs to empty")
	}
}

func TestDefaultBranchNamer_StdoutTooLongTruncates(t *testing.T) {
	// 200-char reply with spaces — slugify should truncate to 40 chars.
	long := strings.Repeat("word ", 40) // 200 chars
	dir := t.TempDir()
	writeFakeClaude(t, dir, long, 0)
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slug, err := namer(ctx, "hello")
	if err != nil {
		t.Fatalf("namer returned error: %v", err)
	}
	if len(slug) > 40 {
		t.Errorf("slug length = %d, want <= 40 (slug=%q)", len(slug), slug)
	}
}

func TestDefaultBranchNamer_ContextTimeout(t *testing.T) {
	dir := t.TempDir()
	writeSlowClaude(t, dir, 10)
	withPATH(t, dir)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := namer(ctx, "hello")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("namer waited %v, expected to be killed near the 200ms timeout", elapsed)
	}
}

func TestDefaultBranchNamer_ClaudeMissing(t *testing.T) {
	// Point PATH at an empty directory so claude cannot be found.
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	namer := DefaultBranchNamer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := namer(ctx, "hello")
	if err == nil {
		t.Fatal("expected error when claude is absent from PATH")
	}
}
