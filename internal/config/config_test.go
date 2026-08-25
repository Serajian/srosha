package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/config"
)

// setMinimum sets only what has no sensible default.
func setMinimum(t *testing.T) {
	t.Helper()
	t.Setenv("NOTIF_DB_DSN", "postgres://srosha:pw@postgres:5432/srosha")
	t.Setenv("NOTIF_MQ_URL", "nats://gateway:pw@nats:4222")
	t.Setenv("NOTIF_CRYPTO_KEYS", `{"1":"`+testKey+`"}`)
	t.Setenv("NOTIF_CRYPTO_KEY_ID", "1")
}

// testKey is 32 zero bytes in standard base64. Key material never has to be
// real to be the right length, and the length is the only thing config checks.
const testKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestGatewayLoadsWithDefaults(t *testing.T) {
	setMinimum(t)

	c, err := config.LoadGateway()
	if err != nil {
		t.Fatalf("LoadGateway() error = %v", err)
	}

	if c.GRPC.Addr != ":50051" || c.RateLimit.PerMinute != 600 {
		t.Errorf("defaults not applied: %+v", c.GRPC)
	}
	if c.App.Env != "development" {
		t.Errorf("Env = %q, want development", c.App.Env)
	}
}

func TestDispatcherLoadsWithDefaults(t *testing.T) {
	setMinimum(t)

	c, err := config.LoadDispatcher()
	if err != nil {
		t.Fatalf("LoadDispatcher() error = %v", err)
	}

	if c.Dispatch.MaxAttempts != 5 || c.Dispatch.ReconcileGiveUp <= c.Dispatch.ReconcileAfter {
		t.Errorf("dispatch defaults look wrong: %+v", c.Dispatch)
	}
}

// A missing DSN found at boot costs seconds; the same DSN found on the first
// request is an outage nobody was watching for.
func TestRequiredKeysAreRefusedAtBoot(t *testing.T) {
	if _, err := config.LoadGateway(); err == nil {
		t.Fatal("LoadGateway() succeeded with no DSN and no MQ url")
	}
}

// One restart should tell you about every problem, not the next one each time.
func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	t.Setenv("NOTIF_DB_MAX_CONNS", "lots")
	t.Setenv("NOTIF_APP_SHUTDOWN_TIMEOUT", "soon")

	_, err := config.LoadGateway()
	if err == nil {
		t.Fatal("LoadGateway() = nil error")
	}

	for _, want := range []string{
		"NOTIF_DB_DSN", "NOTIF_MQ_URL", "NOTIF_DB_MAX_CONNS", "NOTIF_APP_SHUTDOWN_TIMEOUT",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}

// Relaxing the callback check is for a laptop. On a real deployment it is what
// stops a source pointing us at our own network, so it is refused rather than
// warned about.
func TestProductionRefusesARelaxedCallbackCheck(t *testing.T) {
	setMinimum(t)
	t.Setenv("NOTIF_APP_ENV", "production")
	t.Setenv("NOTIF_WEBHOOK_ALLOW_PRIVATE_URL", "true")

	_, err := config.LoadDispatcher()
	if err == nil {
		t.Fatal("production accepted a relaxed callback check")
	}
	if !strings.Contains(err.Error(), "ALLOW_PRIVATE_URL") {
		t.Errorf("error does not say which key: %v", err)
	}
}

func TestDevelopmentAllowsARelaxedCallbackCheck(t *testing.T) {
	setMinimum(t)
	t.Setenv("NOTIF_WEBHOOK_ALLOW_PRIVATE_URL", "true")
	t.Setenv("NOTIF_WEBHOOK_ALLOW_INSECURE_URL", "true")

	c, err := config.LoadDispatcher()
	if err != nil {
		t.Fatalf("LoadDispatcher() error = %v", err)
	}
	if !c.Webhook.AllowPrivateURL || !c.Webhook.AllowInsecureURL {
		t.Error("the relaxed settings were not read")
	}
}

// Give-up at or below after means a row is past its last chance the first time
// recovery sees it, so it never gets a second attempt.
func TestReconcileThresholdsMustMakeSense(t *testing.T) {
	setMinimum(t)
	t.Setenv("NOTIF_RECONCILE_AFTER", "30m")
	t.Setenv("NOTIF_RECONCILE_GIVE_UP", "5m")

	_, err := config.LoadDispatcher()
	if err == nil {
		t.Fatal("a give-up shorter than after was accepted")
	}
}

func TestWebhookSecretsArePerSource(t *testing.T) {
	setMinimum(t)
	t.Setenv("NOTIF_WEBHOOK_SECRETS", `{"acme":"a1b2","shop":"c3d4"}`)

	c, err := config.LoadDispatcher()
	if err != nil {
		t.Fatalf("LoadDispatcher() error = %v", err)
	}
	if c.Webhook.Secrets["acme"].Reveal() != "a1b2" || len(c.Webhook.Secrets) != 2 {
		t.Errorf("secrets not read: %d entries", len(c.Webhook.Secrets))
	}
}

// Printing a config must not leak what is in it.
func TestSecretsDoNotPrint(t *testing.T) {
	setMinimum(t)
	t.Setenv("NOTIF_WEBHOOK_SECRETS", `{"acme":"a1b2"}`)

	c, err := config.LoadDispatcher()
	if err != nil {
		t.Fatalf("LoadDispatcher() error = %v", err)
	}

	printed := fmt.Sprintf("%+v", c)
	for _, leak := range []string{"a1b2", "pw@postgres", "pw@nats"} {
		if strings.Contains(printed, leak) {
			t.Errorf("printing the config leaked %q:\n%s", leak, printed)
		}
	}
}

func TestTheKeyringIsCheckedAtBoot(t *testing.T) {
	cases := map[string]struct {
		keys, active, want string
	}{
		"not base64":       {`{"1":"not base64!"}`, "1", "not valid base64"},
		"wrong length":     {`{"1":"c2hvcnQ="}`, "1", "must be 32"},
		"active not there": {`{"1":"` + testKey + `"}`, "2", "not a key in"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NOTIF_DB_DSN", "postgres://srosha:pw@postgres:5432/srosha")
			t.Setenv("NOTIF_MQ_URL", "nats://gateway:pw@nats:4222")
			t.Setenv("NOTIF_CRYPTO_KEYS", c.keys)
			t.Setenv("NOTIF_CRYPTO_KEY_ID", c.active)

			_, err := config.LoadGateway()
			if err == nil {
				t.Fatal("LoadGateway() accepted a broken keyring")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

// The keyring must survive being logged, the same as every other secret.
func TestTheKeyringDoesNotPrint(t *testing.T) {
	setMinimum(t)

	c, err := config.LoadGateway()
	if err != nil {
		t.Fatalf("LoadGateway() error = %v", err)
	}
	if printed := fmt.Sprintf("%v", c.Crypto); strings.Contains(printed, testKey) {
		t.Errorf("the keyring printed its key material: %s", printed)
	}
}
