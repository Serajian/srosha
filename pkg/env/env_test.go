package env_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

func TestReadsValues(t *testing.T) {
	t.Setenv("T_NAME", "srosha")
	t.Setenv("T_COUNT", "42")
	t.Setenv("T_ON", "true")
	t.Setenv("T_WAIT", "30s")

	r := env.New("T_")

	if got := r.Str("NAME", "other"); got != "srosha" {
		t.Errorf("Str() = %q", got)
	}
	if got := r.Int("COUNT", 1); got != 42 {
		t.Errorf("Int() = %d", got)
	}
	if got := r.Bool("ON", false); !got {
		t.Error("Bool() = false")
	}
	if got := r.Duration("WAIT", time.Second); got != 30*time.Second {
		t.Errorf("Duration() = %v", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v", err)
	}
}

func TestFallsBackWhenUnset(t *testing.T) {
	r := env.New("T_")

	if got := r.Str("MISSING", "fallback"); got != "fallback" {
		t.Errorf("Str() = %q, want the fallback", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("a missing optional key was reported: %v", err)
	}
}

// An empty variable is not a value. Otherwise NOTIF_DB_DSN= starts the service
// with no database and fails at the first query instead of at boot.
func TestEmptyIsTreatedAsUnset(t *testing.T) {
	t.Setenv("T_NAME", "   ")

	r := env.New("T_")
	if got := r.Str("NAME", "fallback"); got != "fallback" {
		t.Errorf("Str() = %q, want the fallback", got)
	}

	r2 := env.New("T_")
	r2.Required("NAME")
	if r2.Err() == nil {
		t.Error("a blank value satisfied Required")
	}
}

// One restart should tell you about every missing key, not the next one each
// time.
func TestCollectsEveryFailure(t *testing.T) {
	t.Setenv("T_COUNT", "not-a-number")
	t.Setenv("T_WAIT", "soon")

	r := env.New("T_")
	r.Required("DSN")
	r.Int("COUNT", 0)
	r.Duration("WAIT", 0)

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil")
	}
	for _, want := range []string{"T_DSN", "T_COUNT", "T_WAIT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestJSON(t *testing.T) {
	t.Setenv("T_SECRETS", `{"acme":"a1b2","shop":"c3d4"}`)

	r := env.New("T_")
	got := map[string]string{}
	r.JSON("SECRETS", &got)

	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
	if got["acme"] != "a1b2" || len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

// The value is a map of secrets, so a parse failure must not print it.
func TestJSONErrorDoesNotEchoTheValue(t *testing.T) {
	t.Setenv("T_SECRETS", `{"acme":"super-secret-value"`)

	r := env.New("T_")
	r.JSON("SECRETS", &map[string]string{})

	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Errorf("the error printed the value: %v", err)
	}
}

// A secret must not appear in a log line just because someone printed the
// struct that holds it.
func TestSecretDoesNotPrintItself(t *testing.T) {
	s := env.Secret("hunter2")

	// Through any, so the check is on what fmt does with the value at runtime
	// rather than on what the compiler can see about its type.
	var v any = s

	for _, got := range []string{
		s.String(),
		fmt.Sprintf("%v", v),
		fmt.Sprintf("%s", v),
		fmt.Sprintf("%+v", struct{ Token env.Secret }{s}),
		fmt.Sprintf("%#v", v),
	} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("leaked: %s", got)
		}
	}

	if s.Reveal() != "hunter2" {
		t.Error("Reveal() does not give the value back")
	}
}

func TestCheck(t *testing.T) {
	r := env.New("T_")
	r.Check(false, "give-up must be longer than after")

	if r.Err() == nil {
		t.Error("a failed check was not reported")
	}
}
