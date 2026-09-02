package public_test

import (
	"os"
	"testing"
)

// The portal serves the SDK's README on /reference, and it serves a COPY,
// because an embed directive cannot reach outside this directory -- and sdk/go
// is a module of its own besides, which the server ships beside and may not
// import.
//
// A copy is a second truth, and this is the only thing standing between it and
// a customer reading last month's instructions. `make sdk-docs` is the fix.
func TestThePortalsCopyOfTheSDKReadmeIsCurrent(t *testing.T) {
	source, err := os.ReadFile("../sdk/go/README.md")
	if err != nil {
		t.Fatalf("could not read the SDK's README: %v", err)
	}

	served, err := os.ReadFile("guarded/portal/sdk.md")
	if err != nil {
		t.Fatalf("the portal has no copy of the SDK's README: %v", err)
	}

	if string(served) != string(source) {
		t.Errorf(
			"the portal serves a stale README (%d bytes against the SDK's %d).\n"+
				"run: make sdk-docs",
			len(served), len(source),
		)
	}
}
