// Package delivery holds one message to one recipient, and the state machine
// that keeps a redelivered message from being sent twice.
package delivery

// Status is the state of one delivery.
type Status string

const (
	StatusPending Status = "PENDING"
	StatusSent    Status = "SENT"
	StatusFailed  Status = "FAILED"
)

// transitions is the state machine as data. PENDING is the only state with
// anywhere to go; both others are final.
//
// This is the duplicate guard: the broker may hand the same message to a worker
// twice, and the second attempt is refused in memory with no extra query.
// A transient failure is NOT a transition -- nothing is written, the message is
// NAKed and the delivery stays PENDING.
var transitions = map[Status][]Status{
	StatusPending: {StatusSent, StatusFailed},
	StatusSent:    {},
	StatusFailed:  {},
}

func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

func (s Status) String() string { return string(s) }

// CanTransitionTo reports whether next is a legal move from s.
func (s Status) CanTransitionTo(next Status) bool {
	for _, allowed := range transitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// IsSettled reports whether this delivery has stopped moving.
func (s Status) IsSettled() bool {
	return s == StatusSent || s == StatusFailed
}

// FailureReason says why a FAILED delivery failed. It is a reason, not a state:
// the program behaves the same for all of them, only the human reading it cares
// about the difference. New reasons cost nothing but a constant.
type FailureReason string

const (
	FailureExpired     FailureReason = "EXPIRED"      // deadline passed before its turn came
	FailureMaxAttempts FailureReason = "MAX_ATTEMPTS" // the broker gave up retrying
	FailurePermanent   FailureReason = "PERMANENT"    // the provider says this will never work
	FailureNoSender    FailureReason = "NO_SENDER"    // no sender registered for the channel
)

func (r FailureReason) Valid() bool {
	switch r {
	case FailureExpired, FailureMaxAttempts, FailurePermanent, FailureNoSender:
		return true
	default:
		return false
	}
}

func (r FailureReason) String() string { return string(r) }
