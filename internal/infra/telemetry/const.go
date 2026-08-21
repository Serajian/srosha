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

// The attributes every line carries, named once because the text handler has to
// recognize them to leave them out.
const (
	attrService = "service"
	attrBinary  = "binary"

	// clockOnly is the timestamp a person needs. The date and the offset are in
	// the json record, which is what anything but a person reads.
	clockOnly = "15:04:05.000"
)
