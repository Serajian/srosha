// Package googleauth turns a Google service account into the short-lived access
// token Google's APIs actually accept, and keeps that token alive.
//
// It exists because this is the first credential in the service that is not the
// thing you send. A bot token goes straight into a header; a service account is
// a private key, and using it means signing an assertion, handing it to Google
// and getting back a token that lasts about an hour. Doing that per message
// would put an RSA signature and a round trip in front of every push.
//
// So the exchange happens here, once per account, and the result is reused
// until it expires. It knows nothing about what the token is then used for.
package googleauth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ErrRejected means Google refused the account rather than failed to answer:
// it does not exist, it has been disabled, or it was never granted the scope.
// Another attempt gets the same answer.
//
// It exists because a well-formed key file is not the same as a working one,
// and only the exchange knows the difference. Without it a deleted service
// account would look like Google being briefly unreachable and be retried until
// the attempts ran out.
var ErrRejected = errors.New("google refused the service account")

// The error codes Google uses when the account itself is the problem. Anything
// else -- a 5xx, a timeout, a connection refused -- is worth asking again.
var finalCodes = map[string]bool{
	"invalid_grant":       true,
	"invalid_client":      true,
	"unauthorized_client": true,
	"invalid_scope":       true,
	"access_denied":       true,
}

func rejected(err error) bool {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return false
	}
	if re.Response != nil && re.Response.StatusCode >= 500 {
		// Google's, and Google's to fix, whatever it called it.
		return false
	}

	// ErrorCode is read out of the body rather than off the struct: the jwt
	// flow builds this error with only the response and the bytes, and leaves
	// the parsed field empty. Checked first anyway, in case that changes.
	code := re.ErrorCode
	if code == "" {
		var answer struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(re.Body, &answer); err != nil {
			return false
		}
		code = answer.Error
	}
	return finalCodes[code]
}

// Account is what a service account says about itself, minus the key.
//
// The project id is here because callers need it -- FCM puts it in the url --
// and asking them to reach into the json themselves would put the shape of
// Google's file in every package that uses one.
type Account struct {
	ProjectID string
	Email     string
}

// Minter hands out tokens for service accounts, one Source per account.
//
// Safe for concurrent use: the dispatcher resolves a sender per message, and
// several may want the same account at once.
type Minter struct {
	scopes []string

	// base carries the http client, because that is the only way oauth2 accepts
	// one. It is the minter's lifetime and not a request's, which is why a
	// context lives in a struct here: the token outlives every request that
	// waited for it.
	base context.Context //nolint:containedctx // see above

	mu      sync.Mutex
	sources map[[32]byte]*Source
}

// NewMinter takes the client rather than making one, so token traffic uses the
// same timeouts, proxy and connection pool as everything else this service
// dials.
func NewMinter(client *http.Client, scopes ...string) (*Minter, error) {
	if client == nil {
		return nil, errors.New("googleauth: no http client")
	}
	if len(scopes) == 0 {
		return nil, errors.New("googleauth: no scopes")
	}
	return &Minter{
		scopes:  scopes,
		base:    context.WithValue(context.Background(), oauth2.HTTPClient, client),
		sources: make(map[[32]byte]*Source),
	}, nil
}

// Open prepares a token source for one service account. It talks to nobody: the
// first token is minted when one is first asked for.
//
// Calling it again with the same account hands back the same source, which is
// the point -- a second source would mean a second token where one was still
// good.
func (m *Minter) Open(serviceAccount []byte) (*Source, error) {
	account, err := ParseAccount(serviceAccount)
	if err != nil {
		return nil, err
	}

	// The key rather than the file: a map key is copied and compared and lives
	// as long as the map, and none of that should be true of a private key.
	sum := sha256.Sum256(serviceAccount)

	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sources[sum]; ok {
		return s, nil
	}

	cfg, err := google.JWTConfigFromJSON(serviceAccount, m.scopes...)
	if err != nil {
		// Never wrapped: the parse error can quote the file it failed on.
		return nil, errors.New("googleauth: service account is not usable")
	}
	if err := checkKey(cfg.PrivateKey); err != nil {
		return nil, err
	}

	if len(m.sources) >= maxCachedAccounts {
		clear(m.sources)
	}

	s := &Source{account: account, tokens: cfg.TokenSource(m.base)}
	m.sources[sum] = s
	return s, nil
}

// Source is one service account's supply of tokens.
type Source struct {
	account Account
	tokens  oauth2.TokenSource
}

// Account is what the file said about itself.
func (s *Source) Account() Account { return s.account }

// Token returns one that is currently valid, minting a new one only when the
// last has expired or there has not been one yet.
//
// The context bounds the wait, not the exchange: oauth2 refreshes through the
// client it was given and has no way to be told to stop. A caller that gives up
// leaves a refresh finishing on its own, and the next caller gets its result.
func (s *Source) Token(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	type result struct {
		token *oauth2.Token
		err   error
	}
	done := make(chan result, 1)
	go func() {
		t, err := s.tokens.Token()
		done <- result{token: t, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("googleauth: token for %s: %w", s.account.Email, ctx.Err())
	case r := <-done:
		if r.err != nil {
			// Google's own message, which says what is wrong with the account
			// and carries no key material.
			if rejected(r.err) {
				return "", fmt.Errorf("googleauth: %w for %s: %w",
					ErrRejected, s.account.Email, r.err)
			}
			return "", fmt.Errorf("googleauth: token for %s: %w", s.account.Email, r.err)
		}
		return r.token.AccessToken, nil
	}
}

// ParseAccount reads the parts of a service account file that are not secret.
func ParseAccount(serviceAccount []byte) (Account, error) {
	var file struct {
		Type      string `json:"type"`
		ProjectID string `json:"project_id"`
		Email     string `json:"client_email"`
	}
	if err := json.Unmarshal(serviceAccount, &file); err != nil {
		// The file is mostly a private key, so nothing from it is quoted.
		return Account{}, errors.New("googleauth: service account is not valid json")
	}

	switch {
	case file.Type != "service_account":
		// The other kind of credentials json is a user's, and it has no key to
		// sign with. Caught here, where it can be said plainly, rather than as
		// a missing field somewhere further in.
		return Account{}, errors.New("googleauth: not a service account key file")
	case file.ProjectID == "":
		return Account{}, errors.New("googleauth: service account names no project")
	case file.Email == "":
		return Account{}, errors.New("googleauth: service account has no client_email")
	}
	return Account{ProjectID: file.ProjectID, Email: file.Email}, nil
}

// checkKey makes sure the file's private key is one that can actually sign.
//
// oauth2 does not do this: JWTConfigFromJSON takes the field as it finds it and
// discovers the problem at the first signature -- which would be at the first
// message, as a failure that looks temporary and is not. A key that is wrong is
// a configuration mistake, and this is where it can still be answered as one.
func checkKey(pemKey []byte) error {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return errors.New("googleauth: service account private key is not pem")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if _, ok := key.(*rsa.PrivateKey); !ok {
			return errors.New("googleauth: service account key is not rsa")
		}
		return nil
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		// The error itself is not carried: it is about the bytes of a key.
		return errors.New("googleauth: service account private key is not usable")
	}
	return nil
}
