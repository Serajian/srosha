package shared

import (
	"encoding/json"
	"fmt"

	"github.com/Serajian/srosha/pkg/errs"
)

// Priority is ordered: PriorityNormal < PriorityHigh < PriorityCritical.
//
// It lives in shared rather than in the notification package because source
// needs it too, for Source.MaxPriority. Putting it in notification would make
// source import notification, while notification already imports source -- an
// import cycle, which Go refuses to compile.
//
// It is an integer rather than a string because the core rule is a comparison:
// requested > sourceMax. With an int that is one instruction; with a string it
// would be a map lookup on every request, and the ordering would live nowhere
// the compiler can see it. Rendering it as "NORMAL"/"HIGH"/"CRITICAL" for
// storage or the wire is the adapter's job.
type Priority int8

const (
	PriorityNormal Priority = iota
	PriorityHigh
	PriorityCritical
)

// priorityNames is the single place the textual form is defined. Both String
// and ParsePriority read it, so the two can never drift apart.
var priorityNames = map[Priority]string{
	PriorityNormal:   "NORMAL",
	PriorityHigh:     "HIGH",
	PriorityCritical: "CRITICAL",
}

func (p Priority) Valid() bool {
	_, ok := priorityNames[p]
	return ok
}

// String renders the canonical name. For an out-of-range value it returns a
// debuggable form rather than an empty string, so a corrupt value shows up in
// a log as Priority(7) instead of vanishing.
func (p Priority) String() string {
	if name, ok := priorityNames[p]; ok {
		return name
	}
	return fmt.Sprintf("Priority(%d)", int8(p))
}

// Clamp caps p at max.
//
// This is only the mechanical half of the silent downgrade rule. Deciding to
// clamp rather than reject the request is the notification aggregate's
// business, and it is the caller of this method. shared knows how to cap a
// priority; it does not know why anyone would want to.
func (p Priority) Clamp(max Priority) Priority {
	if p > max {
		return max
	}
	return p
}

// ParsePriority validates an untrusted string. Same rule as ParseID and
// ParseChannel: use it on input from outside, not on the read path from our
// own database.
func ParsePriority(s string) (Priority, error) {
	for p, name := range priorityNames {
		if name == s {
			return p, nil
		}
	}
	return 0, errs.InvalidInputErr("unknown priority").
		WithErr(ErrUnknownPriority).
		WithStr(fmt.Sprintf("got %q", s))
}

// MarshalJSON writes the name, not the number. In memory an ordered integer is
// what the comparison rule needs; on a wire, "1" tells whoever is reading a
// stuck message nothing at all.
//
// Both directions go through String and ParsePriority, which read the same map,
// so what is written can always be read back.
func (p Priority) MarshalJSON() ([]byte, error) {
	if !p.Valid() {
		return nil, errs.InternalErr("unknown priority").
			WithErr(ErrUnknownPriority).
			WithStr(fmt.Sprintf("got %d", int8(p)))
	}
	return json.Marshal(p.String())
}

func (p *Priority) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return errs.InvalidInputErr("unknown priority").
			WithErr(ErrUnknownPriority).
			WithStr(fmt.Sprintf("got %s", b))
	}

	parsed, err := ParsePriority(name)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
