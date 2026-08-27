package bootstrap

import (
	"context"
	"log/slog"

	"github.com/Serajian/srosha/internal/adapter/api/grpcsrv"
	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/adapter/mq/nats"
	"github.com/Serajian/srosha/internal/adapter/ratelimit"
	"github.com/Serajian/srosha/internal/adapter/secret"
	"github.com/Serajian/srosha/internal/adapter/system"
	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/internal/infra/grpcserver"
	"github.com/Serajian/srosha/internal/registry"
	"github.com/Serajian/srosha/pkg/crypto"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
)

// Gateway opens what the gateway needs: it accepts requests and publishes them,
// so it gets the database and the broker. It has no sender credentials and no
// callback secrets, and must not be given any.
//
// This function is the story -- what was opened, what was built from it, what
// is served. The building itself is below, so the story stays readable.
func Gateway(ctx context.Context, cfg config.Gateway) (*App, error) {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryGateway)
	if err != nil {
		return nil, err
	}

	res := registry.New(log)

	db, err := registry.Postgres(ctx, cfg.DB, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	mq, err := registry.NATS(ctx, cfg.MQ, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	core, err := buildGatewayCore(ctx, cfg, db.Pool(), mq.JetStream(), log)
	if err != nil {
		return abandon(ctx, res, err)
	}

	grpc, err := gatewayGRPC(ctx, cfg, core, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// Health only. Every other http route this service grows is mounted inside
	// api/http, not opened as a second listener here.
	health, err := httpServer(ctx, binaryGateway, cfg.GRPC.HTTPAddr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// service and binary are already on every line, so naming them again here
	// would be a duplicate key in json and nothing at all in text, where the
	// handler drops them.
	log.InfoContext(ctx, "gateway started",
		"env", cfg.App.Env, "grpc", grpc.Addr(), "http", health.Addr())

	return &App{
		log:       log,
		resources: res,
		failed:    watch(grpc.Err(), health.Err()),
	}, nil
}

// gatewayCore is everything the gateway's rpcs stand on: the three stories it
// can be asked to tell, and the one thing that decides whether it will listen.
type gatewayCore struct {
	submitter *usecase.Submitter
	querier   *usecase.Querier
	registrar *usecase.Registrar
	creds     *usecase.Credentials
	authn     *source.Authenticator
}

// buildGatewayCore assembles it from what was opened, in four steps: what the
// machine gives us, what the broker needs, the rows, and the rules over them.
//
// Nothing here is registered with the registry. None of it holds a resource --
// a repository is a struct over a pool, a service is a struct over a
// repository -- so there is nothing to close and no order to get wrong.
func buildGatewayCore(
	ctx context.Context,
	cfg config.Gateway,
	pool *pgxpool.Pool,
	js jetstream.JetStream,
	log *slog.Logger,
) (gatewayCore, error) {
	var core gatewayCore

	// --- what the machine knows --------------------------------------------
	now := system.Clock()

	ids, err := system.NewIDs(now)
	if err != nil {
		return core, err
	}

	limiter, err := ratelimit.NewMemory(cfg.RateLimit.PerMinute, now)
	if err != nil {
		return core, err
	}

	// The cipher is symmetric, so the gateway holding the key to seal a sending
	// secret is the gateway holding the key to open one. That is the price of
	// registering credentials through the API, and it is accepted rather than
	// overlooked: what this guards against is a database dump, and the gateway
	// already reads those rows.
	keys, err := crypto.NewKeyring(cfg.Crypto.Keys, cfg.Crypto.ActiveID)
	if err != nil {
		return core, err
	}

	// --- the broker ---------------------------------------------------------
	//
	// The stream is created here rather than waited for: the dispatcher does
	// the same, and whichever container starts first is not something to make
	// matter.
	stream, err := nats.DispatchStream(cfg.MQ.Stream)
	if err != nil {
		return core, err
	}

	err = nats.EnsureStream(ctx, js, nats.StreamConfig{
		Stream:          stream,
		DuplicateWindow: cfg.MQ.DuplicateWindow,
		MaxAge:          cfg.MQ.MaxAge,
	})
	if err != nil {
		return core, err
	}

	publisher, err := nats.NewDispatchPublisher(js, stream)
	if err != nil {
		return core, err
	}

	// --- the rows -----------------------------------------------------------
	sourceRows := postgres.NewSourceRepository(pool)
	keyRows := postgres.NewAPIKeyRepository(pool)
	notificationRows := postgres.NewNotificationRepository(pool)
	deliveryRows := postgres.NewDeliveryRepository(pool, now)
	credentialRows := postgres.NewCredentialRepository(pool)
	webhookRows := postgres.NewWebhookRepository(pool)
	uow := postgres.NewUnitOfWork(pool)

	// --- the rules over them ------------------------------------------------
	sources := source.NewService(sourceRows, limiter)
	credentials := credential.NewService(credentialRows, now)

	// The rows never see a secret in the clear and the core never sees one
	// sealed. This is the only place both are true at once.
	secrets, err := secret.New(credentialRows, keys, now, log)
	if err != nil {
		return core, err
	}

	notifications := notification.NewService(notificationRows, ids.Generate, now, cfg.RetentionAge)
	deliveries := delivery.NewService(deliveryRows, publisher, ids.Generate, now)
	webhooks := webhook.NewService(webhookRows, ids.Generate, now, webhook.URLPolicy{
		AllowInsecure: cfg.WebhookPolicy.AllowInsecureURL,
		AllowPrivate:  cfg.WebhookPolicy.AllowPrivateURL,
	})

	return gatewayCore{
		submitter: usecase.NewSubmitter(sources, credentials, notifications, deliveries, uow, log),
		querier:   usecase.NewQuerier(notifications, deliveries),
		registrar: usecase.NewRegistrar(sources, webhooks),
		creds: usecase.NewCredentials(
			sources,
			credentials,
			secrets,
			credentialRows,
			credentialRows,
			uow,
			ids.Generate,
			now,
		),
		authn: source.NewAuthenticator(keyRows, now, cfg.Auth.KeyTouchAfter),
	}, nil
}

// gatewayGRPC mounts the rpcs and starts listening.
//
// Building the server and running it are two steps on purpose: the adapter
// knows what it serves, and the registry knows when it stops -- first of
// everything, so a call in flight still has the pool underneath it.
func gatewayGRPC(
	ctx context.Context,
	cfg config.Gateway,
	core gatewayCore,
	log *slog.Logger,
	res *registry.Resources,
) (*grpcserver.Server, error) {
	notifications, err := grpcsrv.NewNotificationServer(core.submitter, core.querier)
	if err != nil {
		return nil, err
	}

	webhooks, err := grpcsrv.NewWebhookServer(core.registrar)
	if err != nil {
		return nil, err
	}

	credentials, err := grpcsrv.NewCredentialServer(core.creds)
	if err != nil {
		return nil, err
	}

	server, err := grpcsrv.New(grpcsrv.Deps{
		Notifications: notifications,
		Webhooks:      webhooks,
		Credentials:   credentials,
		Sources: grpcsrv.NewSourceServer(grpcsrv.Limits{
			Retention:          cfg.RetentionAge,
			RateLimitPerMinute: cfg.RateLimit.PerMinute,
		}),
		Authn:  core.authn,
		Scheme: auth.NewScheme(),
		Log:    log,

		// Everywhere but production, so grpcurl and Postman need no proto
		// files. On a real deployment it would publish the whole API surface
		// to anyone who reached the port, unauthenticated -- see Deps.
		Reflection: !cfg.App.IsProduction(),
	})
	if err != nil {
		return nil, err
	}

	return registry.GRPCServer(ctx, "grpc server", cfg.GRPC, server, res)
}
