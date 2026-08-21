// Package env reads configuration from the process environment.
//
// It collects every failure instead of stopping at the first, so one restart
// tells you about all the missing keys rather than the next one each time.
package env

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Reader reads keys under one prefix and remembers what went wrong.
type Reader struct {
	prefix string
	errs   []error
}

func New(prefix string) *Reader { return &Reader{prefix: prefix} }

// Err returns everything that went wrong, or nil. Reading stops being useful
// after this returns non-nil: the values it produced are defaults, not config.
func (r *Reader) Err() error {
	if len(r.errs) == 0 {
		return nil
	}
	return errors.Join(r.errs...)
}

func (r *Reader) key(name string) string { return r.prefix + name }

func (r *Reader) lookup(name string) (string, bool) {
	v, ok := os.LookupEnv(r.key(name))
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (r *Reader) fail(name, detail string) {
	r.errs = append(r.errs, fmt.Errorf("%s: %s", r.key(name), detail))
}

// Str returns the value or the fallback.
func (r *Reader) Str(name, fallback string) string {
	if v, ok := r.lookup(name); ok {
		return v
	}
	return fallback
}

// Required fails when the key is missing, because some values have no sensible
// default and starting without them only moves the failure to the first request.
func (r *Reader) Required(name string) string {
	v, ok := r.lookup(name)
	if !ok {
		r.fail(name, "is required")
	}
	return v
}

func (r *Reader) RequiredSecret(name string) Secret { return Secret(r.Required(name)) }

func (r *Reader) Secret(name, fallback string) Secret {
	return Secret(r.Str(name, fallback))
}

func (r *Reader) Int(name string, fallback int) int {
	v, ok := r.lookup(name)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.fail(name, fmt.Sprintf("%q is not a whole number", v))
		return fallback
	}
	return n
}

func (r *Reader) Bool(name string, fallback bool) bool {
	v, ok := r.lookup(name)
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.fail(name, fmt.Sprintf("%q is not true or false", v))
		return fallback
	}
	return b
}

func (r *Reader) Duration(name string, fallback time.Duration) time.Duration {
	v, ok := r.lookup(name)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.fail(name, fmt.Sprintf("%q is not a duration such as 30s or 5m", v))
		return fallback
	}
	return d
}

// JSON reads a value that is a JSON document. An environment variable has no
// nesting, so a map whose keys are not known in advance -- one entry per
// source, say -- arrives this way.
func (r *Reader) JSON(name string, target any) {
	v, ok := r.lookup(name)
	if !ok {
		return
	}
	if err := json.Unmarshal([]byte(v), target); err != nil {
		// The value itself is not echoed: it may be a map of secrets.
		r.fail(name, fmt.Sprintf("is not valid json: %v", err))
	}
}

// Check records a rule the values must satisfy together, which no single key
// can enforce on its own.
func (r *Reader) Check(ok bool, format string, args ...any) {
	if !ok {
		r.errs = append(r.errs, fmt.Errorf(format, args...))
	}
}
