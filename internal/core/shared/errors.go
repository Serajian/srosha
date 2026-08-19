package shared

import "errors"

// Sentinel errors for the value objects in this package.
//
// These carry IDENTITY, not presentation. A fresh errs.AppError has no
// identity of its own -- every bad input is ErrInvalidInput, so errors.Is
// cannot tell a malformed target from an empty body. Wrapping a sentinel with
// WithErr restores that distinction:
//
//	errs.InvalidInputErr("invalid target").WithErr(ErrInvalidTarget)
//
//	errors.Is(err, shared.ErrInvalidTarget)   // exact cause
//	errs.IsType(err, errs.ErrInvalidInput)    // how to answer the client
//
// Only errors belonging to a type in this package live here. Aggregate errors
// (ErrSourceInactive, ErrInvalidTransition, ...) belong to their own domain
// package, so that the vocabulary of each aggregate stays readable on its own.
var (
	// id.go
	ErrInvalidID = errors.New("invalid id")

	// channel.go
	ErrUnknownChannel = errors.New("unknown channel")
	ErrEmptyTarget    = errors.New("delivery target is empty")
	ErrInvalidTarget  = errors.New("delivery target has the wrong format")

	// priority.go
	ErrUnknownPriority = errors.New("unknown priority")
)
