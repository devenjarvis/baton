package agent

// LifecyclePhase describes where the developer is in the workflow for a session.
// It is orthogonal to agent.Status, which describes the agent process state.
type LifecyclePhase int

const (
	LifecyclePlanning       LifecyclePhase = iota // reserved — no UI yet
	LifecycleInProgress                           // default; agent running or done, not yet marked ready
	LifecycleReadyForReview                       // developer marked it ready
	LifecycleInReview                             // developer committed to reviewing
	LifecycleShipping                             // PR open, waiting for CI/team
	LifecycleComplete                             // PR merged or manually marked done
)

func (p LifecyclePhase) String() string {
	switch p {
	case LifecyclePlanning:
		return "planning"
	case LifecycleInProgress:
		return "in_progress"
	case LifecycleReadyForReview:
		return "ready_for_review"
	case LifecycleInReview:
		return "in_review"
	case LifecycleShipping:
		return "shipping"
	case LifecycleComplete:
		return "complete"
	default:
		return "in_progress"
	}
}

// LifecyclePhaseFromString parses a string produced by String(). Unknown/empty → InProgress.
func LifecyclePhaseFromString(s string) LifecyclePhase {
	switch s {
	case "planning":
		return LifecyclePlanning
	case "ready_for_review":
		return LifecycleReadyForReview
	case "in_review":
		return LifecycleInReview
	case "shipping":
		return LifecycleShipping
	case "complete":
		return LifecycleComplete
	default:
		return LifecycleInProgress
	}
}
