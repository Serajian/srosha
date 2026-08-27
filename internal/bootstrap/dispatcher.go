package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/adapter/mq/nats"
	"github.com/Serajian/srosha/internal/adapter/notifier"
	"github.com/Serajian/srosha/internal/adapter/secret"
	"github.com/Serajian/srosha/internal/adapter/sender"
	"github.com/Serajian/srosha/internal/adapter/sender/email"
	"github.com/Serajian/srosha/internal/adapter/system"
	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/internal/registry"
	"github.com/Serajian/srosha/pkg/crypto"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
)

// Dispatcher opens what the dispatcher needs. On top of the gateway's two it
// calls out: to the sources' callbacks, and to the providers. Those are two
// clients rather than one, because only the callback address is chosen by
// somebody else.
//
// It has two ways in and neither is a port anybody dials. The broker brings an
// event; the scheduler finds a row nobody was told about. The only listener is
// health.
func Dispatcher(ctx context.Context, cfg config.Dispatcher) (*App, error) {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryDispatcher)
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

	callbacks, err := registry.WebhookClient(cfg.HTTPClient, cfg.Webhook, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	providers, err := registry.SenderClient(cfg.HTTPClient, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// Mail is its own way out. Nothing is held open -- a mail server drops an
	// idle session on its own schedule -- so this is a dialer and there is
	// nothing to register a close for.
	mail, err := registry.SMTPDialer(cfg.HTTPClient)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// Google is its own way out too, and for the opposite reason: a service
	// account is a private key, not a token, so it has to be exchanged for one.
	// This holds the result of that exchange, which is why it is opened once
	// rather than per message.
	tokens, err := registry.GoogleTokens(providers)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// Apple's is the same shape and needs no client at all: a provider token is
	// signed here, not asked for. What it holds is the clock Apple's refresh
	// rules are measured against.
	apple, err := registry.AppleTokens(system.Clock())
	if err != nil {
		return abandon(ctx, res, err)
	}

	core, err := buildDispatcherCore(
		ctx, cfg, db.Pool(), mq.JetStream(), callbacks, providers, mail, tokens, apple, log,
	)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// The broker's way in.
	_, err = registry.Consumer(
		ctx, "dispatch consumer", mq.JetStream(), core.stream, cfg.Dispatch, core.dispatcher, res,
	)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// The other way in, for the deliveries no event ever arrived for: the rows
	// written when a publish never reached the broker.
	//
	// UTC, so a schedule means the same moment wherever this runs and does not
	// happen twice on the day a zone puts its clocks back.
	_, err = registry.Scheduler(ctx, "scheduler", time.UTC, cfg.App.ShutdownTimeout, []registry.Job{
		{
			Name:     "recovery",
			Schedule: cfg.Dispatch.ReconcileSchedule,
			Run:      core.dispatcher.Recover,
		},
		{
			// srosha is not an archive. Nightly rather than on an interval,
			// because a heavy sweep should run at an hour somebody chose.
			Name:     "retention",
			Schedule: cfg.Retention.Schedule,
			Run:      core.retention.Purge,
		},
	}, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	health, err := httpServer(ctx, binaryDispatcher, cfg.HTTP.Addr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	log.InfoContext(ctx, "dispatcher started",
		"env", cfg.App.Env, "http", health.Addr())

	return &App{
		log:       log,
		resources: res,
		failed:    watch(health.Err()),
	}, nil
}

// dispatcherCore is what the two ways in are pointed at, plus the stream the
// consumer is created on.
type dispatcherCore struct {
	dispatcher *usecase.Dispatcher
	retention  *usecase.Retention
	stream     nats.Stream
}

// buildDispatcherCore assembles it, in the same four steps the gateway takes:
// what the machine gives us, what the broker needs, the rows, and the rules
// over them.
//
// Nothing here is registered with the registry. None of it holds a resource --
// they were all opened above and are passed in.
func buildDispatcherCore(
	ctx context.Context,
	cfg config.Dispatcher,
	pool *pgxpool.Pool,
	js jetstream.JetStream,
	callbacks, providers *http.Client,
	mail email.Dialer,
	tokens sender.GoogleTokens,
	apple sender.AppleTokens,
	log *slog.Logger,
) (dispatcherCore, error) {
	var core dispatcherCore

	// --- what the machine knows --------------------------------------------
	now := system.Clock()

	ids, err := system.NewIDs(now)
	if err != nil {
		return core, err
	}

	keys, err := crypto.NewKeyring(cfg.Crypto.Keys, cfg.Crypto.ActiveID)
	if err != nil {
		return core, err
	}

	// --- the broker ---------------------------------------------------------
	//
	// Created here rather than waited for: the gateway does the same, and
	// whichever container starts first is not something to make matter.
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

	// --- the rows -----------------------------------------------------------
	notificationRows := postgres.NewNotificationRepository(pool)
	deliveryRows := postgres.NewDeliveryRepository(pool, now)
	credentialRows := postgres.NewCredentialRepository(pool)
	webhookRows := postgres.NewWebhookRepository(pool)

	// --- the rules over them ------------------------------------------------
	notifications := notification.NewService(notificationRows, ids.Generate, now, cfg.Retention.Age)
	deliveries := delivery.NewTracker(deliveryRows, now)
	credentials := credential.NewService(credentialRows, now)
	webhooks := webhook.NewService(webhookRows, ids.Generate, now, webhook.URLPolicy{
		AllowInsecure: cfg.Webhook.AllowInsecureURL,
		AllowPrivate:  cfg.Webhook.AllowPrivateURL,
	})

	secrets, err := secret.New(credentialRows, keys, now, log)
	if err != nil {
		return core, err
	}

	senders, err := sender.NewRegistry(
		credentials,
		secrets,
		providers,
		mail,
		tokens,
		apple,
		sender.Fallback{
			TelegramToken: cfg.Sender.Telegram.Reveal(),
			BaleToken:     cfg.Sender.Bale.Reveal(),
			Matrix: sender.Matrix{
				Token:      cfg.Sender.Matrix.Token.Reveal(),
				Homeserver: cfg.Sender.Matrix.Homeserver,
			},
			FCMServiceAccount: cfg.Sender.FCM.Reveal(),
			APNs: sender.APNs{
				Key:         cfg.Sender.APNs.Key.Reveal(),
				KeyID:       cfg.Sender.APNs.KeyID,
				TeamID:      cfg.Sender.APNs.TeamID,
				Topic:       cfg.Sender.APNs.Topic,
				Environment: cfg.Sender.APNs.Environment,
			},
			WhatsApp: sender.WhatsApp{
				Token:         cfg.Sender.WhatsApp.Token.Reveal(),
				PhoneNumberID: cfg.Sender.WhatsApp.PhoneNumberID,
			},
			SMTP: sender.SMTP{
				Host:     cfg.Sender.SMTP.Host,
				Port:     cfg.Sender.SMTP.Port,
				Username: cfg.Sender.SMTP.Username,
				From:     cfg.Sender.SMTP.From,
				Password: cfg.Sender.SMTP.Password.Reveal(),
			},
		},
	)
	if err != nil {
		return core, err
	}

	// The signing secret comes from the row, sealed, rather than from config.
	// The dispatcher holds the keyring already -- it opens sending credentials
	// with it on every message -- so this costs it no new capability.
	callbackSecrets, err := secret.NewWebhookKeeper(
		postgres.NewWebhookRepository(pool), keys, now,
	)
	if err != nil {
		return core, err
	}

	callback, err := notifier.New(callbacks, callbackSecrets, now, log)
	if err != nil {
		return core, err
	}

	return dispatcherCore{
		stream:    stream,
		retention: usecase.NewRetention(notifications, log, cfg.Retention.Age, cfg.Retention.Batch),
		dispatcher: usecase.NewDispatcher(
			notifications, deliveries, webhooks, senders, callback,
			ids.Generate, now, log,
			cfg.Dispatch.MaxAttempts, cfg.Webhook.MaxFailures,
			cfg.Dispatch.ReconcileAfter, cfg.Dispatch.ReconcileGiveUp, cfg.Dispatch.ReconcileLease,
			cfg.Dispatch.ReconcileBatch,
		),
	}, nil
}
