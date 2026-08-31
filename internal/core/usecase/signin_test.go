package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type signInRig struct {
	signIn   *usecase.SignIn
	users    *fakeUsers
	sessions *fakeSessions
	mail     *fakeMailer
	ids      shared.IDFunc
	at       time.Time
}

func newSignInRig(t *testing.T) *signInRig {
	t.Helper()

	r := &signInRig{
		users:    newFakeUsers(),
		sessions: newFakeSessions(),
		mail:     &fakeMailer{},
		ids:      seqIDs(),
		at:       time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}
	r.signIn = usecase.NewSignIn(
		r.users, &fakeCodes{}, r.sessions, r.mail, r.ids, fixedNow(r.at),
	)
	return r
}

func (r *signInRig) addUser(t *testing.T, email string, active bool) *user.User {
	t.Helper()

	u, err := user.New(r.ids(), email, user.RoleCustomer, r.at)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	u.IsActive = active
	if err := r.users.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return u
}

// deactivate writes straight into the fake's storage rather than mutating what
// a read returned -- ReadByEmail hands back a copy, as postgres would, so a
// mutation on it would not stick and this precondition would silently not
// hold.
func (r *signInRig) deactivate(t *testing.T, email string) {
	t.Helper()

	r.users.mu.Lock()
	defer r.users.mu.Unlock()
	row, ok := r.users.rows[email]
	if !ok {
		t.Fatalf("no such user: %q", email)
	}
	row.IsActive = false
}

// The whole security argument in one test: a new address, a known one, and a
// deactivated one all answer the same way.
func TestRequestingACodeTellsYouNothing(t *testing.T) {
	r := newSignInRig(t)

	known := "known@acme.test"
	r.addUser(t, known, true)
	r.addUser(t, "off@acme.test", false)

	for _, email := range []string{known, "brand-new@acme.test", "off@acme.test"} {
		if err := r.signIn.Request(context.Background(), email); err != nil {
			t.Errorf("Request(%q) = %v, want no error whatever the address", email, err)
		}
	}
}

// An address nobody has used becomes a customer on the way through. There is no
// separate "create an account", because two flows would answer differently and
// anybody could tell a taken address from a free one.
func TestANewAddressBecomesACustomer(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "brand-new@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}

	u, err := r.users.ReadByEmail(context.Background(), "brand-new@acme.test")
	if err != nil {
		t.Fatalf("the address did not become a user: %v", err)
	}
	if u.Role != user.RoleCustomer {
		t.Errorf("role = %q, want customer", u.Role)
	}
	if !u.IsActive {
		t.Error("the account it made cannot sign in")
	}
}

// A deactivated person gets the same sentence and no code.
func TestADeactivatedPersonIsSentNothing(t *testing.T) {
	r := newSignInRig(t)
	r.addUser(t, "off@acme.test", false)

	if err := r.signIn.Request(context.Background(), "off@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(r.mail.sent) != 0 {
		t.Errorf("sent %d codes to somebody who may not sign in", len(r.mail.sent))
	}
}

func TestTheRightCodeBeginsASession(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	sent := r.mail.sent[0]

	s, err := r.signIn.Verify(context.Background(), "a@acme.test", sent.code)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if s == nil || s.UserID == "" {
		t.Fatal("no session")
	}
}

func TestAWrongCodeSpendsIt(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	sent := r.mail.sent[0]

	if _, err := r.signIn.Verify(context.Background(), "a@acme.test", "000000"); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	if _, err := r.signIn.Verify(context.Background(), "a@acme.test", sent.code); err == nil {
		t.Error("the right code still worked after a wrong guess")
	}
}

// Otherwise anybody can fill a stranger's inbox.
func TestAskingTooOften(t *testing.T) {
	r := newSignInRig(t)

	var err error
	for range usecase.MaxCodeRequests + 1 {
		err = r.signIn.Request(context.Background(), "a@acme.test")
	}
	if !errors.Is(err, logincode.ErrTooManyRequests) {
		t.Errorf("Request = %v, want ErrTooManyRequests", err)
	}
}

// Deactivating somebody ends their session on the next request, not when a
// token they hold happens to expire.
func TestDeactivationEndsTheSessionAtOnce(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	s, err := r.signIn.Verify(context.Background(), "a@acme.test", r.mail.sent[0].code)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, err := r.signIn.Whoami(context.Background(), s.ID); err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	r.deactivate(t, "a@acme.test")

	if _, err := r.signIn.Whoami(context.Background(), s.ID); err == nil {
		t.Error("a deactivated person was still signed in")
	}
}

// Signing out ends the session, and the same cookie a second time is not a
// second sign-out.
func TestSigningOutEndsIt(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	s, err := r.signIn.Verify(context.Background(), "a@acme.test", r.mail.sent[0].code)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if err := r.signIn.End(context.Background(), s.ID); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := r.signIn.Whoami(context.Background(), s.ID); err == nil {
		t.Error("a session survived being signed out of")
	}
}
