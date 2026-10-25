package domain

import "fmt"

// State is the durable lifecycle state of one unit of scheduled work.
type State string

const (
	StatePending State = "pending"
	StateReady   State = "ready"
	StateRunning State = "running"
	StateWaiting State = "waiting"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// Transition validates an ordinary lifecycle transition. Page advancement is
// handled separately by the backlog adapter and is the only operation that
// returns running work to pending.
func Transition(from, to State) error {
	if !knownState(from) {
		return fmt.Errorf("schedule: unknown current state %q", from)
	}
	if !knownState(to) {
		return fmt.Errorf("schedule: unknown next state %q", to)
	}
	allowed := from == StatePending && to == StateReady ||
		from == StateReady && to == StateRunning ||
		from == StateRunning && (to == StateWaiting || to == StateDone || to == StateFailed)
	if !allowed {
		return fmt.Errorf("schedule: illegal state transition %q -> %q", from, to)
	}
	return nil
}

func knownState(state State) bool {
	switch state {
	case StatePending, StateReady, StateRunning, StateWaiting, StateDone, StateFailed:
		return true
	default:
		return false
	}
}
