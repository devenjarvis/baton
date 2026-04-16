package cmd

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devenjarvis/baton/internal/hook"
)

// TestHookSubcommandForwards runs the built baton binary with `hook session-start`
// and asserts the server on the other side receives the event with the right
// AgentID and parsed session_id.
func TestHookSubcommandForwards(t *testing.T) {
	// Find repo root and build the binary into a temp dir.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(thisFile))

	bin := filepath.Join(t.TempDir(), "baton")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building baton: %v\n%s", err, out)
	}

	socket := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := hook.NewServer(socket)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	cmd := exec.Command(bin, "hook", "session-start")
	cmd.Env = append(cmd.Environ(),
		"BATON_HOOK_SOCKET="+socket,
		"BATON_AGENT_ID=test-agent-42",
	)
	cmd.Stdin = strings.NewReader(`{"session_id":"uuid-xyz","cwd":"/tmp/wt"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running hook: %v\n%s", err, out)
	}

	select {
	case e := <-srv.Events():
		if e.Kind != hook.KindSessionStart {
			t.Errorf("kind: got %q, want %q", e.Kind, hook.KindSessionStart)
		}
		if e.AgentID != "test-agent-42" {
			t.Errorf("agent id: got %q, want %q", e.AgentID, "test-agent-42")
		}
		if e.SessionID != "uuid-xyz" {
			t.Errorf("session id: got %q, want %q", e.SessionID, "uuid-xyz")
		}
		if e.CWD != "/tmp/wt" {
			t.Errorf("cwd: got %q, want %q", e.CWD, "/tmp/wt")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hook event")
	}
}

// TestHookSubcommandNoEnv ensures the hook subcommand silently no-ops when
// BATON_HOOK_SOCKET and BATON_AGENT_ID aren't set — this is the case for a
// user running `claude` outside of baton.
func TestHookSubcommandNoEnv(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(thisFile))

	bin := filepath.Join(t.TempDir(), "baton")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building baton: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "hook", "stop")
	// Deliberately no BATON_* env vars.
	cmd.Stdin = strings.NewReader(`{}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0, got err: %v\n%s", err, out)
	}
	if len(out) != 0 {
		t.Errorf("expected no output, got: %q", out)
	}
}

// TestHookSubcommandUnknownEvent ensures unknown event names exit 0 silently.
func TestHookSubcommandUnknownEvent(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(thisFile))

	bin := filepath.Join(t.TempDir(), "baton")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building baton: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "hook", "made-up-event")
	cmd.Stdin = strings.NewReader(`{}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected exit 0 for unknown event, got err: %v\n%s", err, out)
	}
}
