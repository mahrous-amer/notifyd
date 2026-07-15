# notifyd-sdk

Official Python client for the [notifyd](https://notifyd.fluxintek.com) notification
delivery API.

## Install

```
pip install notifyd-sdk
```

Requires Python 3.9+.

## Usage

```python
import os
from notifyd_sdk import NotifydClient

client = NotifydClient(
    api_key=os.environ["NOTIFYD_API_KEY"],
    api_secret=os.environ["NOTIFYD_API_SECRET"],
)

notification = client.send(channel_config_id="your-channel-config-id", body="Deploy finished.")
```

`base_url` defaults to `https://notifyd.fluxintek.com/api`. The client exchanges
`api_key`/`api_secret` for a JWT on first use, caches it until shortly before expiry, and
retries exactly once with a forced refresh if a request comes back `401`.

## Verifying webhook signatures

```python
from notifyd_sdk import verify_webhook_signature

is_valid = verify_webhook_signature(
    endpoint_secret,
    request.headers["X-Notifyd-Timestamp"],
    raw_body,  # must be the exact bytes received, read before any JSON parsing
    request.headers["X-Notifyd-Signature"],
)
```

This covers both the `webhook` channel type (notification content) and status-webhook
deliveries (`notification.delivered` / `notification.failed` events) — they share the
same signing scheme.

## Development

```
python3 -m venv .venv
source .venv/bin/activate
pip install -e ".[dev]"
pytest
```

Signature tests read `../testdata/signature_vectors.json`, a fixture shared with the Go
and TypeScript SDKs — regenerate it via `../testdata/regen_vectors.sh` after any change
to `internal/provider.SignHMAC`.
