package web

import "github.com/Serajian/srosha/pkg/errs"

// message is the only part of an error a page may show.
//
// Same rule as the gRPC side: the message was written for a person, and the
// reason names columns, hosts and rejected values and belongs in the log. An
// error that is not one of ours says nothing about itself, because its text was
// written by a library for us rather than by us for a customer.
func message(err error) string {
	appErr, ok := errs.As(err)
	if !ok {
		return "Something went wrong. Try again."
	}
	return appErr.Message()
}
