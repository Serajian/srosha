package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// OperatorMessage is a message as an operator may see it: when, on what, how
// it went. There is no Title and no Body field, so a page that tried to show
// one would not compile.
type OperatorMessage struct {
	ID        string
	Channels  []string
	Failed    int
	Total     int
	CreatedAt time.Time
}

// OperatorDelivery carries MaskedAddress and no raw address, for the same
// reason.
//
// No CreatedAt: the delivery domain does not keep one of its own -- UpdatedAt
// is a settled row's last transition, and a still-pending one's UpdatedAt is
// its creation, which is all the underlying entity ever tracked.
type OperatorDelivery struct {
	ID            string
	Channel       string
	MaskedAddress string
	SenderName    string
	Status        string
	FailureReason string
	LastError     string
	Attempts      int
	UpdatedAt     time.Time
}

// Messages is what an operator sees of a source's own message log: when, on
// what channel, how many of its deliveries failed -- never what it said.
//
// Ordinary operator work, so it takes mayOperate rather than the super_admin
// check: an operator debugging a customer's complaint does not need the
// roster, only this.
func (o *Operators) Messages(
	ctx context.Context, actor *user.User, sourceID string,
) ([]OperatorMessage, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}

	rows, err := o.notifications.ListForOperator(ctx, sourceID, MaxOperatorMessages)
	if err != nil {
		return nil, err
	}

	out := make([]OperatorMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, OperatorMessage{
			ID:        r.ID.String(),
			Channels:  r.Channels,
			Failed:    r.Failed,
			Total:     r.Total,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// Deliveries is one message's recipients, as an operator may see them: every
// address run through mask, which is enough to recognize and not enough to
// use -- see its own comment for what "enough" means and where it changes
// for a short address.
//
// It takes the SOURCE as well as the message, and refuses a message that does
// not belong to it. The page reads /sources/A/log?message=X and puts the
// answer under A's heading, so a message id from source B would have shown B's
// recipients as A's -- no error, no log line, and an operator drawing a
// conclusion about the wrong customer.
func (o *Operators) Deliveries(
	ctx context.Context, actor *user.User, sourceID, messageID string,
) ([]OperatorDelivery, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}

	msg, err := o.notifications.ReadByID(ctx, shared.ID(messageID))
	if err != nil {
		return nil, err
	}
	if msg.SourceID != sourceID {
		// Not-found rather than forbidden: an operator may read either source,
		// so nothing is being hidden from them. What is wrong is the pairing,
		// and this message is not one of THIS source's.
		return nil, errs.NotFoundErr("that message is not one of this source's").
			WithErr(notification.ErrNotFound).
			WithStr(fmt.Sprintf("message %q belongs to source %q, not %q",
				messageID, msg.SourceID, sourceID))
	}

	ds, err := o.deliveries.ListByNotificationID(ctx, shared.ID(messageID))
	if err != nil {
		return nil, err
	}

	out := make([]OperatorDelivery, 0, len(ds))
	for i := range ds {
		d := &ds[i]
		out = append(out, OperatorDelivery{
			ID:            d.ID.String(),
			Channel:       string(d.Recipient.Channel),
			MaskedAddress: mask(d.Recipient.Address),
			SenderName:    d.SenderName,
			Status:        string(d.Status()),
			FailureReason: string(d.FailureReason()),
			LastError:     d.LastError(),
			Attempts:      d.Attempts(),
			UpdatedAt:     d.UpdatedAt(),
		})
	}
	return out, nil
}

// Senders is what a source is configured to send as: its own identities on
// each channel, whether each is switched on.
//
// Ordinary operator work, so it takes mayOperate rather than the super_admin
// check, the same as Messages and Deliveries -- approving a source blind to
// what it sends as would defeat the point of looking at it first.
//
// This returns credential.Credential directly rather than an Operator-shaped
// projection like Messages and Deliveries do. Those two exist because the raw
// row carries something that must never reach a page -- a message's body, an
// address in the clear -- and masking is a decision, so it belongs in the use
// case. A credential's secret was never on this type to begin with: it is an
// unexported field with no accessor, so nothing here needs to filter it out
// and no page reading this could show it even by mistake.
func (o *Operators) Senders(
	ctx context.Context, actor *user.User, sourceID string,
) ([]credential.Credential, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}
	return o.credentials.ListBySourceID(ctx, sourceID)
}

// Audit is the newest rows of who did what, across the whole service.
//
// super_admin, unlike every other read here -- and the reason is not on the
// page, it is in the column. The gate records the ACTOR of every act, and a
// customer is the actor of most of them: registering a source, issuing a key,
// revoking one, editing settings all run through Gate.Do with the customer as
// actor, so actor_email holds customers' addresses on the majority of rows.
// Reading this log is therefore reading the roster, one address at a time,
// and the roster is what mayGovernPeople exists to keep from an admin -- see
// operator_people.go. It also resolves a source's owner id, which is all
// admin/source.html shows, back to a person.
//
// So it takes mayGovernPeople. An admin loses nothing they need: Queue,
// AllSources, Source, Messages, Deliveries and Senders are all still theirs,
// and those are what reviewing a source takes.
//
// No filter: this is the newest N rows, full stop. "What happened to this
// source" is a real question and this does not answer it, but nothing here
// has asked it yet -- Queue, AllSources and Source already give an operator a
// source to look at, and grepping one page of rows by eye covers the gap
// until something needs more. Adding a filter nobody has asked for yet is the
// same mistake as the port that grows one method per query: it stops being
// this task's smallest shippable read and becomes a guess at the next one.
func (o *Operators) Audit(ctx context.Context, actor *user.User) ([]AuditEntry, error) {
	if err := o.mayGovernPeople(actor); err != nil {
		return nil, err
	}
	return o.audit.List(ctx, MaxOperatorAudit)
}

// mask keeps enough of an address to recognize and not enough to use.
//
// In the use case rather than in SQL: an adapter returns facts and the core
// decides, and how an address is shown to a person is a decision.
//
// The property this guarantees: a masked value never narrows the original
// down to a small, guessable set. Revealing two characters at each end costs
// four characters of the address -- nothing for an email or a phone number,
// where four characters are a small fraction of a long string, but not
// nothing for a Telegram or Bale chat id, which is numeric and can be five
// or six digits long: revealing four of six leaves a hundred candidates,
// closed in one guess by anyone with context about the customer.
//
// So below minMaskable nothing is revealed at all, full stop. The threshold
// is where the four revealed characters stop being at least half the
// string: at eight characters they are exactly half, and shorter than that
// they are the majority of what's there -- not a mask, just a shorter
// address.
func mask(address string) string {
	const keep = 2
	const minMaskable = keep * 4 // below this, "revealed" would be most of the string
	if len(address) < minMaskable {
		return "…"
	}
	return address[:keep] + "…" + address[len(address)-keep:]
}
