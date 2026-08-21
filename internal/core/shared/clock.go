package shared

import "time"

// A single-method interface and a function are the same contract, and the
// function is lighter. These exist so the domain never reads ambient state:
// a test hands it a fixed time and a known sequence of ids.
type (
	NowFunc func() time.Time
	IDFunc  func() ID
)
