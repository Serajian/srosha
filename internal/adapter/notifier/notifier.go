// Package notifier posts a message's outcome to the address a source
// registered, signed so the source can tell it came from us.
//
// It is the only place that knows what the callback looks like on the wire: its
// headers, its signature, and which answers count as delivered.
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Secrets hands over the signing secret for one source.
//
// Declared here rather than taking a map, so the notifier holds no secret of
// its own and cannot be asked for one it was not called about. One secret per
// source, because a shared one would let any source holding it forge a signed
// callback to another.
type Secrets interface {
	SecretFor(sourceID string) (string, bool)
}

// Notifier implements webhook.Notifier.
type Notifier struct {
	client  *http.Client
	secrets Secrets
	now     shared.NowFunc
	log     *slog.Logger
}

func New(
	client *http.Client, secrets Secrets, now shared.NowFunc, log *slog.Logger,
) (*Notifier, error) {
	switch {
	case client == nil:
		return nil, errs.InternalErr("notifier has no http client")
	case secrets == nil:
		return nil, errs.InternalErr("notifier has no signing secrets")
	case now == nil:
		return nil, errs.InternalErr("notifier has no clock")
	case log == nil:
		return nil, errs.InternalErr("notifier has no logger")
	}
	return &Notifier{client: client, secrets: secrets, now: now, log: log}, nil
}

// Notify posts one batch and is never retried -- the query API is the reliable
// way to learn an outcome, and this is the convenience on top of it.
//
// The address belongs to somebody else, so everything about this call is
// guarded elsewhere and deliberately: the client refuses a private address at
// dial time, which is the check that catches a name resolving inwards after it
// passed validation at registration, and it follows no redirects, which is the
// other way that check is dodged.
func (n *Notifier) Notify(ctx context.Context, w *webhook.Webhook, b webhook.Batch) error {
	if w == nil {
		return errs.InternalErr("no callback to post to")
	}

	secret, ok := n.secrets.SecretFor(w.SourceID)
	if !ok || secret == "" {
		// Refused rather than sent unsigned. A callback nobody can verify is
		// one a receiver has to trust on sight, and the missing secret is a
		// deployment mistake worth surfacing -- an unsigned one that quietly
		// worked would hide it until the day somebody started verifying.
		return errs.InternalErr("the callback could not be sent").
			WithStr(fmt.Sprintf("no signing secret for source %q", w.SourceID))
	}

	// The field names come from webhook.Batch's own tags. They are the contract
	// with every client, so they are the domain's and not this adapter's.
	body, err := json.Marshal(b)
	if err != nil {
		return errs.InternalErr("the callback could not be encoded").WithErr(err)
	}

	signature, timestamp := sign(secret, n.now(), body)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		w.CallbackURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return errs.InvalidInputErr("the callback address cannot be called").WithErr(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signatureHeader, signature)
	req.Header.Set(timestampHeader, timestamp)

	// The address IS somebody else's choice -- that is what a callback is -- and
	// the guards for it are the client's, not a check that could be written
	// here: validated at registration, then refused at DIAL time if it resolves
	// onto a private network, which is the only moment that check is true.
	resp, err := n.client.Do(req) //nolint:gosec // see the doc comment above
	if err != nil {
		return errs.UnavailableErr("the callback could not be delivered").
			WithStr(fmt.Sprintf("webhook %s", w.ID)).
			WithErr(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drained so the connection can be reused, bounded because the far end is
	// not ours. Nothing in it is read: a callback answers with a status.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errs.UnavailableErr("the callback was refused").
			WithStr(fmt.Sprintf("webhook %s answered %d", w.ID, resp.StatusCode))
	}
	return nil
}
