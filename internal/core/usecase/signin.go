package usecase

import (
	"context"
	"errors"

	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Mailer sends a code.
//
// Declared here rather than imported, because whoever can reach a mail server
// satisfies it. It is deliberately not srosha's own sending path: a sign-in
// that goes through the service you are signing in to fix is a trap.
type Mailer interface {
	SendCode(ctx context.Context, email, code string) error
}

// SignIn is how somebody proves who they are.
type SignIn struct {
	users    user.Repository
	codes    logincode.Repository
	sessions session.Repository
	mail     Mailer
	newID    shared.IDFunc
	now      shared.NowFunc
}

func NewSignIn(
	users user.Repository, codes logincode.Repository, sessions session.Repository,
	mail Mailer, newID shared.IDFunc, now shared.NowFunc,
) *SignIn {
	return &SignIn{
		users:    users,
		codes:    codes,
		sessions: sessions,
		mail:     mail,
		newID:    newID,
		now:      now,
	}
}

// Request sends a code, and answers the same way for every address.
//
// A new one becomes a customer on the way through: signing up and signing in
// are one flow, because two would answer differently and anybody could tell a
// taken address from a free one.
//
// A deactivated person is sent nothing and told the same thing as everybody
// else. The only error a caller ever sees from here is the request limit, which
// is about them rather than about whose address it is.
func (s *SignIn) Request(ctx context.Context, email string) error {
	address, err := user.NormalizeEmail(email)
	if err != nil {
		return err
	}

	u, err := s.find(ctx, address)
	if err != nil {
		return err
	}
	// Read rather than EnsureActive: this is not an error path. Somebody who
	// may not sign in is sent nothing and told exactly what everybody else is
	// told, so there is no error here to return or to swallow.
	if u == nil || !u.IsActive {
		return nil
	}

	now := s.now()
	n, err := s.codes.CountSince(ctx, u.ID, now.Add(-CodeRequestWindow))
	if err != nil {
		return err
	}
	if n >= MaxCodeRequests {
		return errs.TooManyErr("too many sign-in requests").WithErr(logincode.ErrTooManyRequests)
	}

	code, err := logincode.Generate()
	if err != nil {
		return err
	}
	made := logincode.New(s.newID(), u.ID, code, now)
	if err := s.codes.Create(ctx, made); err != nil {
		return err
	}

	if err := s.mail.SendCode(ctx, u.Email, code); err != nil {
		// Nothing reached an inbox, so nothing was spent. Taking the row back
		// is what makes the count mean codes SENT rather than codes attempted
		// -- see Repository.Forget.
		//
		// Its own failure is ignored, and deliberately. The caller is already
		// being told the send failed, and that is the error worth having; a
		// second one about bookkeeping would replace it. This type has no
		// logger and does not need one for a line that costs, at worst, one
		// wasted allowance out of five.
		_ = s.codes.Forget(ctx, made.ID)
		return err
	}
	return nil
}

// find returns the person behind an address, creating the customer it will
// become if nobody has used it. It returns an error only for a database that
// would not answer.
func (s *SignIn) find(ctx context.Context, address string) (*user.User, error) {
	u, err := s.users.ReadByEmail(ctx, address)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, user.ErrNotFound) {
		return nil, err
	}

	fresh, err := user.New(s.newID(), address, user.RoleCustomer, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.users.Create(ctx, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// Verify checks an attempt and begins a session if it was right.
func (s *SignIn) Verify(ctx context.Context, email, code string) (*session.Session, error) {
	address, err := user.NormalizeEmail(email)
	if err != nil {
		return nil, err
	}

	u, err := s.users.ReadByEmail(ctx, address)
	if err != nil {
		return nil, refuseSignIn()
	}
	if err := u.EnsureActive(); err != nil {
		return nil, refuseSignIn()
	}

	stored, err := s.codes.ReadNewest(ctx, u.ID)
	if err != nil {
		return nil, refuseSignIn()
	}

	checkErr := stored.Check(code, s.now())
	// Written back whatever the answer, because the attempt itself is what
	// spends it.
	if err := s.codes.Spend(ctx, stored); err != nil {
		return nil, err
	}
	if checkErr != nil {
		return nil, checkErr
	}

	sess := session.New(s.newID(), u.ID, s.now())
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// Whoami is who a session belongs to, or an error if it no longer signs anybody
// in.
//
// The user's row is read every time rather than trusted from the session, which
// is what makes deactivating somebody take effect on their next request.
func (s *SignIn) Whoami(ctx context.Context, sessionID shared.ID) (*user.User, error) {
	sess, err := s.sessions.Read(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	if !sess.Valid(now) {
		return nil, errs.UnauthorizedErr("please sign in again").WithErr(session.ErrClosed)
	}

	u, err := s.users.ReadByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if err := u.EnsureActive(); err != nil {
		return nil, err
	}

	sess.Touch(now)
	if err := s.sessions.Touch(ctx, sess); err != nil {
		return nil, err
	}
	return u, nil
}

// End signs somebody out.
func (s *SignIn) End(ctx context.Context, sessionID shared.ID) error {
	return s.sessions.Delete(ctx, sessionID)
}

// refuseSignIn is the one answer every failed attempt gets, whatever went
// wrong. Saying which part was wrong tells whoever is guessing how close they
// got.
func refuseSignIn() error {
	return errs.UnauthorizedErr("that code is not right")
}
