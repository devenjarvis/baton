package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// haikuLogPath returns the diagnostic log path for the given repo. The log
// lives under <repoPath>/.baton/logs/ alongside the hook socket.
func haikuLogPath(repoPath string) string {
	return filepath.Join(repoPath, ".baton", "logs", "haiku.log")
}

// haikuLogMaxBytes is the size threshold past which haikuLog truncates the
// file. The log is diagnostic, not audit; losing history on truncation is
// acceptable in exchange for bounded disk use.
const haikuLogMaxBytes int64 = 1 << 20 // 1 MiB

var haikuLogMu sync.Mutex

// haikuLog appends a single diagnostic line about the branch-namer flow to
// <repoPath>/.baton/logs/haiku.log. Best-effort: any I/O error is silently
// dropped so logging never disrupts the TUI. The line is suffixed with "\n"
// if it doesn't already end in one.
//
// When the log exceeds haikuLogMaxBytes, the file is truncated before the
// next write — the log is for diagnosing the most recent failures, not for
// long-term audit, so a hard truncate is simpler than rotating files.
func haikuLog(repoPath, line string) {
	if repoPath == "" {
		return
	}
	haikuLogMu.Lock()
	defer haikuLogMu.Unlock()

	path := haikuLogPath(repoPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	if info, err := os.Stat(path); err == nil && info.Size() > haikuLogMaxBytes {
		_ = os.Truncate(path, 0)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	if line == "" || line[len(line)-1] != '\n' {
		line += "\n"
	}
	_, _ = f.WriteString(line)
}

// haikuLogAttempt formats and writes one per-attempt diagnostic line.
func haikuLogAttempt(repoPath, sessionID string, attempt int, suffix string, err error, took time.Duration) {
	status := "err"
	detail := ""
	switch {
	case err != nil:
		detail = fmt.Sprintf(" err=%q", err.Error())
	case suffix == "":
		detail = " err=\"empty suffix\""
	default:
		status = "ok"
		detail = fmt.Sprintf(" suffix=%s", suffix)
	}
	haikuLog(repoPath, fmt.Sprintf(
		"%s session=%s attempt=%d status=%s took=%s%s",
		time.Now().UTC().Format(time.RFC3339),
		sessionID, attempt, status, took.Round(time.Millisecond), detail,
	))
}

// haikuLogOutcome formats and writes one final-outcome diagnostic line for
// the whole rename sequence (across all retries).
func haikuLogOutcome(repoPath, sessionID, suffix string, err error, took time.Duration) {
	if err == nil && suffix != "" {
		haikuLog(repoPath, fmt.Sprintf(
			"%s session=%s status=ok suffix=%s took=%s",
			time.Now().UTC().Format(time.RFC3339),
			sessionID, suffix, took.Round(time.Millisecond),
		))
		return
	}
	detail := "unknown"
	if err != nil {
		detail = err.Error()
	}
	haikuLog(repoPath, fmt.Sprintf(
		"%s session=%s status=fail err=%q took=%s",
		time.Now().UTC().Format(time.RFC3339),
		sessionID, detail, took.Round(time.Millisecond),
	))
}
