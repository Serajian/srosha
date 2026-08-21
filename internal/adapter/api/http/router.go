// Package http is every http handler this service serves, in one place. The
// gRPC side sits beside it in api/grpc; both are driving adapters, and neither
// knows the other exists.
//
// The package name shadows net/http, which is why the standard library is
// imported as nethttp here. That matches api/grpc, which shadows the grpc
// package the same way.
package http

import (
	"context"
	"errors"
	"log/slog"
	nethttp "net/http"
)

// Check is one dependency and what it answered. Err is nil when it is fine.
//
// This package declares its own rather than taking registry's: it is an
// adapter, and adapters may not see registry. Bootstrap, which sees both,
// translates.
type Check struct {
	Name string
	Err  error
}

// Deps is what the routes need. It grows as the surface does, and everything in
// it was built by bootstrap -- this package opens nothing and reads no config.
type Deps struct {
	// Binary is which process this is. It goes in the readiness body, so an
	// operator holding one response knows which one produced it.
	Binary string

	// Ready reports each open dependency. This package does not know what is
	// open, and must not.
	Ready func(context.Context) []Check

	Log *slog.Logger
}

func (d Deps) validate() error {
	var errs []error

	if d.Binary == "" {
		errs = append(errs, errors.New("binary is empty"))
	}
	if d.Ready == nil {
		errs = append(errs, errors.New("no readiness check"))
	}
	if d.Log == nil {
		errs = append(errs, errors.New("no logger"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// New builds the whole http surface. Every route this service answers is
// mounted from here, so nobody has to grep the package to find out what it
// serves.
func New(d Deps) (nethttp.Handler, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	mux := nethttp.NewServeMux()
	mountHealth(mux, d)
	return mux, nil
}
