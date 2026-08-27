# SDKs

One directory per language. Every one of them is a gRPC client, because gRPC is
the only surface srosha has and none is planned.

```
api/proto/notification/v1/*.proto     the one contract
              │
              │ buf generate
              ▼
sdk/go/                               module github.com/Serajian/srosha/sdk/go
```

They live here rather than in repositories of their own so that the proto cannot
drift away from them. A separate repository would need the contract copied and
kept in step by hand, and that goes wrong quietly — worse with every language
added, not better.

## The rule every language SDK follows

- **It is a module of its own**, so a customer gets the contract and nothing
  else, and so its version number is its own. A server release with nothing in
  it for a customer must not move the number they pin.
- **It never imports `internal/`.** It is on the other side of the wire from
  this service, and shares nothing with it but the proto.
- **Its dependencies are deliberately few.** For Go that is grpc and protobuf,
  and that is all.
- **It hides the transport.** A customer should never have to name a protobuf
  type or a gRPC status code to use it.

## Go

```
go get github.com/Serajian/srosha/sdk/go
```

`sdk/go/notification/v1` is the generated contract, and the server reads it from
here too. `sdk/go/srosha` is what a customer imports.

The design, and what was decided against, is in
`docs/superpowers/specs/2026-08-27-go-sdk-design.md`.
