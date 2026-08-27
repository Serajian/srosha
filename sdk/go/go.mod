// The client library, and the contract it speaks.
//
// A module of its own so that a customer importing it gets grpc and protobuf
// and nothing else -- not pgx, not nats, not this service's own dependencies --
// and so that its version number is its own. A server release with nothing in
// it for a customer should not move the number they pin.
//
// The go directive is deliberately below the server's. An SDK must not force a
// customer to upgrade their toolchain; 1.23 is the lowest that has
// range-over-func, which the listing iterator needs.
module github.com/Serajian/srosha/sdk/go

go 1.25.0

require (
	google.golang.org/grpc v1.83.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)
