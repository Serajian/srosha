package telemetry

// The accepted values, named once. config validates the same set to give the
// operator a readable message; this package validates it again because it does
// not trust its caller.
const (
	levelDebug = "debug"
	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"

	formatJSON = "json"
	formatText = "text"
)
