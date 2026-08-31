package srosha

import (
	"strings"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file is the whole of the translation between this package's types and
// the wire. Nothing else here builds a protobuf message or reads one, which is
// what keeps the promise that protobuf never reaches a customer.
//
// Every enum goes out by name and comes back by name. Going out, a value this
// build does not know becomes UNSPECIFIED and the service says what it thinks;
// coming back, it keeps whatever the service called it. That asymmetry is
// deliberate: guessing on the way out would send something a customer did not
// ask for, while guessing on the way in would hide that the service has grown.

func toTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// fromTimestamp turns an absent timestamp into the zero time rather than a
// pointer. A caller reads zero as "unset", which is what every field using it
// means.
func fromTimestamp(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

var channelsOut = map[Channel]pb.Channel{
	ChannelEmail:    pb.Channel_CHANNEL_EMAIL,
	ChannelTelegram: pb.Channel_CHANNEL_TELEGRAM,
	ChannelBale:     pb.Channel_CHANNEL_BALE,
	ChannelWhatsApp: pb.Channel_CHANNEL_WHATSAPP,
	ChannelMatrix:   pb.Channel_CHANNEL_MATRIX,
	ChannelGotify:   pb.Channel_CHANNEL_GOTIFY,
	ChannelFCM:      pb.Channel_CHANNEL_FCM,
	ChannelAPNs:     pb.Channel_CHANNEL_APNS,
}

func toChannel(c Channel) pb.Channel { return channelsOut[c] }

// fromChannel reads the name off the enum rather than a table of our own, so a
// channel added to the service after this build was made arrives as its own
// name instead of as an empty string.
func fromChannel(c pb.Channel) Channel {
	if c == pb.Channel_CHANNEL_UNSPECIFIED {
		return ""
	}
	return Channel(strings.ToLower(strings.TrimPrefix(c.String(), "CHANNEL_")))
}

var prioritiesOut = map[Priority]pb.Priority{
	PriorityDefault:  pb.Priority_PRIORITY_UNSPECIFIED,
	PriorityNormal:   pb.Priority_PRIORITY_NORMAL,
	PriorityHigh:     pb.Priority_PRIORITY_HIGH,
	PriorityCritical: pb.Priority_PRIORITY_CRITICAL,
}

func toPriority(p Priority) pb.Priority { return prioritiesOut[p] }

func fromPriority(p pb.Priority) Priority {
	if p == pb.Priority_PRIORITY_UNSPECIFIED {
		return PriorityDefault
	}
	return Priority(strings.ToLower(strings.TrimPrefix(p.String(), "PRIORITY_")))
}

var windowsOut = map[Window]pb.Window{
	Everything: pb.Window_WINDOW_UNSPECIFIED,
	LastHour:   pb.Window_WINDOW_LAST_HOUR,
	LastDay:    pb.Window_WINDOW_LAST_DAY,
	LastWeek:   pb.Window_WINDOW_LAST_WEEK,
	LastMonth:  pb.Window_WINDOW_LAST_MONTH,
}

func toWindow(w Window) pb.Window { return windowsOut[w] }

func fromStatus(s pb.DeliveryStatus) Status {
	if s == pb.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED {
		return ""
	}
	return Status(strings.ToLower(strings.TrimPrefix(s.String(), "DELIVERY_STATUS_")))
}

func fromReason(r pb.FailureReason) FailureReason {
	if r == pb.FailureReason_FAILURE_REASON_UNSPECIFIED {
		return FailureNone
	}
	return FailureReason(strings.ToLower(strings.TrimPrefix(r.String(), "FAILURE_REASON_")))
}

func toRoutes(rs []Route) []*pb.Route {
	out := make([]*pb.Route, 0, len(rs))
	for _, r := range rs {
		out = append(out, &pb.Route{
			Channel: toChannel(r.Channel),
			Address: r.Address,
			Sender:  r.Sender,
		})
	}
	return out
}

func fromNotification(n *pb.Notification) Notification {
	if n == nil {
		return Notification{}
	}
	return Notification{
		ID:             n.GetId(),
		IdempotencyKey: n.GetIdempotencyKey(),
		Title:          n.GetTitle(),
		Body:           n.GetBody(),
		Requested:      fromPriority(n.GetRequestedPriority()),
		Priority:       fromPriority(n.GetEffectivePriority()),
		ExpireAt:       fromTimestamp(n.GetExpireAt()),
		Metadata:       n.GetMetadata(),
		CreatedAt:      fromTimestamp(n.GetCreatedAt()),
	}
}

func fromDeliveries(ds []*pb.Delivery) []Delivery {
	out := make([]Delivery, 0, len(ds))
	for _, d := range ds {
		out = append(out, Delivery{
			ID:                d.GetId(),
			Channel:           fromChannel(d.GetChannel()),
			Address:           d.GetAddress(),
			Sender:            d.GetSender(),
			Status:            fromStatus(d.GetStatus()),
			Reason:            fromReason(d.GetFailureReason()),
			ProviderMessageID: d.GetProviderMessageId(),
			UpdatedAt:         fromTimestamp(d.GetUpdatedAt()),
		})
	}
	return out
}
