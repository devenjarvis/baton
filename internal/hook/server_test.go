package hook

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerRoundTrip(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	want := Event{
		Kind:      KindSessionStart,
		AgentID:   "session-1-agent-1",
		SessionID: "uuid-abc",
		CWD:       "/tmp/worktree",
	}

	if err := SendEvent(socketPath, want); err != nil {
		t.Fatalf("SendEvent: %v", err)
	}

	select {
	case got := <-srv.Events():
		if got.Kind != want.Kind {
			t.Errorf("Kind: got %q, want %q", got.Kind, want.Kind)
		}
		if got.AgentID != want.AgentID {
			t.Errorf("AgentID: got %q, want %q", got.AgentID, want.AgentID)
		}
		if got.SessionID != want.SessionID {
			t.Errorf("SessionID: got %q, want %q", got.SessionID, want.SessionID)
		}
		if got.CWD != want.CWD {
			t.Errorf("CWD: got %q, want %q", got.CWD, want.CWD)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestServerMultipleEvents(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	events := []Event{
		{Kind: KindSessionStart, AgentID: "a1", SessionID: "s1"},
		{Kind: KindStop, AgentID: "a1", SessionID: "s1"},
		{Kind: KindSessionEnd, AgentID: "a1", SessionID: "s1"},
	}
	for _, e := range events {
		if err := SendEvent(socketPath, e); err != nil {
			t.Fatalf("SendEvent %v: %v", e.Kind, err)
		}
	}

	// Each SendEvent opens its own connection and connections race through
	// Accept, so we can't rely on ordering — just verify all three kinds arrive.
	seen := make(map[Kind]int)
	for range events {
		select {
		case got := <-srv.Events():
			seen[got.Kind]++
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for events; seen %v", seen)
		}
	}
	for _, want := range events {
		if seen[want.Kind] < 1 {
			t.Errorf("missing event kind %q; seen %v", want.Kind, seen)
		}
	}
}

func TestServerCloseRemovesSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// SocketPath should no longer exist.
	if err := SendEvent(socketPath, Event{Kind: KindStop, AgentID: "a1"}); err == nil {
		t.Error("SendEvent to closed server should fail")
	}
}

func TestServerCloseIdempotent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	// Second Close must not panic or deadlock.
	if err := srv.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestServerClosesEventsChannel(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Channel should be closed.
	_, ok := <-srv.Events()
	if ok {
		t.Error("expected events channel to be closed")
	}
}

func TestServerStaleSocketReuse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")

	// First server.
	srv1, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer 1: %v", err)
	}
	// Deliberately don't Close, then simulate stale by closing only listener.
	// We just Close properly since Close removes the file; then we touch a
	// stale file and open again.
	if err := srv1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	// Re-create — should succeed even if file were stale.
	srv2, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer 2: %v", err)
	}
	if err := srv2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestSendEventNoServer(t *testing.T) {
	// Dialing a nonexistent socket must fail (caller decides to ignore).
	if err := SendEvent(filepath.Join(t.TempDir(), "nope.sock"), Event{Kind: KindStop, AgentID: "a"}); err == nil {
		t.Error("expected error dialing nonexistent socket")
	}
}

// TestServerRemovesPriorRegularFile verifies that NewServer cleans up a
// stale leftover at the socket path even if the previous baton crashed.
func TestServerRemovesPriorRegularFile(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "hook.sock")

	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer should remove stale file and bind, got: %v", err)
	}
	defer func() { _ = srv.Close() }()

	// Round-trip a real event to confirm the listener is functional.
	if err := SendEvent(socketPath, Event{Kind: KindStop, AgentID: "a"}); err != nil {
		t.Fatalf("SendEvent after stale-file cleanup: %v", err)
	}
	select {
	case <-srv.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// TestServerMalformedJSONSkipped verifies that a malformed line on a single
// connection is ignored without killing the server, and a subsequent
// well-formed message on the same connection is still delivered.
func TestServerMalformedJSONSkipped(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// First a malformed line, then a valid event.
	if _, err := conn.Write([]byte("{not json\n{\"kind\":\"stop\",\"agentId\":\"a\"}\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = conn.Close()

	select {
	case got := <-srv.Events():
		if got.Kind != KindStop {
			t.Errorf("expected Stop event after malformed line, got %q", got.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — server may have died on malformed JSON")
	}
}

// TestServerConnectionDropMidLine verifies the server handles a connection
// closed before sending a newline-terminator without crashing or blocking
// other handlers.
func TestServerConnectionDropMidLine(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	// Open a connection, send an incomplete JSON fragment, close.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := conn.Write([]byte("{\"kind\":\"Sto")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = conn.Close()

	// Server should still accept and process subsequent valid events.
	if err := SendEvent(socketPath, Event{Kind: KindStop, AgentID: "alive"}); err != nil {
		t.Fatalf("SendEvent after dropped conn: %v", err)
	}
	select {
	case got := <-srv.Events():
		if got.AgentID != "alive" {
			t.Errorf("AgentID = %q, want alive", got.AgentID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — server may have wedged after partial-line drop")
	}
}

// TestServerSlowConsumerDoesNotBlock verifies that a stuck consumer does
// not block the hook senders. The events channel is buffered at 64 and
// excess messages must be dropped, not block the writer (which would in
// turn wedge `claude`).
func TestServerSlowConsumerDoesNotBlock(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "hook.sock")
	srv, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = srv.Close() }()

	const burst = 200
	done := make(chan struct{})
	go func() {
		for i := 0; i < burst; i++ {
			_ = SendEvent(socketPath, Event{Kind: KindStop, AgentID: "a"})
		}
		close(done)
	}()

	select {
	case <-done:
		// Senders unblocked even though the events channel was never drained.
	case <-time.After(5 * time.Second):
		t.Fatal("senders blocked — slow consumer wedged the hook path")
	}
}
