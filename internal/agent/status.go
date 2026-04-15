package agent

import "time"

// Status represents the current state of an agent.
type Status int

const (
	StatusStarting Status = iota
	StatusActive
	StatusIdle
	StatusDone
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusStarting:
		return "Starting"
	case StatusActive:
		return "Active"
	case StatusIdle:
		return "Idle"
	case StatusDone:
		return "Done"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// Symbol returns a single-character symbol for the status.
func (s Status) Symbol() string {
	switch s {
	case StatusStarting:
		return "◎"
	case StatusActive:
		return "●"
	case StatusIdle:
		return "○"
	case StatusDone:
		return "✓"
	case StatusError:
		return "✗"
	default:
		return "?"
	}
}

const (
	idleTimeout          = 3 * time.Second
	composingIdleTimeout = 30 * time.Second

	// visualStabilityWindow is the amount of time the rendered screen must
	// remain unchanged before an agent is considered visually stable (nothing
	// animating, no output churn).
	visualStabilityWindow = 2 * time.Second

	// stuckFallbackTimeout is the grace period after which a silent agent
	// (no PTY output) is treated as stuck, even if the primary stability
	// signal never trips.
	stuckFallbackTimeout = 60 * time.Second
)

// Exported mirrors of the stability constants for callers outside this package
// (e.g., the TUI chime trigger) that need to compare against the same window.
const (
	VisualStabilityWindow = visualStabilityWindow
	StuckFallbackTimeout  = stuckFallbackTimeout
)
