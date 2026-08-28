package bootstrap

// Which binary this is. It goes on every log line, because both write to one
// collector and nothing else separates them there.
const (
	binaryGateway    = "gateway"
	binaryDispatcher = "dispatcher"
	binaryConsole    = "console"
)
