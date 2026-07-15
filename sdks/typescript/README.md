# @notifyd/sdk

Official TypeScript/Node client for the [notifyd](https://notifyd.fluxintek.com) notification
delivery API.

## Install

```
npm install @notifyd/sdk
```

Requires Node 18+ (uses the native `fetch` global).

## Usage

```ts
import { NotifydClient } from "@notifyd/sdk";

const client = new NotifydClient({
  apiKey: process.env.NOTIFYD_API_KEY!,
  apiSecret: process.env.NOTIFYD_API_SECRET!,
});

const notification = await client.send({
  channelConfigId: "your-channel-config-id",
  body: "Deploy finished.",
});
```

`baseUrl` defaults to `https://notifyd.fluxintek.com/api`. The client exchanges
`apiKey`/`apiSecret` for a JWT on first use, caches it until shortly before expiry, and
retries exactly once with a forced refresh if a request comes back `401`.

## Verifying webhook signatures

```ts
import { verifyWebhookSignature } from "@notifyd/sdk";

const isValid = verifyWebhookSignature(
  endpointSecret,
  request.headers["x-notifyd-timestamp"],
  rawBody, // must be the exact bytes received, read before any JSON parsing
  request.headers["x-notifyd-signature"],
);
```

This covers both the `webhook` channel type (notification content) and status-webhook
deliveries (`notification.delivered` / `notification.failed` events) — they share the
same signing scheme.

## Development

```
npm ci
npm run build
npm test
```

Signature tests read `../testdata/signature_vectors.json`, a fixture shared with the Go
and Python SDKs — regenerate it via `../testdata/regen_vectors.sh` after any change to
`internal/provider.SignHMAC`.
