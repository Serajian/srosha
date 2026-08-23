package nats

import (
	"encoding/json"

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
