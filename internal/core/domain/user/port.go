package user

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Repository is where people are kept.
//
// ReadByEmail answers ErrNotFound rather than nil for an address nobody has
// used: sign-in has to tell "no such person" from "a person we could not read",
// and a nil with no error makes those the same.
type Repository interface {
	Create(ctx context.Context, u *User) error
	ReadByEmail(ctx context.Context, email string) (*User, error)
	ReadByID(ctx context.Context, id shared.ID) (*User, error)
	// List is every account, capped at limit -- the caller decides how many
	// rows one screen may hold.
	List(ctx context.Context, limit int32) ([]User, error)
	UpdateRole(ctx context.Context, u *User) error
	SetActive(ctx context.Context, u *User) error
}
