package nats

import (
	"encoding/json"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// encode turns an event into the bytes that go on the wire.
//
// json, and the field names are the event type's own tags rather than anything
// chosen here: the two binaries are deployed separately and a message published
// by one version is read by another, so the shape of this is a contract between
// releases. Whoever changes those tags is changing that contract.
//
// Priority travels as a name rather than as its iota, because Priority marshals
// itself that way. That matters here more than it does in the database:
// reordering the constants would silently reinterpret every message already in
// the stream, and a name cannot be reinterpreted.
//
// A dispatch event carries identifiers only -- no address, no title, no body.
// It sits in the broker's file storage and in its logs, and none of that is a
// place for somebody's phone number. Whatever event comes next has the same
// rule to keep.
//
// id names the thing being encoded, and is only ever used to say which one
// failed. It never reaches the wire.
func encode(event any, id string) ([]byte, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return nil, errs.InternalErr("event could not be encoded").
			WithStr(id).
			WithErr(err)
	}
	return data, nil
}

// decode reads what a gateway published, possibly a release ago.
//
// A message that will not decode is not going to decode on the next attempt
// either: it was written by a version whose shape we no longer understand, or
// it is not ours at all. The caller terminates it rather than asking for it
// again, and says so in the log -- retrying would occupy the queue forever.
func decode(data []byte) (shared.DispatchEvent, error) {
	var e shared.DispatchEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return e, errs.InternalErr("event could not be decoded").WithErr(err)
	}

	// The id is the only field the dispatcher cannot work without: everything
	// else it needs is read from the row. An event without one is a message
	// nobody can act on.
	if e.DeliveryID.IsZero() {
		return e, errs.InternalErr("event carries no delivery id")
	}
	return e, nil
}
