package srosha

import (
	"context"
	"iter"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
)

// Get answers "what happened to my message": the message itself and one outcome
// per recipient.
//
// Deliveries are paged, because a message to a mailing list has more recipients
// than fit in one answer. Get walks all of them.
func (c *Client) Get(ctx context.Context, id string) (Result, error) {
	var (
		out   Result
		token string
		first = true
	)

	for first || token != "" {
		first = false

		var res *pb.NotificationServiceGetResponse
		err := c.call(ctx, func(ctx context.Context) error {
			var err error
			res, err = c.notifications.Get(ctx, &pb.NotificationServiceGetRequest{
				Id:   id,
				Page: &pb.Page{After: token},
			})
			return err
		})
		if err != nil {
			return Result{}, err
		}

		out.Notification = fromNotification(res.GetNotification())
		out.Deliveries = append(out.Deliveries, fromDeliveries(res.GetDeliveries())...)
		token = res.GetNextPageToken()
	}
	return out, nil
}

// List walks what this source sent, newest first, as far back as the window
// reaches.
//
//	for n, err := range c.List(ctx, srosha.LastWeek) {
//	    if err != nil {
//	        return err
//	    }
//	    fmt.Println(n.ID, n.Title)
//	}
//
// The window is a closed set and not two timestamps, because srosha is not an
// archive: past its retention age a message is deleted, and a range reaching
// beyond that would come back short with nothing saying so. Everything is the
// zero value and means as far back as the service keeps, which is the only
// answer that is right whatever that age is set to.
//
// A window longer than the deployment keeps is refused, and the error names the
// real limit.
//
// Pages are fetched as the loop asks for them, so stopping early stops the
// requests. An error ends the iteration: the second value is set once and the
// loop is done.
func (c *Client) List(ctx context.Context, window Window) iter.Seq2[Notification, error] {
	return func(yield func(Notification, error) bool) {
		var (
			token string
			first = true
		)

		for first || token != "" {
			first = false

			var res *pb.NotificationServiceListResponse
			err := c.call(ctx, func(ctx context.Context) error {
				var err error
				res, err = c.notifications.List(ctx, &pb.NotificationServiceListRequest{
					Window: toWindow(window),
					Page:   &pb.Page{After: token},
				})
				return err
			})
			if err != nil {
				yield(Notification{}, err)
				return
			}

			for _, n := range res.GetNotifications() {
				if !yield(fromNotification(n), nil) {
					return
				}
			}
			token = res.GetNextPageToken()
		}
	}
}
