# notifyd Go SDK

Official Go client for the [notifyd](https://notifyd.fluxintek.com) notification delivery API.

```go
import "github.com/mahrous-amer/notifyd/sdks/go"
```

## Install

```
go get github.com/mahrous-amer/notifyd/sdks/go
```

## Usage

```go
client, err := notifyd.New(notifyd.Config{
	APIKey:    os.Getenv("NOTIFYD_API_KEY"),
	APISecret: os.Getenv("NOTIFYD_API_SECRET"),
})
if err != nil {
	log.Fatal(err)
}

notification, err := client.Send(ctx, notifyd.SendInput{
	ChannelConfigID: "your-channel-config-id",
	Body:            "Deploy finished.",
})
```

`Config.BaseURL` defaults to `https://notifyd.fluxintek.com/api`. The client exchanges
`APIKey`/`APISecret` for a JWT on first use, caches it until shortly before expiry, and
retries exactly once with a forced refresh if a request comes back `401`.

## Verifying webhook signatures

```go
err := notifyd.VerifyWebhookSignature(
	endpointSecret,
	r.Header.Get("X-Notifyd-Timestamp"),
	rawBody, // must be the exact bytes received, read before any JSON parsing
	r.Header.Get("X-Notifyd-Signature"),
)
if err != nil {
	http.Error(w, "invalid signature", http.StatusUnauthorized)
	return
}
```

This covers both the `webhook` channel type (notification content) and status-webhook
deliveries (`notification.delivered` / `notification.failed` events) — they share the
same signing scheme.

## Development

This is a separate Go module (`sdks/go/go.mod`) so its dependencies never affect the
main notifyd service module. From `sdks/go`:

```
go build ./...
go test ./...
```

Signature tests read `../testdata/signature_vectors.json`, a fixture shared with the
TypeScript and Python SDKs — regenerate it via `../testdata/regen_vectors.sh` after any
change to `internal/provider.SignHMAC`.
