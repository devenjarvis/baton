package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/devenjarvis/baton/internal/hook"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:   "hook <event>",
	Short: "Forward a Claude Code hook event to the running baton process",
	Long: `hook is invoked by Claude Code via the settings file baton writes before
spawning a session. It reads the Claude hook JSON payload on stdin and
forwards it to the baton TUI over the unix socket named by BATON_HOOK_SOCKET.

This command is not intended to be run by humans. Output is kept silent —
Claude interprets stdout from hooks as feedback. Errors go to stderr. Exit
code is always 0 so hook failures never block Claude.`,
	Args: cobra.ExactArgs(1),
	RunE: runHook,
	// Silence Cobra's usage printing on error — a stray usage dump would be
	// interpreted by Claude as hook feedback.
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.AddCommand(hookCmd)
}

func runHook(cmd *cobra.Command, args []string) error {
	// Resolve the event kind. Unknown events exit 0 silently so we don't break
	// Claude if a future hook name is wired in by accident.
	var kind hook.Kind
	switch args[0] {
	case "session-start":
		kind = hook.KindSessionStart
	case "stop":
		kind = hook.KindStop
	case "session-end":
		kind = hook.KindSessionEnd
	default:
		return nil
	}

	socketPath := os.Getenv("BATON_HOOK_SOCKET")
	agentID := os.Getenv("BATON_AGENT_ID")
	// Without the env vars there's no route to any running baton — exit
	// silently so running `claude` outside of baton doesn't spew errors.
	if socketPath == "" || agentID == "" {
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		fmt.Fprintln(os.Stderr, "baton hook: reading stdin:", err)
		return nil
	}

	// Parse just the fields we route on; keep the rest in Raw so the server
	// can inspect extras if it cares later.
	var payload struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			// Claude may send a non-JSON payload or nothing at all; that's fine.
			payload.SessionID = ""
		}
	}

	e := hook.Event{
		Kind:      kind,
		AgentID:   agentID,
		SessionID: payload.SessionID,
		CWD:       payload.CWD,
		Raw:       json.RawMessage(raw),
	}

	if err := hook.SendEvent(socketPath, e); err != nil {
		fmt.Fprintln(os.Stderr, "baton hook: forwarding event:", err)
	}
	return nil
}
