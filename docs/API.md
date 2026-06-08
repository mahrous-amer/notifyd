# Notifyd API Documentation

**Base URL:** `https://notifyd.fluxintek.com`

---

## Authentication

All endpoints except `/health` and `/auth/token` require a JWT bearer token:

```
Authorization: Bearer <token>
```

### Issue Token

```
POST /auth/token
```

**Tenant authentication:**

```json
{
  "api_key": "your-api-key",
  "api_secret": "your-api-secret"
}
```

**Admin authentication:**

```json
{
  "api_key": "<ADMIN_API_KEY>",
  "api_secret": "<ADMIN_API_SECRET>"
}
```

**Response** `200 OK`:

```json
{
  "token": "eyJ...",
  "expires_in": "1h0m0s"
}
```

| Error | Status | Cause |
|-------|--------|-------|
| `api_key and api_secret required` | 400 | Missing fields |
| `invalid credentials` | 401 | Wrong key/secret |
| `tenant is disabled` | 403 | Tenant `is_active = false` |

---

## Health

### Check Health

```
GET /health
```

**Response** `200 OK` / `503 Service Unavailable`:

```json
{
  "status": "ok",
  "checks": {
    "postgres": "ok",
    "redis": "ok"
  }
}
```

---

## Channels

Channels define where notifications are delivered (Telegram chat, Discord webhook, etc.). Each channel config belongs to a tenant.

### List Channels

```
GET /channels
```

**Response** `200 OK`:

```json
[
  {
    "id": "a8fd4ddd-c8ba-4b25-b1e2-eacaa8f4db9b",
    "tenant_id": "00000000-0000-0000-0000-000000000001",
    "channel": "telegram",
    "name": "admin-alerts",
    "config": {
      "bot_token": "8669384730:AAH...",
      "chat_id": "1747284053"
    },
    "is_active": true,
    "delivery_prefs": null,
    "created_at": "2026-03-02T00:28:57.672809Z",
    "updated_at": "2026-03-02T00:28:57.672809Z"
  }
]
```

### Create Channel

```
POST /channels
```

**Request:**

```json
{
  "channel": "telegram",
  "name": "my-alerts",
  "config": {
    "bot_token": "YOUR_BOT_TOKEN",
    "chat_id": "YOUR_CHAT_ID"
  },
  "delivery_prefs": {
    "priority": "normal",
    "max_retries": 5,
    "format_mode": "plain"
  }
}
```

**Supported channels and their config:**

| Channel | Config Fields |
|---------|--------------|
| `telegram` | `bot_token`, `chat_id` |
| `discord` | `webhook_url` |
| `whatsapp` | `api_url`, `api_token`, `phone_number` |

**Delivery preferences** (all optional):

| Field | Values | Default |
|-------|--------|---------|
| `priority` | `critical`, `normal`, `low` | — |
| `max_retries` | non-negative integer | — |
| `format_mode` | `plain`, `markdown`, `html` | — |

**Response** `201 Created`: Channel config object.

### Get Channel

```
GET /channels/{channelID}
```

**Response** `200 OK`: Channel config object.

### Update Channel

```
PATCH /channels/{channelID}
```

**Request** (all fields optional):

```json
{
  "name": "new-name",
  "config": { "chat_id": "9999" },
  "is_active": false,
  "delivery_prefs": { "priority": "critical" }
}
```

**Response** `200 OK`: Updated channel config object.

### Delete Channel

```
DELETE /channels/{channelID}
```

**Response** `204 No Content`.

---

## Notifications

### Send Notification

```
POST /notifications/send
```

**Request:**

```json
{
  "channel_config_id": "a8fd4ddd-c8ba-4b25-b1e2-eacaa8f4db9b",
  "subject": "Alert Title",
  "body": "Your notification message here",
  "metadata": { "key": "value" }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `channel_config_id` | yes | UUID of the channel config to deliver through |
| `body` | yes | Notification body text |
| `subject` | no | Notification subject/title |
| `metadata` | no | Arbitrary JSON metadata |

**Response** `202 Accepted`:

```json
{
  "id": "f01d1800-0a86-428a-9a1c-82968893f204",
  "tenant_id": "00000000-0000-0000-0000-000000000001",
  "channel_config_id": "a8fd4ddd-c8ba-4b25-b1e2-eacaa8f4db9b",
  "channel": "telegram",
  "subject": "Alert Title",
  "body": "Your notification message here",
  "status": "pending",
  "retry_count": 0,
  "max_retries": 5,
  "created_at": "2026-03-02T01:10:13.274176339Z",
  "updated_at": "2026-03-02T01:10:13.274176339Z"
}
```

### Send Multiple Notifications

```
POST /notifications/send-multi
```

Send to multiple channels in one request (1–50 items).

**Request:**

```json
{
  "channels": [
    {
      "channel_config_id": "uuid-1",
      "subject": "Alert",
      "body": "Message for channel 1"
    },
    {
      "channel_config_id": "uuid-2",
      "body": "Message for channel 2"
    }
  ]
}
```

**Response** `202 Accepted`:

```json
{
  "sent": [ /* notification objects */ ],
  "errors": [ "channel config not found" ]
}
```

Partial success is possible — some notifications may send even if others fail.

### List Notifications

```
GET /notifications?limit=20&offset=0&status=delivered&channel=telegram
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 20 | 1–100 |
| `offset` | int | 0 | Pagination offset |
| `status` | string | — | `pending`, `processing`, `delivered`, `failed`, `retrying` |
| `channel` | string | — | `telegram`, `discord`, `whatsapp` |

**Response** `200 OK`:

```json
{
  "data": [ /* notification objects */ ],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

### Get Notification

```
GET /notifications/{notificationID}
```

**Response** `200 OK`: Notification object.

### List Delivery Attempts

```
GET /notifications/{notificationID}/attempts
```

**Response** `200 OK`:

```json
[
  {
    "id": "uuid",
    "notification_id": "uuid",
    "attempt_number": 1,
    "status": "success",
    "provider_response": { },
    "error_message": null,
    "duration_ms": 666,
    "attempted_at": "2026-03-02T01:10:13Z"
  }
]
```

### Get Delivery Metrics

```
GET /notifications/{notificationID}/metrics
```

**Response** `200 OK`:

```json
{
  "id": "uuid",
  "notification_id": "uuid",
  "provider_msg_id": "123",
  "delivered_at": "2026-03-02T01:10:13Z",
  "read_at": null,
  "interactions": { },
  "collected_at": "2026-03-02T01:10:13Z"
}
```

---

## Admin Endpoints

Require an admin JWT token (obtained by authenticating with `ADMIN_API_KEY` / `ADMIN_API_SECRET`).

### List Tenants

```
GET /admin/tenants?limit=20&offset=0
```

**Response** `200 OK`:

```json
{
  "data": [
    {
      "id": "uuid",
      "name": "Test Company",
      "slug": "test-co",
      "api_key": "test-api-key-123",
      "is_active": true,
      "created_at": "2026-03-02T00:00:00Z",
      "updated_at": "2026-03-02T00:00:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

### Create Tenant

```
POST /admin/tenants
```

**Request:**

```json
{
  "name": "Acme Corp",
  "slug": "acme"
}
```

**Response** `201 Created`: Tenant object with generated `api_key`.

### Get Tenant

```
GET /admin/tenants/{tenantID}
```

### Update Tenant

```
PATCH /admin/tenants/{tenantID}
```

**Request** (all fields optional):

```json
{
  "name": "New Name",
  "is_active": false
}
```

### Delete Tenant

```
DELETE /admin/tenants/{tenantID}
```

**Response** `204 No Content`.

---

## Notification Statuses

| Status | Description |
|--------|-------------|
| `pending` | Queued, waiting for worker to pick up |
| `processing` | Worker is attempting delivery |
| `delivered` | Successfully delivered to provider |
| `retrying` | Delivery failed, will retry |
| `failed` | All retries exhausted |

---

## Error Responses

All errors follow this format:

```json
{
  "error": "descriptive error message"
}
```

| Status | Common Causes |
|--------|--------------|
| 400 | Invalid request body, missing required fields, invalid UUID, invalid status/channel filter |
| 401 | Missing or invalid JWT token |
| 403 | Tenant disabled, or non-admin accessing admin routes |
| 404 | Resource not found or not owned by tenant |
| 500 | Internal server error |

---

## Examples

### Full workflow: authenticate, create channel, send notification

```bash
# 1. Get token
TOKEN=$(curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"test-api-key-123","api_secret":"test-secret-12345"}' \
  | jq -r .token)

# 2. Create a Telegram channel config
curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "telegram",
    "name": "ops-alerts",
    "config": {
      "bot_token": "YOUR_BOT_TOKEN",
      "chat_id": "YOUR_CHAT_ID"
    }
  }'

# 3. Send a notification (use channel_config_id from step 2)
curl -s -X POST https://notifyd.fluxintek.com/notifications/send \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel_config_id": "CHANNEL_CONFIG_ID_FROM_STEP_2",
    "subject": "Deploy Complete",
    "body": "v1.0 deployed successfully"
  }'

# 4. Check delivery status
curl -s https://notifyd.fluxintek.com/notifications/NOTIFICATION_ID \
  -H "Authorization: Bearer $TOKEN"

# 5. View delivery attempts
curl -s https://notifyd.fluxintek.com/notifications/NOTIFICATION_ID/attempts \
  -H "Authorization: Bearer $TOKEN"
```

### Admin: create a new tenant

```bash
# 1. Get admin token
ADMIN_TOKEN=$(curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"ADMIN_API_KEY","api_secret":"ADMIN_API_SECRET"}' \
  | jq -r .token)

# 2. Create tenant
curl -s -X POST https://notifyd.fluxintek.com/admin/tenants \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name":"Acme Corp","slug":"acme"}'
```

---

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `API_PORT` | HTTP listen port | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | — |
| `REDIS_ADDR` | Redis address | — |
| `REDIS_PASSWORD` | Redis password | — |
| `JWT_SIGNING_KEY` | HMAC-SHA256 signing key | — |
| `JWT_EXPIRATION` | Token lifetime | `1h` |
| `JWT_ISSUER` | Token issuer claim | `notifyd` |
| `ADMIN_API_KEY` | Admin API key | — |
| `ADMIN_API_SECRET` | Admin API secret | — |
| `WORKER_CONCURRENCY` | Async worker threads | `10` |
| `MAX_RETRIES` | Default max delivery retries | `5` |
| `MIN_RETRY_DELAY` | Minimum retry backoff | `15s` |
| `MAX_RETRY_DELAY` | Maximum retry backoff | `30m` |
| `TELEGRAM_BOT_TOKEN` | Admin bot token | — |
| `TELEGRAM_ADMIN_CHAT` | Admin bot chat ID | — |
| `LOG_LEVEL` | Logging level | `info` |
