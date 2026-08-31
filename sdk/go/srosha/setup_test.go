package srosha_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/Serajian/srosha/sdk/go/notification/v1"
	"github.com/Serajian/srosha/sdk/go/srosha"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// setup stands in for the two services a source uses once, at the beginning.
type setup struct {
	pb.UnimplementedCredentialServiceServer
	pb.UnimplementedWebhookServiceServer

	registrations []*pb.CredentialServiceRegisterRequest
	updates       []*pb.CredentialServiceUpdateRequest
	rotations     []*pb.CredentialServiceRotateRequest
	lists         []*pb.CredentialServiceListRequest
	hooks         []*pb.RegisterRequest

	// noSecret is a registration that moved an address rather than creating a
	// callback.
	noSecret bool

	err error
}

func (s *setup) Register(
	_ context.Context, req *pb.CredentialServiceRegisterRequest,
) (*pb.CredentialServiceRegisterResponse, error) {
	s.registrations = append(s.registrations, req)
	if s.err != nil {
		return nil, s.err
	}
	return &pb.CredentialServiceRegisterResponse{Credential: &pb.Credential{
		Id: "01CRED", Channel: req.GetChannel(), Name: req.GetName(),
		IsDefault: req.GetIsDefault(), IsActive: true,
		CreatedAt: timestamppb.Now(),
	}}, nil
}

func (s *setup) List(
	_ context.Context, req *pb.CredentialServiceListRequest,
) (*pb.CredentialServiceListResponse, error) {
	s.lists = append(s.lists, req)
	return &pb.CredentialServiceListResponse{Credentials: []*pb.Credential{
		{
			Id:        "01A",
			Channel:   pb.Channel_CHANNEL_EMAIL,
			Name:      "alerts",
			IsActive:  true,
			IsDefault: true,
		},
		{Id: "01B", Channel: pb.Channel_CHANNEL_EMAIL, Name: "old", IsActive: false},
	}}, nil
}

func (s *setup) Update(
	_ context.Context, req *pb.CredentialServiceUpdateRequest,
) (*pb.CredentialServiceUpdateResponse, error) {
	s.updates = append(s.updates, req)
	return &pb.CredentialServiceUpdateResponse{Credential: &pb.Credential{Id: req.GetId()}}, nil
}

func (s *setup) Rotate(
	_ context.Context, req *pb.CredentialServiceRotateRequest,
) (*pb.CredentialServiceRotateResponse, error) {
	s.rotations = append(s.rotations, req)
	return &pb.CredentialServiceRotateResponse{Credential: &pb.Credential{Id: req.GetId()}}, nil
}

func (s *setup) Deactivate(
	_ context.Context, req *pb.CredentialServiceDeactivateRequest,
) (*pb.CredentialServiceDeactivateResponse, error) {
	return &pb.CredentialServiceDeactivateResponse{
		Credential: &pb.Credential{Id: req.GetId(), IsActive: false},
	}, nil
}

func (s *setup) Activate(
	_ context.Context, req *pb.CredentialServiceActivateRequest,
) (*pb.CredentialServiceActivateResponse, error) {
	return &pb.CredentialServiceActivateResponse{
		Credential: &pb.Credential{Id: req.GetId(), IsActive: true},
	}, nil
}

func (s *setup) SetDefault(
	_ context.Context, req *pb.CredentialServiceSetDefaultRequest,
) (*pb.CredentialServiceSetDefaultResponse, error) {
	return &pb.CredentialServiceSetDefaultResponse{
		Credential: &pb.Credential{Id: req.GetId(), IsDefault: true},
	}, nil
}

// --- webhooks ---

func (s *setup) RegisterWebhook(req *pb.RegisterRequest) *pb.RegisterResponse {
	s.hooks = append(s.hooks, req)
	return &pb.RegisterResponse{
		Webhook: &pb.Webhook{
			Id: "01HOOK", CallbackUrl: req.GetCallbackUrl(), IsActive: true,
		},
		Secret: secretOnFirst(s.noSecret),
	}
}

// hooks is the WebhookService half, kept apart so the method names do not
// collide with CredentialService's.
type hooks struct {
	pb.UnimplementedWebhookServiceServer
	parent *setup
}

func (h hooks) Register(
	_ context.Context, req *pb.RegisterRequest,
) (*pb.RegisterResponse, error) {
	return h.parent.RegisterWebhook(req), nil
}

func (h hooks) Get(
	context.Context, *pb.WebhookServiceGetRequest,
) (*pb.WebhookServiceGetResponse, error) {
	return &pb.WebhookServiceGetResponse{Webhook: &pb.Webhook{
		Id: "01HOOK", CallbackUrl: "https://acme.test/hook",
		IsActive: true, ConsecutiveFailures: 3,
	}}, nil
}

func secretOnFirst(moved bool) string {
	if moved {
		return ""
	}
	return "whsec_first"
}

func (h hooks) RotateSecret(
	context.Context, *pb.RotateSecretRequest,
) (*pb.RotateSecretResponse, error) {
	return &pb.RotateSecretResponse{Secret: "whsec_rotated"}, nil
}

func (h hooks) Deactivate(
	context.Context, *pb.DeactivateRequest,
) (*pb.DeactivateResponse, error) {
	return &pb.DeactivateResponse{Webhook: &pb.Webhook{Id: "01HOOK", IsActive: false}}, nil
}

func (h hooks) Activate(
	context.Context, *pb.ActivateRequest,
) (*pb.ActivateResponse, error) {
	return &pb.ActivateResponse{
		Webhook: &pb.Webhook{Id: "01HOOK", IsActive: true, ConsecutiveFailures: 0},
	}, nil
}

func dialSetup(t *testing.T, s *setup) *srosha.Client {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterCredentialServiceServer(server, s)
	pb.RegisterWebhookServiceServer(server, hooks{parent: s})

	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	c, err := srosha.New(context.Background(), "passthrough:///bufnet", apiKey,
		srosha.WithInsecure(),
		srosha.WithDialOptions(
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// --- what each credential puts on the wire -----------------------------------

// The service parses these for real in the server's own contract_test.go. This
// checks the shape from this side, so a break says which half moved.
func TestEachCredentialSendsItsOwnShape(t *testing.T) {
	cases := []struct {
		name    string
		cred    srosha.Credential
		channel pb.Channel
		secret  string
		fields  map[string]any
	}{
		{
			name:    "telegram carries a token and no settings at all",
			cred:    srosha.TelegramCredential{Token: "111:bot"},
			channel: pb.Channel_CHANNEL_TELEGRAM, secret: "111:bot",
		},
		{
			name:    "bale is the same shape as telegram",
			cred:    srosha.BaleCredential{Token: "222:bot"},
			channel: pb.Channel_CHANNEL_BALE, secret: "222:bot",
		},
		{
			name:    "fcm's whole credential is the file",
			cred:    srosha.FCMCredential{ServiceAccount: `{"type":"service_account"}`},
			channel: pb.Channel_CHANNEL_FCM, secret: `{"type":"service_account"}`,
		},
		{
			name: "email is an account, not a token",
			cred: srosha.SMTPCredential{
				Host: "smtp.acme.test", Port: 587, Username: "bot",
				From: "bot@acme.test", Password: "pw",
			},
			channel: pb.Channel_CHANNEL_EMAIL, secret: "pw",
			fields: map[string]any{
				"host": "smtp.acme.test", "port": float64(587),
				"username": "bot", "from": "bot@acme.test",
			},
		},
		{
			name:    "matrix brings its own homeserver",
			cred:    srosha.MatrixCredential{Homeserver: "https://m.test", Token: "syt"},
			channel: pb.Channel_CHANNEL_MATRIX, secret: "syt",
			fields: map[string]any{"homeserver": "https://m.test"},
		},
		{
			name:    "gotify brings its own server url",
			cred:    srosha.GotifyCredential{ServerURL: "https://g.test", Token: "syt"},
			channel: pb.Channel_CHANNEL_GOTIFY, secret: "syt",
			fields: map[string]any{"server_url": "https://g.test"},
		},
		{
			name:    "whatsapp needs two values",
			cred:    srosha.WhatsAppCredential{PhoneNumberID: "123", Token: "EAAG"},
			channel: pb.Channel_CHANNEL_WHATSAPP, secret: "EAAG",
			fields: map[string]any{"phone_number_id": "123"},
		},
		{
			name: "apns needs four",
			cred: srosha.APNsCredential{
				KeyID: "ABC", TeamID: "TEAM", Topic: "com.acme.app",
				Environment: srosha.APNsSandbox, Key: "p8",
			},
			channel: pb.Channel_CHANNEL_APNS, secret: "p8",
			fields: map[string]any{
				"key_id": "ABC", "team_id": "TEAM",
				"topic": "com.acme.app", "environment": "sandbox",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &setup{}
			cl := dialSetup(t, s)

			if _, err := cl.Credentials.Register(context.Background(), srosha.Registration{
				Name: "alerts", Default: true, Credential: c.cred,
			}); err != nil {
				t.Fatalf("Register: %v", err)
			}

			got := s.registrations[0]
			if got.GetChannel() != c.channel {
				t.Errorf("channel = %v, want %v", got.GetChannel(), c.channel)
			}
			if got.GetSecret() != c.secret {
				t.Errorf("secret was not carried through")
			}
			if !got.GetIsDefault() || got.GetName() != "alerts" {
				t.Errorf("name/default = %q/%t", got.GetName(), got.GetIsDefault())
			}

			if c.fields == nil {
				if got.GetConfig() != "" {
					t.Errorf("config = %q, want none for this channel", got.GetConfig())
				}
				return
			}

			var sent map[string]any
			if err := json.Unmarshal([]byte(got.GetConfig()), &sent); err != nil {
				t.Fatalf("config is not json: %v", err)
			}
			for k, want := range c.fields {
				if sent[k] != want {
					t.Errorf("config[%q] = %v, want %v", k, sent[k], want)
				}
			}
		})
	}
}

// An unset environment goes out empty rather than filled in here.
//
// The default belongs to the service, not to this build: srosha reads an empty
// environment as production, and a client that decided for itself would be a
// second place that had to be changed the day the service changed its mind.
// That it becomes production is proved on the other side, in the server's own
// contract_test.go.
func TestAnUnsetAPNsEnvironmentIsLeftToTheService(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	if _, err := c.Credentials.Register(context.Background(), srosha.Registration{
		Name: "alerts",
		Credential: srosha.APNsCredential{
			KeyID: "ABC", TeamID: "TEAM", Topic: "com.acme.app", Key: "p8",
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !strings.Contains(s.registrations[0].GetConfig(), `"environment":""`) {
		t.Errorf("config = %s, want the environment left empty",
			s.registrations[0].GetConfig())
	}
}

// Every field goes every time, empty ones included, because Update replaces the
// whole settings document rather than patching it. Omitting an empty field
// would make "leave this alone" and "clear this" the same request.
func TestEveryFieldIsSentEvenWhenEmpty(t *testing.T) {
	settings, err := srosha.SMTPCredential{Host: "h", From: "f"}.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	for _, key := range []string{"host", "port", "username", "from"} {
		if !strings.Contains(settings, `"`+key+`"`) {
			t.Errorf("settings = %s, want %q in it", settings, key)
		}
	}
}

// The way out for a channel this build has never heard of.
func TestARawCredentialGoesStraightThrough(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	if _, err := c.Credentials.Register(context.Background(), srosha.Registration{
		Name: "sms",
		Credential: srosha.RawCredential{
			Channel: "sms", Config: `{"line":"3000"}`, Secret: "k",
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := s.registrations[0].GetConfig(); got != `{"line":"3000"}` {
		t.Errorf("config = %q, want it untouched", got)
	}
	// An unknown channel has no enum value, so the service decides rather than
	// this build guessing.
	if got := s.registrations[0].GetChannel(); got != pb.Channel_CHANNEL_UNSPECIFIED {
		t.Errorf("channel = %v, want unspecified for one we do not know", got)
	}
}

// A credential ends up in a log line eventually. None of them may take its
// secret along.
func TestNoCredentialPrintsItsSecret(t *testing.T) {
	creds := []srosha.Credential{
		srosha.TelegramCredential{Token: "SECRETVALUE"},
		srosha.BaleCredential{Token: "SECRETVALUE"},
		srosha.FCMCredential{ServiceAccount: "SECRETVALUE"},
		srosha.SMTPCredential{Host: "h", From: "f", Password: "SECRETVALUE"},
		srosha.MatrixCredential{Homeserver: "https://m", Token: "SECRETVALUE"},
		srosha.GotifyCredential{ServerURL: "https://g", Token: "SECRETVALUE"},
		srosha.WhatsAppCredential{PhoneNumberID: "1", Token: "SECRETVALUE"},
		srosha.APNsCredential{KeyID: "a", TeamID: "b", Topic: "c", Key: "SECRETVALUE"},
		srosha.RawCredential{Channel: "sms", Secret: "SECRETVALUE"},
	}
	for _, cred := range creds {
		t.Run(fmt.Sprintf("%T", cred), func(t *testing.T) {
			if printed := fmt.Sprintf("%v %s %+v", cred, cred, cred); strings.Contains(
				printed,
				"SECRETVALUE",
			) {
				t.Errorf("printed %q", printed)
			}

			// And marshaling it must not either -- a struct reaches a log line
			// through json as often as through %v.
			raw, err := json.Marshal(cred)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if strings.Contains(string(raw), "SECRETVALUE") {
				t.Errorf("marshaled %s", raw)
			}
		})
	}
}

// --- the rest of the credential surface --------------------------------------

func TestRegisterNeedsACredential(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	_, err := c.Credentials.Register(context.Background(), srosha.Registration{Name: "x"})
	if !errors.Is(err, srosha.ErrInvalidRequest) {
		t.Errorf("Register = %v, want ErrInvalidRequest", err)
	}
	if len(s.registrations) != 0 {
		t.Error("it was sent anyway")
	}
}

// Switched-off identities are in the listing, and that is the point: without
// them nobody could turn one back on.
func TestListKeepsTheOnesThatWereSwitchedOff(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	got, err := c.Credentials.List(context.Background(), srosha.ChannelEmail)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %d, want both", len(got))
	}
	if got[0].Name != "alerts" || !got[0].Default || !got[0].Active {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Active {
		t.Error("the switched-off one came back active")
	}
	if s.lists[0].GetChannel() != pb.Channel_CHANNEL_EMAIL {
		t.Errorf("asked for %v, want email", s.lists[0].GetChannel())
	}
}

func TestAnEmptyChannelListsEveryOne(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	if _, err := c.Credentials.List(context.Background(), ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := s.lists[0].GetChannel(); got != pb.Channel_CHANNEL_UNSPECIFIED {
		t.Errorf("channel = %v, want unspecified for all of them", got)
	}
}

// Rotate takes the secret alone, because that is the only half that changes.
func TestRotateSendsOnlyTheSecret(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	if _, err := c.Credentials.Rotate(context.Background(), "01CRED", "new-token"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if s.rotations[0].GetSecret() != "new-token" || s.rotations[0].GetId() != "01CRED" {
		t.Errorf("rotate = %+v", s.rotations[0])
	}
}

// Update sends the settings half and nothing else. A secret set on the
// credential handed to it is ignored, which is documented and worth a test so
// it stays true.
func TestUpdateSendsSettingsAndNotTheSecret(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	_, err := c.Credentials.Update(context.Background(), "01CRED", srosha.SMTPCredential{
		Host: "smtp.new.test", Port: 465, From: "bot@acme.test", Password: "IGNORED",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	sent := s.updates[0].GetConfig()
	if !strings.Contains(sent, "smtp.new.test") {
		t.Errorf("config = %q, want the new host", sent)
	}
	if strings.Contains(sent, "IGNORED") {
		t.Errorf("config = %q, the secret went with it", sent)
	}
}

func TestDeactivateAndActivateComeBackWithTheIdentity(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	off, err := c.Credentials.Deactivate(context.Background(), "01CRED")
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if off.Active {
		t.Error("it came back active")
	}

	on, err := c.Credentials.Activate(context.Background(), "01CRED")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !on.Active {
		t.Error("it came back inactive")
	}

	def, err := c.Credentials.SetDefault(context.Background(), "01CRED")
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if !def.Default {
		t.Error("it did not take the default")
	}
}

func TestARefusedRegistrationIsTyped(t *testing.T) {
	s := &setup{err: status.Error(codes.AlreadyExists, "that name is taken")}
	c := dialSetup(t, s)

	_, err := c.Credentials.Register(context.Background(), srosha.Registration{
		Name: "alerts", Credential: srosha.TelegramCredential{Token: "t"},
	})
	if !errors.Is(err, srosha.ErrDuplicate) {
		t.Errorf("Register = %v, want ErrDuplicate", err)
	}
}

// --- webhooks ----------------------------------------------------------------

func TestAWebhookIsRegisteredAndReadBack(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	got, secret, err := c.Webhooks.Register(context.Background(), "https://acme.test/hook")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if secret != "whsec_first" {
		t.Errorf("secret = %q, want the one and only time it is handed over", secret)
	}
	if got.CallbackURL != "https://acme.test/hook" || !got.Active {
		t.Errorf("webhook = %+v", got)
	}
	if s.hooks[0].GetCallbackUrl() != "https://acme.test/hook" {
		t.Errorf("sent %q", s.hooks[0].GetCallbackUrl())
	}

	read, err := c.Webhooks.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ConsecutiveFailures != 3 {
		t.Errorf("failures = %d, want 3", read.ConsecutiveFailures)
	}
}

func TestAWebhookCanBeSwitchedOffAndBackOn(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	off, err := c.Webhooks.Deactivate(context.Background())
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if off.Active {
		t.Error("it came back active")
	}

	on, err := c.Webhooks.Activate(context.Background())
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !on.Active || on.ConsecutiveFailures != 0 {
		t.Errorf("webhook = %+v, want active with a clean count", on)
	}
}

// --- whoami ------------------------------------------------------------------

type identity struct {
	pb.UnimplementedSourceServiceServer

	res   *pb.WhoamiResponse
	err   error
	calls int
}

func (i *identity) Whoami(
	context.Context, *pb.WhoamiRequest,
) (*pb.WhoamiResponse, error) {
	i.calls++
	if i.err != nil {
		return nil, i.err
	}
	return i.res, nil
}

func dialIdentity(t *testing.T, i *identity, opts ...srosha.Option) *srosha.Client {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterSourceServiceServer(server, i)

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

	c, err := srosha.New(context.Background(), "passthrough:///bufnet", apiKey, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestWhoamiSaysWhoYouAreAndWhatYouMay(t *testing.T) {
	i := &identity{res: &pb.WhoamiResponse{
		Id:                 "01K0SRC0000000000000000000",
		Name:               "acme",
		MaxPriority:        pb.Priority_PRIORITY_HIGH,
		AllowCustomAddress: false,
		DefaultAddresses:   map[string]string{"email": "ops@acme.test"},
		Retention:          durationpb.New(7 * 24 * time.Hour),
		RateLimitPerMinute: 600,
	}}
	c := dialIdentity(t, i)

	me, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	if me.ID != "01K0SRC0000000000000000000" || me.Name != "acme" {
		t.Errorf("me = %+v", me)
	}
	if me.MaxPriority != srosha.PriorityHigh {
		t.Errorf("ceiling = %q, want high", me.MaxPriority)
	}
	if me.AllowCustomAddress {
		t.Error("allow custom address came back true")
	}
	if me.DefaultAddresses[srosha.ChannelEmail] != "ops@acme.test" {
		t.Errorf("default addresses = %v, want them keyed by Channel", me.DefaultAddresses)
	}
	if me.Retention != 7*24*time.Hour {
		t.Errorf("retention = %v, want 7 days", me.Retention)
	}
	if me.RateLimitPerMinute != 600 {
		t.Errorf("rate limit = %d, want 600", me.RateLimitPerMinute)
	}
}

// The duration is what travels, because it is the honest number. This rounds it
// down to a window the service will actually accept.
func TestTheLongestWindowYouMayAskFor(t *testing.T) {
	cases := []struct {
		retention time.Duration
		want      srosha.Window
	}{
		{30 * 24 * time.Hour, srosha.LastMonth},
		{90 * 24 * time.Hour, srosha.LastMonth},
		{10 * 24 * time.Hour, srosha.LastWeek}, // no Window says ten days
		{7 * 24 * time.Hour, srosha.LastWeek},
		{48 * time.Hour, srosha.LastDay},
		{90 * time.Minute, srosha.LastHour},
		{time.Minute, srosha.Everything}, // shorter than any window there is
	}
	for _, c := range cases {
		t.Run(c.retention.String(), func(t *testing.T) {
			me := srosha.Me{Retention: c.retention}
			if got := me.MaxWindow(); got != c.want {
				t.Errorf("MaxWindow() = %q, want %q", got, c.want)
			}
		})
	}
}

// A key srosha does not know arrives as the sentinel a caller can act on --
// which is the point of calling this at startup at all.
func TestWhoamiSurfacesABadKey(t *testing.T) {
	i := &identity{err: status.Error(codes.Unauthenticated, "invalid credentials")}
	c := dialIdentity(t, i, srosha.WithRetry(1))

	_, err := c.Whoami(context.Background())
	if !errors.Is(err, srosha.ErrUnauthorized) {
		t.Errorf("Whoami = %v, want ErrUnauthorized", err)
	}
}

// Reaching nobody is transient, and is retried like any other call.
func TestWhoamiRetriesWhenSroshaIsDown(t *testing.T) {
	i := &identity{err: status.Error(codes.Unavailable, "no")}
	c := dialIdentity(t, i, srosha.WithRetry(3))

	if _, err := c.Whoami(context.Background()); !errors.Is(err, srosha.ErrUnavailable) {
		t.Errorf("Whoami = %v, want ErrUnavailable", err)
	}
	if i.calls != 3 {
		t.Errorf("tried %d times, want 3", i.calls)
	}
}

// The secret crosses the wire on the first registration and nowhere else, so
// the SDK must hand it straight through rather than dropping it.
func TestRotateSecretHandsBackTheNewOne(t *testing.T) {
	s := &setup{}
	c := dialSetup(t, s)

	got, err := c.Webhooks.RotateSecret(context.Background())
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if got != "whsec_rotated" {
		t.Errorf("secret = %q, want the rotated one", got)
	}
}

// A registration that moved an address rather than creating a callback returns
// no secret, because the existing one still stands.
func TestMovingAnAddressReturnsNoSecret(t *testing.T) {
	s := &setup{noSecret: true}
	c := dialSetup(t, s)

	_, secret, err := c.Webhooks.Register(context.Background(), "https://acme.test/moved")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if secret != "" {
		t.Errorf("secret = %q, want none when the address only moved", secret)
	}
}
