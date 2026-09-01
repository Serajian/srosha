package bootstrap

import (
	"context"

	"github.com/Serajian/srosha/internal/infra/disk"
)

// The two ports the capacity alert needs that nothing else provides in the
// shape it wants.
//
// Both are a few lines and a type, because the core may not import infra and
// infra does not know the core's ports exist. bootstrap is the one place that
// sees both -- the same reason the gotify sender for operator alerts is built
// here rather than inside the alert package. See make arch-check.

type diskFree struct{}

func (diskFree) Free(path string) (available, total uint64, err error) {
	return disk.Free(path)
}

// jetstreamBytes renames rather than reimplements. The port is Bytes because
// the postgres side is a SizeReporter and the word is enough there; on a NATS
// connection a method called Bytes says nothing, so infra calls it
// StoredBytes and the adapting happens here.
type jetstreamBytes struct {
	mq interface {
		StoredBytes(ctx context.Context) (uint64, error)
	}
}

func (j jetstreamBytes) Bytes(ctx context.Context) (uint64, error) {
	return j.mq.StoredBytes(ctx)
}
