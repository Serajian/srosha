package bootstrap

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/api/web"
	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/adapter/mailer"
	"github.com/Serajian/srosha/internal/adapter/system"
	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/internal/registry"

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

	db, err := registry.Postgres(ctx, cfg.DB, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	dialer, err := registry.SMTPDialer(cfg.Console.MailTimeout)
	if err != nil {
		return abandon(ctx, res, err)
	}

	signIn, err := buildConsoleCore(cfg, db.Pool(), dialer)
	if err != nil {
		return abandon(ctx, res, err)
	}

	pages, err := web.New(web.Deps{
		SignIn:       signIn,
		SecureCookie: cfg.Console.SecureCookie,
		Log:          log,
	})
	if err != nil {
		return abandon(ctx, res, err)
	}

	// Two listeners on purpose. The pages are public, and readiness is not:
	// putting both on one port would publish it to the internet.
	portal, err := registry.HTTPServer(
		ctx, "portal pages", cfg.Console.PortalAddr, cfg.HTTPServer, pages, res,
	)
	if err != nil {
		return abandon(ctx, res, err)
	}

	health, err := httpServer(ctx, binaryConsole, cfg.HTTP.Addr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	log.InfoContext(ctx, "console started",
		"env", cfg.App.Env, "portal", portal.Addr(), "http", health.Addr())

	return &App{
		log:       log,
		resources: res,
		failed:    watch(portal.Err(), health.Err()),
	}, nil
}

// buildPortalCore assembles the one use case the portal stands on.
//
// Nothing here is registered with the registry: none of it holds a resource, so
// there is nothing to close and no order to get wrong.
func buildConsoleCore(
	cfg config.Console, pool *pgxpool.Pool, dialer *smtp.Dialer,
) (*usecase.SignIn, error) {
	now := system.Clock()

	ids, err := system.NewIDs(now)
	if err != nil {
		return nil, err
	}

	post, err := mailer.New(dialer, smtp.Identity{
		Host:     cfg.Console.SMTP.Host,
		Port:     cfg.Console.SMTP.Port,
		Username: cfg.Console.SMTP.Username,
		Password: cfg.Console.SMTP.Password.Reveal(),
	}, cfg.Console.SMTP.From)
	if err != nil {
		return nil, err
	}

	return usecase.NewSignIn(
		postgres.NewUserRepository(pool),
		postgres.NewLoginCodeRepository(pool),
		postgres.NewSessionRepository(pool),
		post,
		ids.Generate,
		now,
	), nil
}
