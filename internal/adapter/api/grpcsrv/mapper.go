package grpcsrv

import (
	"time"

	pb "github.com/Serajian/srosha/gen/notification/v1"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file is the whole of the translation between the wire and the core, in
// both directions. Nothing else in this package builds a domain value or reads
// one, so the rules about what a request may say live in one place.
//
// Inbound is validated; outbound is not. A request comes from a stranger and
// every field is a claim. A domain value was built by our own constructors and
// has already been checked -- checking it again on the way out would only
// invent a way to fail while answering.

// --- inbound -----------------------------------------------------------------

// toPriority reads what the caller asked for. UNSPECIFIED means they did not
// say, which is NORMAL: proto3 cannot tell an absent field from one set to its
// zero value, so the zero has to mean "no answer" rather than a real priority.
func toPriority(p pb.Priority) (shared.Priority, error) {
	switch p {
	case pb.Priority_PRIORITY_UNSPECIFIED, pb.Priority_PRIORITY_NORMAL:
		return shared.PriorityNormal, nil
	case pb.Priority_PRIORITY_HIGH:
		return shared.PriorityHigh, nil
	case pb.Priority_PRIORITY_CRITICAL:
		return shared.PriorityCritical, nil
	default:
		return 0, errs.InvalidInputErr("unknown priority").WithErr(shared.ErrInvalidID)
	}
}

// toChannel refuses UNSPECIFIED rather than defaulting. There is no sensible
// channel to guess: sending a message somewhere the caller did not ask for is
// worse than telling them they forgot to choose.
func toChannel(c pb.Channel) (shared.Channel, error) {
	switch c {
	case pb.Channel_CHANNEL_EMAIL:
		return shared.ChannelEmail, nil
	case pb.Channel_CHANNEL_TELEGRAM:
		return shared.ChannelTelegram, nil
	case pb.Channel_CHANNEL_BALE:
		return shared.ChannelBale, nil
	case pb.Channel_CHANNEL_WHATSAPP:
		return shared.ChannelWhatsApp, nil
	case pb.Channel_CHANNEL_UNSPECIFIED:
		return "", errs.InvalidInputErr("channel is required").
			WithErr(shared.ErrUnknownChannel)
	default:
		return "", errs.InvalidInputErr("unknown channel").
			WithErr(shared.ErrUnknownChannel)
	}
}

// toTime turns an absent timestamp into a nil pointer rather than the zero
// time. The domain reads nil as "never expires"; the zero time would read as
// "expired in 1970" and refuse every message that did not set one.
func toTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

// toSubmitCommand builds the command from a request and the authenticated
// source.
//
// sourceID is a separate argument and never a field of the request: read from
// the body it would be whatever the caller typed, and one customer could send
// as another. Here that is structurally impossible.
func toSubmitCommand(sourceID string, req *pb.SubmitRequest) (usecase.SubmitCommand, error) {
	var cmd usecase.SubmitCommand

	priority, err := toPriority(req.GetPriority())
	if err != nil {
		return cmd, err
	}

	routes := make([]source.Route, 0, len(req.GetRoutes()))
	senders := make(map[shared.Channel]string, len(req.GetRoutes()))

	for _, r := range req.GetRoutes() {
		channel, err := toChannel(r.GetChannel())
		if err != nil {
			return cmd, err
		}
		routes = append(routes, source.Route{Channel: channel, Address: r.GetAddress()})

		// Senders is keyed by channel because one message sends once per
		// channel. Two routes on one channel naming different identities is a
		// request that contradicts itself, so the last one would silently win
		// -- refuse it instead.
		if name := r.GetSender(); name != "" {
			if existing, ok := senders[channel]; ok && existing != name {
				return cmd, errs.InvalidInputErr("two senders named for one channel").
					WithErr(shared.ErrUnknownChannel)
			}
			senders[channel] = name
		}
	}

	cmd.SourceID = sourceID
	cmd.IdempotencyKey = req.GetIdempotencyKey()
	cmd.Title = req.GetTitle()
	cmd.Body = req.GetBody()
	cmd.Priority = priority
	cmd.ExpireAt = toTime(req.GetExpireAt())
	cmd.Metadata = req.GetMetadata()
	cmd.Routes = routes
	cmd.Senders = senders
	return cmd, nil
}

// toCursor reads a page request. Both zero values are meaningful and neither is
// an error: no token is the first page, and no limit is the default. The domain
// clamps an oversized limit rather than refusing it.
func toCursor(p *pb.Page) (shared.Cursor, error) {
	if p == nil {
		return shared.Cursor{}, nil
	}

	c := shared.Cursor{Limit: int(p.GetLimit())}
	if after := p.GetAfter(); after != "" {
		id, err := shared.ParseID(after)
		if err != nil {
			return shared.Cursor{}, err
		}
		c.After = &id
	}
	return c, nil
}

// --- outbound ----------------------------------------------------------------

func fromPriority(p shared.Priority) pb.Priority {
	switch p {
	case shared.PriorityNormal:
		return pb.Priority_PRIORITY_NORMAL
	case shared.PriorityHigh:
		return pb.Priority_PRIORITY_HIGH
	case shared.PriorityCritical:
		return pb.Priority_PRIORITY_CRITICAL
	default:
		return pb.Priority_PRIORITY_UNSPECIFIED
	}
}

func fromChannel(c shared.Channel) pb.Channel {
	switch c {
	case shared.ChannelEmail:
		return pb.Channel_CHANNEL_EMAIL
	case shared.ChannelTelegram:
		return pb.Channel_CHANNEL_TELEGRAM
	case shared.ChannelBale:
		return pb.Channel_CHANNEL_BALE
	case shared.ChannelWhatsApp:
		return pb.Channel_CHANNEL_WHATSAPP
	default:
		return pb.Channel_CHANNEL_UNSPECIFIED
	}
}

func fromStatus(s delivery.Status) pb.DeliveryStatus {
	switch s {
	case delivery.StatusPending:
		return pb.DeliveryStatus_DELIVERY_STATUS_PENDING
	case delivery.StatusSent:
		return pb.DeliveryStatus_DELIVERY_STATUS_SENT
	case delivery.StatusFailed:
		return pb.DeliveryStatus_DELIVERY_STATUS_FAILED
	default:
		return pb.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED
	}
}

func fromFailureReason(r delivery.FailureReason) pb.FailureReason {
	switch r {
	case delivery.FailureExpired:
		return pb.FailureReason_FAILURE_REASON_EXPIRED
	case delivery.FailurePermanent:
		return pb.FailureReason_FAILURE_REASON_PERMANENT
	case delivery.FailureMaxAttempts:
		return pb.FailureReason_FAILURE_REASON_MAX_ATTEMPTS
	case delivery.FailureNoSender:
		return pb.FailureReason_FAILURE_REASON_NO_SENDER
	case delivery.FailureNotReachable:
		return pb.FailureReason_FAILURE_REASON_NOT_REACHABLE
	default:
		return pb.FailureReason_FAILURE_REASON_UNSPECIFIED
	}
}

// fromTime leaves an absent time absent. A zero timestamp on the wire would
// read as 1970 to every client rather than as "not set".
func fromTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func fromNotification(n *notification.Notification) *pb.Notification {
	if n == nil {
		return nil
	}
	return &pb.Notification{
		Id:                n.ID.String(),
		IdempotencyKey:    n.IdempotencyKey,
		Title:             n.Title,
		Body:              n.Body,
		RequestedPriority: fromPriority(n.RequestedPriority),
		EffectivePriority: fromPriority(n.EffectivePriority),
		ExpireAt:          fromTime(n.ExpireAt),
		Metadata:          n.Metadata(),
		CreatedAt:         timestamppb.New(n.CreatedAt),
	}
}

// fromDelivery does not send the provider's own error text. It is written for
// operators and can name hosts, limits and internals; the reason says what
// happened with none of it.
func fromDelivery(d *delivery.Delivery) *pb.Delivery {
	if d == nil {
		return nil
	}
	return &pb.Delivery{
		Id:                d.ID.String(),
		Channel:           fromChannel(d.Recipient.Channel),
		Address:           d.Recipient.Address,
		Sender:            d.SenderName,
		Status:            fromStatus(d.Status()),
		FailureReason:     fromFailureReason(d.FailureReason()),
		ProviderMessageId: d.ProviderMessageID(),
		UpdatedAt:         timestamppb.New(d.UpdatedAt()),
	}
}

func fromNotifications(page shared.Pagination[notification.Notification]) []*pb.Notification {
	out := make([]*pb.Notification, 0, len(page.Items))
	for _, n := range page.Items {
		out = append(out, fromNotification(n))
	}
	return out
}

func nextNotificationPageToken(page shared.Pagination[notification.Notification]) string {
	if page.NextCursor == nil {
		return ""
	}
	return page.NextCursor.String()
}

func fromDeliveries(page shared.Pagination[delivery.Delivery]) []*pb.Delivery {
	out := make([]*pb.Delivery, 0, len(page.Items))
	for _, d := range page.Items {
		out = append(out, fromDelivery(d))
	}
	return out
}

// nextPageToken is empty when there is no next page, which is what tells a
// client to stop. It is derived from the cursor rather than stored beside it,
// so the two can never disagree.
func nextPageToken(page shared.Pagination[delivery.Delivery]) string {
	if page.NextCursor == nil {
		return ""
	}
	return page.NextCursor.String()
}

// toCredentialRegistration reads a registration off the wire.
//
// config travels as a string and stays one: the service treats a provider's
// settings as opaque json, and parsing it here only to serialize it again would
// be this layer inventing a shape the rest of the code does not have.
func toCredentialRegistration(
	req *pb.CredentialServiceRegisterRequest,
) (usecase.CredentialRegistration, error) {
	channel, err := toChannel(req.GetChannel())
	if err != nil {
		return usecase.CredentialRegistration{}, err
	}

	var config []byte
	if raw := req.GetConfig(); raw != "" {
		config = []byte(raw)
	}

	return usecase.CredentialRegistration{
		Channel:   channel,
		Name:      req.GetName(),
		Config:    config,
		Secret:    req.GetSecret(),
		IsDefault: req.GetIsDefault(),
	}, nil
}

// fromCredential answers with the identity and never with its secret. There is
// no field for one, which is the point: it cannot be added by accident.
func fromCredential(c *credential.Credential) *pb.Credential {
	if c == nil {
		return nil
	}
	return &pb.Credential{
		Id:        c.ID.String(),
		Channel:   fromChannel(c.Channel),
		Name:      c.Name,
		IsDefault: c.IsDefault(),
		IsActive:  c.IsActive(),
		CreatedAt: timestamppb.New(c.CreatedAt),
	}
}

func fromCredentials(cs []credential.Credential) []*pb.Credential {
	out := make([]*pb.Credential, 0, len(cs))
	for i := range cs {
		out = append(out, fromCredential(&cs[i]))
	}
	return out
}

// onChannel keeps the ones on a channel. A source has a handful of identities,
// so this is a loop rather than a second statement in the database.
func onChannel(cs []credential.Credential, c shared.Channel) []credential.Credential {
	out := make([]credential.Credential, 0, len(cs))
	for i := range cs {
		if cs[i].Channel == c {
			out = append(out, cs[i])
		}
	}
	return out
}

func fromWebhook(w *webhook.Webhook) *pb.Webhook {
	if w == nil {
		return nil
	}
	return &pb.Webhook{
		Id:                  w.ID.String(),
		CallbackUrl:         w.CallbackURL,
		IsActive:            w.IsActive(),
		ConsecutiveFailures: int32(w.ConsecutiveFailures()), //nolint:gosec // a count, bounded by the configured limit
		CreatedAt:           timestamppb.New(w.CreatedAt),
		UpdatedAt:           timestamppb.New(w.UpdatedAt),
	}
}
