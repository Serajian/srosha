package postgres

import (
	"context"
	"encoding/json"

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
		OwnerUserID:        s.OwnerUserID.String(),
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

// ListByOwner is a customer's own page, and the whole of the ownership rule:
// one WHERE clause, so nothing above it has to remember to filter.
func (r *SourceRepository) ListByOwner(
	ctx context.Context, ownerID shared.ID,
) ([]source.Source, error) {
	rows, err := r.q(ctx).ListSourcesByOwner(ctx, ownerID.String())
	if err != nil {
		return nil, failed("list sources by owner", err)
	}
	return toSources(rows)
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

// UpdateSettings writes the three columns the customer owns.
//
// There is no method here that writes them all. There was one -- Update, which
// carried max_priority and allow_custom_address too -- and it went with
// Deactivate and Activate once nothing in production called any of the three:
// Activate set is_active without touching approved_at or reviewed_at, which is
// a fifth state the table has no meaning for. A source moves in exactly two
// ways now, and which columns each may reach is the whole difference between
// them: this, and UpdateReview.
func (r *SourceRepository) UpdateSettings(ctx context.Context, s *source.Source) error {
	addresses, err := fromAddresses(s.DefaultAddresses)
	if err != nil {
		return badRow("source", s.ID, "default_addresses", err)
	}

	rows, err := r.q(ctx).UpdateSourceSettings(ctx, gen.UpdateSourceSettingsParams{
		ID:               s.ID,
		Name:             s.Name,
		Description:      s.Description,
		DefaultAddresses: addresses,
		UpdatedAt:        s.UpdatedAt,
	})
	if err != nil {
		return failed("update source settings", err)
	}
	if rows == 0 {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return nil
}

func (r *SourceRepository) UpdateReview(ctx context.Context, s *source.Source) error {
	rows, err := r.q(ctx).UpdateReview(ctx, gen.UpdateReviewParams{
		ID:         s.ID,
		IsActive:   s.IsActive,
		ApprovedAt: s.ApprovedAt,
		ReviewedAt: s.ReviewedAt,
		ReviewNote: s.ReviewNote,
		UpdatedAt:  s.UpdatedAt,
	})
	if err != nil {
		return failed("update review", err)
	}
	if rows == 0 {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return nil
}

func (r *SourceRepository) ListForReview(ctx context.Context) ([]source.Source, error) {
	rows, err := r.q(ctx).ListForReview(ctx)
	if err != nil {
		return nil, failed("list for review", err)
	}
	return toSources(rows)
}

func (r *SourceRepository) ListAll(ctx context.Context) ([]source.Source, error) {
	rows, err := r.q(ctx).ListAllSources(ctx)
	if err != nil {
		return nil, failed("list all sources", err)
	}
	return toSources(rows)
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
		OwnerUserID:        shared.ID(row.OwnerUserID),
		Name:               row.Name,
		Description:        row.Description,
		MaxPriority:        priority,
		IsActive:           row.IsActive,
		ApprovedAt:         row.ApprovedAt,
		ReviewedAt:         row.ReviewedAt,
		ReviewNote:         row.ReviewNote,
		AllowCustomAddress: row.AllowCustomAddress,
		DefaultAddresses:   addresses,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}, nil
}

// toSources maps a page of rows the same way toSource maps one, so the three
// listing methods share the same rules for a bad row instead of each risking
// its own.
func toSources(rows []gen.Source) ([]source.Source, error) {
	out := make([]source.Source, 0, len(rows))
	for _, row := range rows {
		s, err := toSource(row)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, nil
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
