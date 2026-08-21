// Package delivery holds one message to one recipient, and the state machine
// that keeps a redelivered message from being sent twice.
package delivery

import (
	"encoding/json"
	"fmt"

	"github.com/Serajian/srosha/pkg/errs"
)

// Status is the state of one delivery.
type Status string

const (
	StatusPending Status = "PENDING"
	StatusSent    Status = "SENT"
	StatusFailed  Status = "FAILED"
)

// transitions is the duplicate guard: the broker may deliver the same message
// twice, and the second attempt is refused here. A transient failure is not a
// transition at all -- nothing is written and the delivery stays PENDING.
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

// FailureReason is a reason, not a state: the program behaves the same for all
// of them, so a new one costs nothing but a constant.
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

// MarshalJSON and UnmarshalJSON keep an unknown status off a wire in either
// direction. A Status that is not in the transition table has nowhere to go and
// nothing downstream knows what to do with it.
func (s Status) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, errs.InternalErr("unknown delivery status").
			WithErr(ErrUnknownStatus).
			WithStr(fmt.Sprintf("got %q", string(s)))
	}
	return json.Marshal(string(s))
}

func (s *Status) UnmarshalJSON(b []byte) error {
	got, err := decode[Status](b, "unknown delivery status", ErrUnknownStatus)
	if err != nil {
		return err
	}
	*s = got
	return nil
}

func (r FailureReason) MarshalJSON() ([]byte, error) {
	if !r.Valid() {
		return nil, errs.InternalErr("unknown failure reason").
			WithErr(ErrUnknownFailureReason).
			WithStr(fmt.Sprintf("got %q", string(r)))
	}
	return json.Marshal(string(r))
}

func (r *FailureReason) UnmarshalJSON(b []byte) error {
	got, err := decode[FailureReason](b, "unknown failure reason", ErrUnknownFailureReason)
	if err != nil {
		return err
	}
	*r = got
	return nil
}

// decode reads a name and refuses anything the type does not recognize.
func decode[T interface {
	~string
	Valid() bool
}](b []byte, msg string, sentinel error) (T, error) {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return "", errs.InvalidInputErr(msg).
			WithErr(sentinel).
			WithStr(fmt.Sprintf("got %s", b))
	}

	got := T(name)
	if !got.Valid() {
		return "", errs.InvalidInputErr(msg).
			WithErr(sentinel).
			WithStr(fmt.Sprintf("got %q", name))
	}
	return got, nil
}
