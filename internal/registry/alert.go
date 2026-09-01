package registry

import "context"

// Closer is something opened elsewhere that still has to be closed in order.
//
// Most things here are opened by this package, which is what keeps
// internal/infra free of anything srosha knows. An alerter is the exception on
// purpose: it is an adapter, and adapters are bootstrap's to build. What it
// still needs from here is a place in the shutdown order.
type Closer interface {
	Close(context.Context) error
}

// Alerts records an already-built alerter so shutdown drains it.
//
// tierClient: it is outbound, and nothing inside the process waits on it. It
// stops after the listeners and before the store, so an alert raised on the
// way down still has somewhere to go.
func (r *Resources) Alerts(name string, c Closer) {
	r.add(step{tier: tierClient, name: name, close: c.Close})
}
