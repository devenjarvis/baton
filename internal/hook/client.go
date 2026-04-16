package hook

import (
	"encoding/json"
	"net"
	"time"
)

// SendEvent dials the server at socketPath, writes e as a newline-terminated
// JSON message, and closes the connection.
//
// Any error is returned to the caller, but hook-path callers should treat it
// as non-fatal — hook failures must not block Claude.
func SendEvent(socketPath string, e Event) error {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Bound the write so a wedged server can't block the hook process forever.
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	_, err = conn.Write(data)
	return err
}
