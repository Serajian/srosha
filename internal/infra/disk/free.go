// Package disk answers one question: how much room is left where this process
// writes.
//
// It is infrastructure because it is a syscall, and it is its own package
// because that syscall is the only thing in it.
package disk

import "syscall"

// Free reports the bytes available at path, and the size of the filesystem
// holding it.
//
// Available, not free: the two differ by the reserve a filesystem keeps for
// root, and a service that does not run as root cannot have those bytes.
// Reporting them would promise room that is not there.
//
// Inside a container this reads the filesystem behind the overlay, which is the
// host's. Measured on the deployed host on 2026-09-01: `df /` inside the
// dispatcher and on the host agreed to within rounding, 95.8G against 96G. That
// is what makes the question worth asking from inside the application rather
// than from a script beside it.
func Free(path string) (available, total uint64, err error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, err
	}
	// Bsize is int64 on linux and uint32 on darwin; the conversion is what
	// lets one file build on the host it deploys to and the laptop it is
	// written on.
	return s.Bavail * uint64(s.Bsize), s.Blocks * uint64(s.Bsize), nil
}
