package bootstrap

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/adapter/api/web"
	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/adapter/mailer"
	"github.com/Serajian/srosha/internal/adapter/ratelimit"
	"github.com/Serajian/srosha/internal/adapter/secret"
	"github.com/Serajian/srosha/internal/adapter/sender"
	"github.com/Serajian/srosha/internal/adapter/sender/email"
	"github.com/Serajian/srosha/internal/adapter/system"
	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/internal/infra/httpserver"
	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/internal/registry"
	"github.com/Serajian/srosha/pkg/crypto"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Console opens what the human-facing binary needs: it serves pages and reads
// rows, so it gets the database and one mail account.
//
// It carries the customer portal today and will carry the admin surface beside
// it, on a listener of its own. They share this process, the mail account and
// the session cookie -- a cookie is not scoped by port, so a customer's session
// reaches the admin listener too, and what keeps them apart is the admin
// guard's own role check, read from the live row on every request.
//
// It has no broker, no sending credentials and no callback secrets, and must
// not be given any. The mail account is its own rather than the sender's,
// because signing in must not depend on how a customer's messages happen to be
// configured -- and because a sign-in that goes through the service you are
// signing in to fix is a trap.
func Console(ctx context.Context, cfg config.Console) (*App, error) {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryConsole)
	if err != nil {
		return nil, err
	}

	res := registry.New(log)

	// First, so every failure below has somewhere to report to. Unconfigured
	// it is silent and costs nothing.
	notify, err := alerts(cfg.Alert, res, log)
	if err != nil {
		_ = res.Close(ctx)
		return nil, err
	}

	db, err := registry.Postgres(ctx, cfg.DB, res)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	dialer, err := registry.SMTPDialer(cfg.Console.MailTimeout)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	core, err := buildConsoleCore(cfg, db.Pool(), dialer, notify, res, log)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	pages, err := web.NewPortal(web.PortalDeps{
		SignIn:       core.signIn,
		Sources:      core.sources,
		Keys:         core.keys,
		Senders:      core.senders,
		Trials:       core.trials,
		Callbacks:    core.callbacks,
		SecureCookie: cfg.Console.SecureCookie,

		// Everywhere but production, the same rule the gateway applies to
		// reflection. gin's debug output prints the request that panicked, and
		// requests here carry sign-in codes.
		Debug: !cfg.App.IsProduction(),
		Log:   log,
	})
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	panel, err := web.NewAdmin(web.AdminDeps{
		SignIn:       core.signIn,
		Operators:    core.operators,
		SecureCookie: cfg.Console.SecureCookie,
		Debug:        !cfg.App.IsProduction(),
		Log:          log,
	})
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	// Three listeners on purpose. The portal is public, readiness is not, and
	// the admin panel must not be reachable from either -- putting any two of
	// these on one port would publish something that must not be.
	portal, err := servePortal(ctx, cfg.Console.PortalAddr, cfg.HTTPServer, pages, res)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	health, err := httpServer(ctx, binaryConsole, cfg.HTTP.Addr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	// cfg.Console.AdminAddr defaults to the loopback interface, not every
	// interface, so this listener stays off the network a customer can reach
	// as a property of the process -- see docs/ARCHITECTURE.md, "Two surfaces
	// in one binary, and what keeps them apart".
	admin, err := serveAdmin(ctx, cfg.Console.AdminAddr, cfg.HTTPServer, panel, res)
	if err != nil {
		return abandon(ctx, res, notify, err)
	}

	log.InfoContext(ctx, "console started",
		"env", cfg.App.Env, "portal", portal.Addr(), "http", health.Addr(), "admin", admin.Addr())
	notify.Notify(ctx, "console started",
		"env "+cfg.App.Env+", portal "+portal.Addr()+", admin "+admin.Addr())

	return &App{
		log:        log,
		resources:  res,
		watch:      newWatcher(notify, log),
		watchEvery: cfg.Alert.ReadyEvery,
		failed:     watch(portal.Err(), health.Err(), admin.Err()),
	}, nil
}

// servePortal and serveAdmin are the only two ways a surface reaches a
// listener, and the whole point of them is that each takes ONE surface's
// address type and ONE surface's handler type.
//
// Both used to be a bare registry.HTTPServer call taking a string and an
// http.Handler, which meant swapping the two arguments compiled, passed every
// test, and served the admin panel on the port Traefik publishes. There is no
// test that catches that -- both listeners answer, and each answers correctly
// -- so it is held by the types instead: see web.PortalHandler and
// settings.PortalAddr.
func servePortal(
	ctx context.Context, addr settings.PortalAddr, s settings.HTTPServer,
	h web.PortalHandler, res *registry.Resources,
) (*httpserver.Server, error) {
	return registry.HTTPServer(ctx, "portal pages", string(addr), s, h, res)
}

func serveAdmin(
	ctx context.Context, addr settings.AdminAddr, s settings.HTTPServer,
	h web.AdminHandler, res *registry.Resources,
) (*httpserver.Server, error) {
	return registry.HTTPServer(ctx, "admin pages", string(addr), s, h, res)
}

// buildPortalCore assembles the one use case the portal stands on.
//
// Nothing here is registered with the registry: none of it holds a resource, so
// there is nothing to close and no order to get wrong.
// consoleCore is everything the pages stand on. Nothing here is registered with
// the registry: none of it holds a resource, so there is nothing to close.
type consoleCore struct {
	signIn    *usecase.SignIn
	sources   *usecase.Sources
	keys      *usecase.Keys
	senders   *usecase.Credentials
	callbacks *usecase.Registrar
	operators *usecase.Operators

	// senders as a registry, not as a use case: this is what a trial sends
	// through. It can open a source's own identity and no other -- see
	// consoleSenders.
	senderRegistry *sender.Registry

	trials *usecase.Trials
}

func buildConsoleCore(
	cfg config.Console, pool *pgxpool.Pool, dialer *smtp.Dialer,
	notify usecase.Alerter, res *registry.Resources, log *slog.Logger,
) (consoleCore, error) {
	var core consoleCore

	now := system.Clock()

	ids, err := system.NewIDs(now)
	if err != nil {
		return core, err
	}

	post, err := mailer.New(dialer, smtp.Identity{
		Host:     cfg.Console.SMTP.Host,
		Port:     cfg.Console.SMTP.Port,
		Username: cfg.Console.SMTP.Username,
		Password: cfg.Console.SMTP.Password.Reveal(),
	}, cfg.Console.SMTP.From)
	if err != nil {
		return core, err
	}

	// The gate is the one point every mutating change goes through, and it
	// writes the audit row before the change runs. Operators reads the same
	// log below, so the repository is kept rather than built twice.
	auditRows := postgres.NewAuditRepository(pool)
	gate := usecase.NewGate(auditRows, notify, ids.Generate, now)

	core.signIn = usecase.NewSignIn(
		postgres.NewUserRepository(pool),
		postgres.NewLoginCodeRepository(pool),
		postgres.NewSessionRepository(pool),
		post,
		ids.Generate,
		now,
	)
	core.sources = usecase.NewSources(
		postgres.NewSourceRepository(pool), gate, ids.Generate, now,
	)
	core.keys = usecase.NewKeys(
		postgres.NewAPIKeyRepository(pool), core.sources, auth.NewScheme(),
		gate, ids.Generate, now,
	)

	if err := buildIdentityCore(&core, cfg, pool, dialer, gate, ids, now, res, log); err != nil {
		return core, err
	}

	// Operators is the admin surface's one use case. Every repository it needs
	// is already opened above -- source, user and credential rows the portal's
	// use cases already read, plus notification and delivery rows for the
	// queue's message and delivery views -- so this wraps the same pool again
	// rather than opening anything twice.
	core.operators = usecase.NewOperators(
		postgres.NewSourceRepository(pool),
		postgres.NewUserRepository(pool),
		postgres.NewNotificationRepository(pool),
		postgres.NewDeliveryRepository(pool, now),
		postgres.NewCredentialRepository(pool),
		auditRows, gate, now, cfg.Console.AdminListLimit,
	)
	return core, nil
}

// buildIdentityCore assembles the two use cases a customer configures a source
// with. Both already exist and are already tested; the console is a second face
// on them, next to gRPC.
//
// The rate limiter is required by source.Service and never consulted here: it
// is spent by Admit, which is the sending path, and the console does not send.
func buildIdentityCore(
	core *consoleCore, cfg config.Console, pool *pgxpool.Pool, dialer *smtp.Dialer,
	gate *usecase.Gate, ids *system.IDs, now shared.NowFunc,
	res *registry.Resources, log *slog.Logger,
) error {
	keys, err := crypto.NewKeyring(cfg.Crypto.Keys, cfg.Crypto.ActiveID)
	if err != nil {
		return err
	}

	limiter, err := ratelimit.NewMemory(consoleRateLimit, now)
	if err != nil {
		return err
	}

	sourceRows := postgres.NewSourceRepository(pool)
	credentialRows := postgres.NewCredentialRepository(pool)
	webhookRows := postgres.NewWebhookRepository(pool)

	callbackSecrets, err := secret.NewWebhookKeeper(webhookRows, keys, now)
	if err != nil {
		return err
	}

	sources := source.NewService(sourceRows, limiter)

	core.callbacks = usecase.NewRegistrar(
		sources,
		webhook.NewService(webhookRows, ids.Generate, now, webhook.URLPolicy{
			AllowInsecure: cfg.WebhookPolicy.AllowInsecureURL,
			AllowPrivate:  cfg.WebhookPolicy.AllowPrivateURL,
		}),
		callbackSecrets,
	)

	// The rows never see a secret in the clear and the core never sees one
	// sealed. This is the only place both are true at once.
	secrets, err := secret.New(credentialRows, keys, now, log)
	if err != nil {
		return err
	}

	providers, err := registry.SenderClient(cfg.HTTPClient, res)
	if err != nil {
		return err
	}
	tokens, err := registry.GoogleTokens(providers)
	if err != nil {
		return err
	}
	apple, err := registry.AppleTokens(now)
	if err != nil {
		return err
	}

	// One service, not two: the registry resolves the same rows the use case
	// writes.
	creds := credential.NewService(credentialRows, now)

	// The dialer is the one sign-in codes go through. It is a connection
	// factory and not an account -- the source's own identity is supplied per
	// send -- so sharing it lends the customer nothing of srosha's.
	core.senderRegistry, err = consoleSenders(creds, secrets, providers, dialer, tokens, apple)
	if err != nil {
		return err
	}

	// A bucket of its own, and not the one above. That one is the SENDING
	// quota, spent in the gateway; a test must not look like it costs a
	// customer a message.
	trialLimit, err := ratelimit.NewMemory(cfg.Console.TrialPerMinute, now)
	if err != nil {
		return err
	}
	core.trials = usecase.NewTrials(
		sources, creds, core.senderRegistry, gate, trialLimit, ids.Generate,
	)

	core.senders = usecase.NewCredentials(
		sources,
		creds,
		secrets,
		credentialRows,
		credentialRows,
		postgres.NewUnitOfWork(pool),
		ids.Generate,
		now,
	)
	return nil
}

// consoleSenders builds the registry a trial sends through, and exists as its
// own function for one reason: the empty Fallback below is the security
// boundary of the whole feature, and a boundary a test cannot reach is a
// comment.
//
// With an identity of srosha's in there, a customer whose own credential is
// broken would press "test", srosha would send as itself, and the page would
// say it worked -- a wrong answer wearing a right one's clothes. Every branch
// of Registry.ours asks configured() first, so an empty Fallback refuses all
// eight channels.
//
// TestTheConsoleCannotSendAsSrosha calls THIS function, so filling the fallback
// in turns it red.
func consoleSenders(
	creds *credential.Service, secrets sender.Secrets, providers *http.Client,
	dialer email.Dialer, tokens sender.GoogleTokens, apple sender.AppleTokens,
) (*sender.Registry, error) {
	return sender.NewRegistry(creds, secrets, providers, dialer, tokens, apple, sender.Fallback{})
}
