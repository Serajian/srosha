package srosha

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// Submit hands srosha a message and returns as soon as it is stored. Sending
// happens afterwards, so this answers in the time it takes to write a row
// rather than the time a provider takes to accept one.
//
// Ask what happened with Get, or register a webhook and be told.
//
// If Message.IdempotencyKey is empty a key is generated for this call, and that
// is what makes retrying safe: a timeout followed by another attempt is the
// same message, not a second one. Two separate Submit calls get two keys and
// therefore two messages, which is correct -- the same alert sent twice on
// purpose is a real thing.
func (c *Client) Submit(ctx context.Context, m Message) (Receipt, error) {
	if len(m.Routes) == 0 {
		return Receipt{}, &Error{
			kind:    ErrInvalidRequest,
			Message: "a message needs at least one route",
		}
	}

	key := m.IdempotencyKey
	if key == "" {
		var err error
		if key, err = newIdempotencyKey(); err != nil {
			return Receipt{}, &Error{kind: ErrInternal, Message: err.Error()}
		}
	}

	req := &pb.SubmitRequest{
		IdempotencyKey: key,
		Title:          m.Title,
		Body:           m.Body,
		Priority:       toPriority(m.Priority),
		ExpireAt:       toTimestamp(m.ExpireAt),
		Metadata:       m.Metadata,
		Routes:         toRoutes(m.Routes),
	}

	var res *pb.SubmitResponse
	err := c.call(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.notifications.Submit(ctx, req)
		return err
	})
	if err != nil {
		return Receipt{}, err
	}

	return Receipt{
		ID:         res.GetId(),
		Priority:   fromPriority(res.GetEffectivePriority()),
		Downgraded: res.GetDowngraded(),
		Duplicate:  res.GetDuplicate(),
	}, nil
}

// newIdempotencyKey is sixteen random bytes as hex. Random rather than derived
// from the message: two identical alerts minutes apart are two messages, and a
// content hash would make the second one vanish.
func newIdempotencyKey() (string, error) {
	raw := make([]byte, idempotencyKeyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("could not generate an idempotency key")
	}
	return hex.EncodeToString(raw), nil
}
