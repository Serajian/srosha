package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceRepository implements source.Repository.
type SourceRepository struct{ base }

func NewSourceRepository(pool *pgxpool.Pool) *SourceRepository {
	return &SourceRepository{base{pool: pool}}
}

// Create registers a source. is_active is not passed: a new source is switched
// on, and the column default says so.
func (r *SourceRepository) Create(ctx context.Context, s *source.Source) error {
	addresses, err := fromAddresses(s.DefaultAddresses)
	if err != nil {
		return badRow("source", s.ID, "default_addresses", err)
	}

	err = r.q(ctx).CreateSource(ctx, gen.CreateSourceParams{
		ID:                 s.ID,
		Name:               s.Name,
		MaxPriority:        s.MaxPriority.String(),
		AllowCustomAddress: s.AllowCustomAddress,
		DefaultAddresses:   addresses,
		CreatedAt:          s.CreatedAt,
	})
	if violates(err, "sources_pkey") {
		return errs.DuplicateErr("source already exists").WithStr(s.ID)
	}
	if err != nil {
		return failed("create source", err)
	}
	return nil
}

// ReadByID returns a suspended source like any other. Refusing it here would
// report "no such source" for an id that is perfectly correct; the domain's
// EnsureActive says what is actually wrong.
func (r *SourceRepository) ReadByID(ctx context.Context, id string) (*source.Source, error) {
	row, err := r.q(ctx).ReadSource(ctx, id)
	switch {
	case noRows(err):
		return nil, errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	case err != nil:
		return nil, failed("read source", err)
	}
	return toSource(row)
}

// Update writes what changes over a customer's life. Switching a source off is
// not one of them -- that is Deactivate, so a rename cannot flip it by accident.
func (r *SourceRepository) Update(ctx context.Context, s *source.Source, now time.Time) error {
	addresses, err := fromAddresses(s.DefaultAddresses)
	if err != nil {
		return badRow("source", s.ID, "default_addresses", err)
	}

	rows, err := r.q(ctx).UpdateSource(ctx, gen.UpdateSourceParams{
		ID:                 s.ID,
		Name:               s.Name,
		MaxPriority:        s.MaxPriority.String(),
		AllowCustomAddress: s.AllowCustomAddress,
		DefaultAddresses:   addresses,
		UpdatedAt:          now,
	})
	if err != nil {
		return failed("update source", err)
	}
	if rows == 0 {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return nil
}

// Deactivate and Activate report a source that was already in the asked-for
// state as not found, because nothing was changed and saying otherwise would
// claim an act that did not happen.
func (r *SourceRepository) Deactivate(ctx context.Context, id string, now time.Time) error {
	rows, err := r.q(ctx).DeactivateSource(ctx, gen.DeactivateSourceParams{ID: id, UpdatedAt: now})
	return changed(rows, err, "deactivate source")
}

func (r *SourceRepository) Activate(ctx context.Context, id string, now time.Time) error {
	rows, err := r.q(ctx).ActivateSource(ctx, gen.ActivateSourceParams{ID: id, UpdatedAt: now})
	return changed(rows, err, "activate source")
}

func changed(rows int64, err error, op string) error {
	if err != nil {
		return failed(op, err)
	}
	if rows == 0 {
		return errs.NotFoundErr("nothing to change").WithStr(op)
	}
	return nil
}

// --- mapping -----------------------------------------------------------------

func toSource(row gen.Source) (*source.Source, error) {
	priority, err := shared.ParsePriority(row.MaxPriority)
	if err != nil {
		return nil, badRow("source", row.ID, "max_priority", err)
	}

	addresses, err := toAddresses(row.DefaultAddresses)
	if err != nil {
		return nil, badRow("source", row.ID, "default_addresses", err)
	}

	return &source.Source{
		ID:                 row.ID,
		Name:               row.Name,
		MaxPriority:        priority,
		IsActive:           row.IsActive,
		AllowCustomAddress: row.AllowCustomAddress,
		DefaultAddresses:   addresses,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}, nil
}

// toAddresses reads the jsonb map. A null or absent value is an empty map and
// not an error: a source with no defaults configured is ordinary, and Resolve
// already refuses a channel it finds nothing for.
func toAddresses(raw []byte) (map[shared.Channel]string, error) {
	if len(raw) == 0 {
		return map[shared.Channel]string{}, nil
	}

	var addresses map[shared.Channel]string
	if err := json.Unmarshal(raw, &addresses); err != nil {
		return nil, err
	}
	if addresses == nil {
		addresses = map[shared.Channel]string{}
	}
	return addresses, nil
}

func fromAddresses(addresses map[shared.Channel]string) ([]byte, error) {
	if addresses == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(addresses)
}
