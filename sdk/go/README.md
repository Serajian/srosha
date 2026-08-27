# srosha — Go SDK

The client for [srosha](../../README.md), an asynchronous notification service.
A source submits a message once; srosha delivers it out of band across its
channels, with at-least-once delivery and per-channel retry.

```
go get github.com/Serajian/srosha/sdk/go
```

Nothing in this package's surface is protobuf or gRPC. Times are `time.Time`,
channels and priorities are strings, failures are errors `errors.Is`
understands.

## Send

```go
c, err := srosha.New(ctx, "srosha.acme.test:443", apiKey)
if err != nil {
    return err
}
defer c.Close()

r, err := c.Submit(ctx, srosha.Message{
    Title: "Your order shipped",
    Body:  "Tracking: 123",
    Routes: []srosha.Route{
        srosha.Email("a@b.test"),
        srosha.Telegram("123456789"),
    },
})
```

`Submit` returns as soon as the message is stored, not when it is delivered.
Ask what happened with `Get`, or register a webhook and be told.

**Retrying is safe.** If you leave `IdempotencyKey` empty one is generated for
the call, so a timeout followed by another attempt is the same message rather
than a second one. Two separate `Submit` calls get two keys and two messages,
which is correct — the same alert sent twice on purpose is a real thing.

## Ask what happened

```go
got, err := c.Get(ctx, r.ID)
for _, d := range got.Deliveries {
    fmt.Println(d.Channel, d.Status, d.Reason)
}
```

`Reason` is worth a look when a delivery failed. `FailureNotReachable` is the
one you can act on: the provider refused the *recipient* rather than the
message, so nothing you wrote differently would have helped and the address
itself is the problem.

```go
for n, err := range c.List(ctx, srosha.LastWeek) {
    if err != nil {
        return err
    }
    fmt.Println(n.ID, n.Title)
}
```

Pages are fetched as the loop asks for them, so breaking out stops the
requests. The window is a closed set rather than two timestamps because srosha
is not an archive: past its retention age a message is deleted, and a range
reaching further would come back short with nothing saying so.
`srosha.Everything` is the zero value and means as far back as the deployment
keeps, which is the only answer that is right whatever that age is set to.

## Register your identities, once

```go
c.Credentials.Register(ctx, srosha.Registration{
    Name:    "alerts",
    Default: true,
    Credential: srosha.TelegramCredential{Token: botToken},
})
```

After that, `Submit` names a channel and not an identity. Only a channel with
more than one needs to say which:

```go
srosha.Email("a@b.test").From("marketing")
```

Every channel has its own type, because every channel needs different things:

| | |
| --- | --- |
| `TelegramCredential`, `BaleCredential` | a bot token |
| `FCMCredential` | a Firebase service account file |
| `SMTPCredential` | a host, a port, a user, a from address, a password |
| `MatrixCredential` | a homeserver and an access token |
| `WhatsAppCredential` | a phone number id and a token |
| `APNsCredential` | a signing key, a key id, a team id, a topic |
| `RawCredential` | anything this build has no type for yet |

`Rotate` replaces the secret and keeps the name, which is what a leaked token
needs — registering a second identity instead would make every message still
naming the old one fail. `Update` replaces the settings and keeps the secret;
it sends only the settings half, so a secret set on the credential you hand it
is ignored.

None of them print their secret, through `%v` or through `json.Marshal`.

## Errors

```go
switch {
case errors.Is(err, srosha.ErrRateLimited):
    // wait; the client already backs off unless you turned retries off
case errors.Is(err, srosha.ErrInvalidRequest):
    // fix the request. err.Error() carries srosha's own words
}
```

One sentinel per code srosha answers with, and no finer. `ErrInvalidRequest`
covers an address that is not an address, a missing body and a listing window
past retention, and the only way to tell those apart today would be matching on
the message text — which breaks the day somebody rewords a sentence. **Show the
message; do not match on it.**

The client retries `ErrUnavailable`, `ErrTimeout` and `ErrRateLimited` and
nothing else, three attempts by default. `WithRetry(1)` turns it off.

## Connecting

| | |
| --- | --- |
| `WithInsecure()` | plaintext, for a caller inside srosha's own network |
| `WithTLSConfig(*tls.Config)` | a private CA in staging |
| `WithTimeout(d)` | a deadline when your context carries none |
| `WithRetry(n)` | total attempts; `1` means no retrying |

**TLS is the default.** srosha's gRPC listener runs without it and is reachable
only from inside its own network, so a service that lives there says
`WithInsecure()` out loud. A default that was insecure is what reaches
production by accident.

## Webhooks

`c.Webhooks.Register(ctx, url)` says where srosha should POST outcomes. It must
be https and must not point inside srosha's own network.

Every callback is signed with HMAC-SHA256 over `<timestamp>.<body>`.
**This package does not verify that signature for you.** Until it does, verify
it yourself before trusting a callback — an unverified one is anything anybody
posted to that url. The callback is best effort and is switched off after
enough failures, so it is a convenience: `Get` and `List` are the reliable path.

## Versions

`v0.x` while the API settles. The Go directive is 1.23 — deliberately below the
service's own, because an SDK should not make you upgrade your toolchain.

The design, and what was decided against, is in
[the spec](../../docs/superpowers/specs/2026-08-27-go-sdk-design.md).
