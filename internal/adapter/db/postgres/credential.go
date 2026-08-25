package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CredentialRepository implements credential.Repository.
//
// The table holds two different things and this serves them on two paths. The
// core is given identities without their secrets, because credential.Credential
// has nowhere to put one; the sender registry asks for the secret by id, at the
// moment it sends. Nothing reads both at once.
type CredentialRepository struct{ base }

func NewCredentialRepository(pool *pgxpool.Pool) *CredentialRepository {
	return &CredentialRepository{base{pool: pool}}
}

// Create registers an identity. is_active is not passed: a new credential is
// switched on, and the column default says so.
//
// Writing it as the default when the channel already has one is refused by the
// index rather than allowed to produce two. ClearDefault is the other half of
// that move and has to run first, in the same transaction.
func (r *CredentialRepository) Create(
	ctx context.Context, c *credential.Credential, config []byte, secret string,
) error {
	if len(config) == 0 {
		config = []byte("{}")
	}

	err := r.q(ctx).CreateCredential(ctx, gen.CreateCredentialParams{
		ID:        c.ID.String(),
		SourceID:  c.SourceID,
		Channel:   string(c.Channel),
		Name:      c.Name,
		Config:    config,
		Secret:    optional(secret),
		IsDefault: c.IsDefault(),
		CreatedAt: c.CreatedAt,
	})
	switch {
	case violates(err, "credentials_source_channel_name_key"):
		return errs.DuplicateErr("a credential by that name already exists on this channel").
			WithStr(fmt.Sprintf("source %q, channel %q, name %q", c.SourceID, c.Channel, c.Name))
	case violates(err, "credentials_one_default_per_channel"):
		return errs.DuplicateErr("this channel already has a default credential").
			WithStr(fmt.Sprintf("source %q, channel %q", c.SourceID, c.Channel))
	case violates(err, "credentials_pkey"):
		return errs.DuplicateErr("credential already exists").WithStr(c.ID.String())
	case err != nil:
		return failed("create credential", err)
	}
	return nil
}

// ListBySourceAndChannel hands over the whole set, switched-off ones included,
// and lets credential.Pick choose. Filtering here would report a disabled
// identity as one that does not exist, and the source would go looking for a
// typo instead of turning it back on.
func (r *CredentialRepository) ListBySourceAndChannel(
	ctx context.Context, sourceID string, c shared.Channel,
) ([]credential.Credential, error) {
	rows, err := r.q(ctx).ListCredentialsBySourceAndChannel(
		ctx, gen.ListCredentialsBySourceAndChannelParams{
			SourceID: sourceID,
			Channel:  string(c),
		})
	if err != nil {
		return nil, failed("list credentials", err)
	}

	out := make([]credential.Credential, 0, len(rows))
	for _, row := range rows {
		cred, err := toCredential(row)
		if err != nil {
			return nil, err
		}
		out = append(out, cred)
	}
	return out, nil
}

// ReadMaterial returns what a sender needs to build its client: the provider
// settings, and the secret exactly as stored -- still encrypted. Decrypting is
// not this layer's business; the value names the key that produced it, and
// whoever holds the keys opens it.
//
// The config stays raw json. What a provider needs is the sender's own shape,
// and giving it one here would make a second provider a change in the database
// package.
//
// A missing row and a deactivated one arrive here as the same silence, because
// the statement asks for an active row by id. On the send path the second is the
// likely one -- the identity was chosen moments ago and switched off since -- so
// the message says both.
func (r *CredentialRepository) ReadMaterial(
	ctx context.Context, id shared.ID,
) (config []byte, secret string, err error) {
	row, err := r.q(ctx).ReadCredentialSecret(ctx, id.String())
	switch {
	case noRows(err):
		return nil, "", errs.NotFoundErr("no active credential with that id").
			WithErr(credential.ErrNotFound).
			WithStr(id.String())
	case err != nil:
		return nil, "", failed("read credential secret", err)
	}
	return row.Config, deref(row.Secret), nil
}

// Reseal replaces a stored secret with the same secret under the current key.
//
// It reports whether the row was actually rewritten. Finding nothing is not a
// failure: two senders reading one credential at the same moment both reseal,
// and only one of them can be the row -- the other sealed the same value and
// its write is not needed. The statement matches on the old value so the loser
// writes nothing rather than overwriting the winner.
//
// The values are opaque here. What "sealed" means, and which key did it, is the
// business of whoever holds the keys.
func (r *CredentialRepository) Reseal(
	ctx context.Context, id shared.ID, previous, secret string, now time.Time,
) (bool, error) {
	rows, err := r.q(ctx).ResealCredentialSecret(ctx, gen.ResealCredentialSecretParams{
		ID:        id.String(),
		Previous:  optional(previous),
		Secret:    optional(secret),
		UpdatedAt: now,
	})
	if err != nil {
		return false, failed("reseal credential secret", err)
	}
	return rows > 0, nil
}

// ClearDefault takes the flag off whichever credential holds it, so the next one
// can take it. Finding none is not a failure: a channel with no default yet is
// the ordinary case for the first credential on it.
//
// It only ever makes sense next to the write that takes the flag over, and both
// have to be in one transaction -- alone, it leaves the channel with no default
// at all and every message that names no identity fails.
func (r *CredentialRepository) ClearDefault(
	ctx context.Context, sourceID string, c shared.Channel, now time.Time,
) error {
	_, err := r.q(ctx).ClearDefaultCredential(ctx, gen.ClearDefaultCredentialParams{
		SourceID:  sourceID,
		Channel:   string(c),
		UpdatedAt: now,
	})
	if err != nil {
		return failed("clear default credential", err)
	}
	return nil
}

// --- mapping -----------------------------------------------------------------

func toCredential(row gen.ListCredentialsBySourceAndChannelRow) (credential.Credential, error) {
	channel := shared.Channel(row.Channel)
	if !channel.Valid() {
		return credential.Credential{},
			badRow("credential", row.ID, "channel", shared.ErrUnknownChannel)
	}

	return *credential.Restore(credential.Snapshot{
		ID:        shared.ID(row.ID),
		SourceID:  row.SourceID,
		Channel:   channel,
		Name:      row.Name,
		IsDefault: row.IsDefault,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}), nil
}
