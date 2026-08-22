package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// The mappers are the whole reason this package exists. A generated struct is
// the shape of a table; an entity is the shape of the business, and the two are
// allowed to disagree.
//
// A row that will not map is an internal error, never an invalid input: it was
// valid when it was written, so if it is unreadable now something on our side
// changed -- a column, an enum, a migration -- and no client can fix it.

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

func badRow(table, id, column string, err error) error {
	return errs.InternalErr("stored data could not be read").
		WithStr(fmt.Sprintf("%s %q: column %s", table, id, column)).
		WithErr(err)
}
