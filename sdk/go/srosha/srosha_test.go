package srosha_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
	"github.com/Serajian/srosha/sdk/go/srosha"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const apiKey = "srosha_not-a-real-key"

// --- a server to talk to -----------------------------------------------------

// fake is srosha, as far as these tests are concerned. It records what arrived
// and answers with whatever the test set.
type fake struct {
	pb.UnimplementedNotificationServiceServer

	mu sync.Mutex

	// what it saw
	auth     []string
	submits  []*pb.SubmitRequest
	lists    []*pb.NotificationServiceListRequest
	gets     []*pb.NotificationServiceGetRequest
	attempts int

	// what it answers
	submitErr   error
	listErr     error
	failFirst   int // fail this many calls before succeeding
	listPages   []*pb.NotificationServiceListResponse
	getPages    []*pb.NotificationServiceGetResponse
	submitReply *pb.SubmitResponse
}

func (f *fake) record(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		f.auth = append(f.auth, strings.Join(md.Get("authorization"), ","))
	} else {
		f.auth = append(f.auth, "")
	}
	f.attempts++
}

func (f *fake) Submit(
	ctx context.Context, req *pb.SubmitRequest,
) (*pb.SubmitResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(ctx)
	f.submits = append(f.submits, req)

	if f.failFirst > 0 {
		f.failFirst--
		return nil, status.Error(codes.Unavailable, "come back later")
	}
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	if f.submitReply != nil {
		return f.submitReply, nil
	}
	return &pb.SubmitResponse{Id: "01ID", EffectivePriority: pb.Priority_PRIORITY_NORMAL}, nil
}

func (f *fake) List(
	ctx context.Context, req *pb.NotificationServiceListRequest,
) (*pb.NotificationServiceListResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(ctx)
	f.lists = append(f.lists, req)

	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.listPages) == 0 {
		return &pb.NotificationServiceListResponse{}, nil
	}
	page := f.listPages[0]
	f.listPages = f.listPages[1:]
	return page, nil
}

func (f *fake) Get(
	ctx context.Context, req *pb.NotificationServiceGetRequest,
) (*pb.NotificationServiceGetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.record(ctx)
	f.gets = append(f.gets, req)

	if len(f.getPages) == 0 {
		return &pb.NotificationServiceGetResponse{}, nil
	}
	page := f.getPages[0]
	f.getPages = f.getPages[1:]
	return page, nil
}

// dial stands the fake up on an in-memory pipe. No port, no network, no Docker
// -- and still a real gRPC server, so serialization, metadata and status codes
// are exercised rather than mocked away.
func dial(t *testing.T, f *fake, opts ...srosha.Option) *srosha.Client {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterNotificationServiceServer(server, f)

	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	opts = append(opts,
		srosha.WithInsecure(),
		srosha.WithDialOptions(
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)

	// passthrough:// because grpc.NewClient resolves a bare target through DNS,
	// and "bufnet" is a pipe rather than a name anybody can look up.
	c, err := srosha.New(context.Background(), "passthrough:///bufnet", apiKey, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func msg() srosha.Message {
	return srosha.Message{
		Title:  "Hello",
		Body:   "world",
		Routes: []srosha.Route{srosha.EmailTo("a@b.test")},
	}
}

// --- connecting --------------------------------------------------------------

func TestTheKeyGoesOutOnEveryCall(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	if _, err := c.Submit(context.Background(), msg()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for n, err := range c.List(context.Background(), srosha.LastDay) {
		_ = n
		if err != nil {
			t.Fatalf("List: %v", err)
		}
	}

	if len(f.auth) != 2 {
		t.Fatalf("saw %d calls, want 2", len(f.auth))
	}
	for i, got := range f.auth {
		if got != "bearer "+apiKey {
			t.Errorf("call %d authorization = %q, want the bearer key", i, got)
		}
	}
}

// A client is not usable without both halves, and saying so at construction is
// cheaper than a call that is refused.
func TestAClientNeedsAnAddressAndAKey(t *testing.T) {
	if _, err := srosha.New(context.Background(), "", apiKey); err == nil {
		t.Error("New with no address succeeded")
	}
	if _, err := srosha.New(context.Background(), "srosha.test:443", "  "); err == nil {
		t.Error("New with no key succeeded")
	}
}

// --- submit ------------------------------------------------------------------

func TestASubmittedMessageComesBackWithItsReceipt(t *testing.T) {
	f := &fake{submitReply: &pb.SubmitResponse{
		Id:                "01M11",
		EffectivePriority: pb.Priority_PRIORITY_NORMAL,
		Downgraded:        true,
		Duplicate:         false,
	}}
	c := dial(t, f)

	m := msg()
	m.Priority = srosha.PriorityCritical
	m.ExpireAt = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	m.Metadata = map[string]string{"order_id": "42"}
	m.Routes = append(m.Routes, srosha.TelegramTo("123456789").From("alerts"))

	got, err := c.Submit(context.Background(), m)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got.ID != "01M11" || got.Priority != srosha.PriorityNormal || !got.Downgraded {
		t.Errorf("receipt = %+v", got)
	}

	sent := f.submits[0]
	if sent.GetPriority() != pb.Priority_PRIORITY_CRITICAL {
		t.Errorf("priority = %v, want critical", sent.GetPriority())
	}
	if !sent.GetExpireAt().AsTime().Equal(m.ExpireAt) {
		t.Errorf("expire = %v, want %v", sent.GetExpireAt().AsTime(), m.ExpireAt)
	}
	if sent.GetMetadata()["order_id"] != "42" {
		t.Errorf("metadata = %v, want it carried through", sent.GetMetadata())
	}

	routes := sent.GetRoutes()
	if len(routes) != 2 {
		t.Fatalf("sent %d routes, want 2", len(routes))
	}
	if routes[0].GetChannel() != pb.Channel_CHANNEL_EMAIL {
		t.Errorf("route 0 channel = %v", routes[0].GetChannel())
	}
	if routes[1].GetSender() != "alerts" {
		t.Errorf("route 1 sender = %q, want alerts", routes[1].GetSender())
	}
}

// A message with nowhere to go is refused here, without a round trip: the
// service would say the same thing and charge a request for it.
func TestAMessageWithNoRouteNeverLeaves(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	m := msg()
	m.Routes = nil

	_, err := c.Submit(context.Background(), m)
	if !errors.Is(err, srosha.ErrInvalidRequest) {
		t.Errorf("Submit = %v, want ErrInvalidRequest", err)
	}
	if f.attempts != 0 {
		t.Error("it was sent anyway")
	}
}

// --- idempotency: what makes retrying safe -----------------------------------

func TestAKeyIsGeneratedWhenTheCallerGaveNone(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	if _, err := c.Submit(context.Background(), msg()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got := f.submits[0].GetIdempotencyKey(); got == "" {
		t.Fatal("no idempotency key was sent")
	}
}

func TestTheCallersOwnKeyIsUsedUnchanged(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	m := msg()
	m.IdempotencyKey = "order-42"

	if _, err := c.Submit(context.Background(), m); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got := f.submits[0].GetIdempotencyKey(); got != "order-42" {
		t.Errorf("key = %q, want the caller's", got)
	}
}

// The whole reason a key is generated: three attempts must be one message, not
// three.
func TestEveryAttemptCarriesTheSameKey(t *testing.T) {
	f := &fake{failFirst: 2}
	c := dial(t, f, srosha.WithRetry(3))

	if _, err := c.Submit(context.Background(), msg()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(f.submits) != 3 {
		t.Fatalf("saw %d attempts, want 3", len(f.submits))
	}

	first := f.submits[0].GetIdempotencyKey()
	for i, s := range f.submits {
		if s.GetIdempotencyKey() != first {
			t.Errorf("attempt %d used key %q, want %q", i, s.GetIdempotencyKey(), first)
		}
	}
}

// Two calls are two messages. The same alert sent twice on purpose is a real
// thing, and a content hash would have made the second one vanish.
func TestTwoSubmitsGetTwoKeys(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	for range 2 {
		if _, err := c.Submit(context.Background(), msg()); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	if f.submits[0].GetIdempotencyKey() == f.submits[1].GetIdempotencyKey() {
		t.Error("two separate calls shared a key")
	}
}

func TestAGeneratedKeyIsHexAndLongEnough(t *testing.T) {
	key, err := srosha.NewIdempotencyKey()
	if err != nil {
		t.Fatalf("NewIdempotencyKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key is %d characters, want 32", len(key))
	}
	if strings.TrimLeft(key, "0123456789abcdef") != "" {
		t.Errorf("key = %q, want hex", key)
	}
}

// --- errors ------------------------------------------------------------------

func TestEveryCodeBecomesSomethingACallerCanTest(t *testing.T) {
	cases := map[codes.Code]error{
		codes.InvalidArgument:    srosha.ErrInvalidRequest,
		codes.Unauthenticated:    srosha.ErrUnauthorized,
		codes.PermissionDenied:   srosha.ErrForbidden,
		codes.NotFound:           srosha.ErrNotFound,
		codes.AlreadyExists:      srosha.ErrDuplicate,
		codes.ResourceExhausted:  srosha.ErrRateLimited,
		codes.Unavailable:        srosha.ErrUnavailable,
		codes.DeadlineExceeded:   srosha.ErrTimeout,
		codes.Internal:           srosha.ErrInternal,
		codes.Unimplemented:      srosha.ErrInternal,
		codes.FailedPrecondition: srosha.ErrInternal,
	}
	for code, want := range cases {
		t.Run(code.String(), func(t *testing.T) {
			if got := srosha.KindOf(code); !errors.Is(got, want) {
				t.Errorf("kindOf(%v) = %v, want %v", code, got, want)
			}
		})
	}
}

// The service's own sentence is the only thing it says about what went wrong,
// so losing it would leave a caller with a category and nothing else.
func TestTheServicesMessageSurvives(t *testing.T) {
	f := &fake{submitErr: status.Error(
		codes.InvalidArgument, "this service keeps messages for 7 days")}
	c := dial(t, f)

	_, err := c.Submit(context.Background(), msg())
	if !errors.Is(err, srosha.ErrInvalidRequest) {
		t.Fatalf("Submit = %v, want ErrInvalidRequest", err)
	}
	if !strings.Contains(err.Error(), "keeps messages for 7 days") {
		t.Errorf("error = %q, want the service's own words in it", err)
	}

	var e *srosha.Error
	if !errors.As(err, &e) {
		t.Fatal("errors.As did not find *srosha.Error")
	}
	if e.Message != "this service keeps messages for 7 days" {
		t.Errorf("Message = %q", e.Message)
	}
}

// --- retry -------------------------------------------------------------------

func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"the service is down", status.Error(codes.Unavailable, "no"), 3},
		{"over the limit", status.Error(codes.ResourceExhausted, "no"), 3},
		{"a request that is simply wrong", status.Error(codes.InvalidArgument, "no"), 1},
		{"a key it does not know", status.Error(codes.Unauthenticated, "no"), 1},
		{"not found", status.Error(codes.NotFound, "no"), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{submitErr: c.err}
			cl := dial(t, f, srosha.WithRetry(3))

			if _, err := cl.Submit(context.Background(), msg()); err == nil {
				t.Fatal("Submit: want an error")
			}
			if f.attempts != c.want {
				t.Errorf("tried %d times, want %d", f.attempts, c.want)
			}
		})
	}
}

func TestRetryingCanBeSwitchedOff(t *testing.T) {
	f := &fake{submitErr: status.Error(codes.Unavailable, "no")}
	c := dial(t, f, srosha.WithRetry(1))

	if _, err := c.Submit(context.Background(), msg()); err == nil {
		t.Fatal("Submit: want an error")
	}
	if f.attempts != 1 {
		t.Errorf("tried %d times, want 1", f.attempts)
	}
}

// --- listing -----------------------------------------------------------------

func page(token string, ids ...string) *pb.NotificationServiceListResponse {
	out := &pb.NotificationServiceListResponse{NextPageToken: token}
	for _, id := range ids {
		out.Notifications = append(out.Notifications, &pb.Notification{
			Id:        id,
			Title:     "t",
			CreatedAt: timestamppb.New(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)),
		})
	}
	return out
}

func TestListWalksEveryPage(t *testing.T) {
	f := &fake{listPages: []*pb.NotificationServiceListResponse{
		page("p2", "a", "b"),
		page("p3", "c"),
		page("", "d"),
	}}
	c := dial(t, f)

	var got []string
	for n, err := range c.List(context.Background(), srosha.LastWeek) {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		got = append(got, n.ID)
	}

	if fmt.Sprint(got) != "[a b c d]" {
		t.Errorf("listed %v, want every page in order", got)
	}
	if len(f.lists) != 3 {
		t.Errorf("made %d requests, want 3", len(f.lists))
	}
	if f.lists[1].GetPage().GetAfter() != "p2" {
		t.Errorf("second request carried %q, want the first page's token",
			f.lists[1].GetPage().GetAfter())
	}
}

// Stopping the loop stops the requests: pages are fetched as they are asked
// for, not gathered up front.
func TestStoppingEarlyStopsAsking(t *testing.T) {
	f := &fake{listPages: []*pb.NotificationServiceListResponse{
		page("p2", "a", "b"),
		page("", "c"),
	}}
	c := dial(t, f)

	for n, err := range c.List(context.Background(), srosha.LastWeek) {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if n.ID == "a" {
			break
		}
	}
	if len(f.lists) != 1 {
		t.Errorf("made %d requests after stopping at the first item, want 1", len(f.lists))
	}
}

// A refusal ends the iteration: one pair carrying the error, and then the loop
// is done. A caller who checks err on every step cannot miss it.
func TestAnErrorEndsTheListing(t *testing.T) {
	f := &fake{listErr: status.Error(
		codes.InvalidArgument, "this service keeps messages for 7 days")}
	c := dial(t, f, srosha.WithRetry(1))

	var (
		yields int
		last   error
	)
	for n, err := range c.List(context.Background(), srosha.LastMonth) {
		yields++
		last = err
		if err == nil {
			t.Errorf("yielded a notification %q alongside no error", n.ID)
		}
	}

	if yields != 1 {
		t.Fatalf("the loop ran %d times, want once", yields)
	}
	if !errors.Is(last, srosha.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", last)
	}
	if !strings.Contains(last.Error(), "7 days") {
		t.Errorf("error = %q, want the service's own words", last)
	}
}

// An empty listing yields nothing at all rather than one empty pair.
func TestAnEmptyListingYieldsNothing(t *testing.T) {
	f := &fake{}
	c := dial(t, f)

	for range c.List(context.Background(), srosha.LastWeek) {
		t.Fatal("an empty listing yielded something")
	}
}

func TestTheWindowIsSentAsGiven(t *testing.T) {
	cases := map[srosha.Window]pb.Window{
		srosha.Everything: pb.Window_WINDOW_UNSPECIFIED,
		srosha.LastHour:   pb.Window_WINDOW_LAST_HOUR,
		srosha.LastDay:    pb.Window_WINDOW_LAST_DAY,
		srosha.LastWeek:   pb.Window_WINDOW_LAST_WEEK,
		srosha.LastMonth:  pb.Window_WINDOW_LAST_MONTH,
	}
	for window, want := range cases {
		t.Run(string(window)+"_", func(t *testing.T) {
			f := &fake{}
			c := dial(t, f)

			for range c.List(context.Background(), window) {
				break
			}
			if got := f.lists[0].GetWindow(); got != want {
				t.Errorf("window = %v, want %v", got, want)
			}
		})
	}
}

// --- get ---------------------------------------------------------------------

func TestGetGathersEveryDeliveryPage(t *testing.T) {
	f := &fake{getPages: []*pb.NotificationServiceGetResponse{
		{
			Notification: &pb.Notification{Id: "01M11", Title: "t"},
			Deliveries: []*pb.Delivery{
				{
					Id:      "d1",
					Channel: pb.Channel_CHANNEL_EMAIL,
					Status:  pb.DeliveryStatus_DELIVERY_STATUS_SENT,
				},
			},
			NextPageToken: "p2",
		},
		{
			Notification: &pb.Notification{Id: "01M11", Title: "t"},
			Deliveries: []*pb.Delivery{
				{
					Id:            "d2",
					Channel:       pb.Channel_CHANNEL_APNS,
					Status:        pb.DeliveryStatus_DELIVERY_STATUS_FAILED,
					FailureReason: pb.FailureReason_FAILURE_REASON_NOT_REACHABLE,
				},
			},
		},
	}}
	c := dial(t, f)

	got, err := c.Get(context.Background(), "01M11")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Deliveries) != 2 {
		t.Fatalf("got %d deliveries, want both pages", len(got.Deliveries))
	}
	if got.Deliveries[0].Channel != srosha.ChannelEmail ||
		got.Deliveries[0].Status != srosha.StatusSent {
		t.Errorf("delivery 0 = %+v", got.Deliveries[0])
	}
	if got.Deliveries[1].Reason != srosha.FailureNotReachable {
		t.Errorf("delivery 1 reason = %q, want not_reachable", got.Deliveries[1].Reason)
	}
}
