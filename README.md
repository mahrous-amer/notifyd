# notifyd

notifyd is a multi-tenant notification delivery service written in Go. It receives notification requests via a REST API, routes them to third-party messaging channels (Discord, Telegram, WhatsApp, email), and guarantees delivery through a Redis-backed queue with automatic retries and exponential backoff.

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [API Reference](#api-reference)
  - [Authentication](#authentication)
  - [Channels](#channels)
  - [Notifications](#notifications)
  - [Admin: Tenants](#admin-tenants)
- [End-to-End Walkthrough](#end-to-end-walkthrough)
- [Channel Configuration](#channel-configuration)
- [Telegram Admin Bot](#telegram-admin-bot)
- [Retry Strategy](#retry-strategy)
- [Monitoring](#monitoring)
- [Development](#development)
- [Makefile Targets](#makefile-targets)
- [Deployment](#deployment)
- [Project Structure](#project-structure)
- [License](#license)

---

## Features

- **Multi-tenant** — each tenant has its own API key, API secret, and isolated channel configurations.
- **JWT authentication** — tenants exchange their API key and secret for a short-lived JWT. All protected endpoints require a valid bearer token.
- **Admin authentication** — separate admin API key/secret for tenant management endpoints.
- **Four delivery channels** — Discord (webhook), Telegram (Bot API), WhatsApp (Meta Cloud API), and email (bring-your-own SMTP).
- **Guaranteed delivery** — notifications are enqueued in Redis via [Asynq](https://github.com/hibiken/asynq). The worker processes them asynchronously, independent of the API server.
- **Exponential backoff retries** — failed deliveries are retried automatically up to a configurable maximum. Permanently failed tasks move to Asynq's dead letter queue.
- **Delivery attempt tracking** — every attempt (success or failure) is recorded in PostgreSQL with timing, HTTP response data, and error messages.
- **Notification status lifecycle** — `pending` → `processing` → `delivered` / `retrying` → `failed`.
- **PostgreSQL storage** — tenants, channel configs, notifications, and delivery attempts all persist in PostgreSQL with proper foreign-key constraints and indexes.
- **Docker-based deployment** — multi-stage Dockerfile with separate targets for API, worker, and admin bot. Non-root containers, stripped binaries, auto-migration on startup.
- **Secrets never leave the database** — the worker fetches channel credentials from PostgreSQL at dispatch time, so secrets are not stored in Redis task payloads.
- **Telegram admin bot** — conversational interface for tenant management and notification monitoring.

---

## Architecture

```
                          ┌──────────────────────────────────┐
                          │          REST clients             │
                          └──────────────┬───────────────────┘
                                         │ HTTPS (Cloudflare Tunnel)
                          ┌──────────────▼───────────────────┐
                          │            Caddy                  │
                          │     (reverse proxy + TLS)         │
                          └──────────────┬───────────────────┘
                                         │ HTTP
                          ┌──────────────▼───────────────────┐
                          │          API Server               │
                          │        (cmd/api, :8080)           │
                          │                                   │
                          │  Auth / Channel / Notification /  │
                          │         Admin handlers            │
                          └──────────────┬───────────────────┘
                                         │
               ┌─────────────────────────┼─────────────────────────┐
               │                         │                         │
    ┌──────────▼──────────┐  ┌──────────▼──────────┐  ┌──────────▼──────────┐
    │      PostgreSQL      │  │        Redis         │  │   Telegram Admin    │
    │  tenants             │  │   Asynq task queue   │  │   Bot (cmd/admin-   │
    │  channel_configs     │  │                      │  │   bot)              │
    │  notifications       │  └──────────┬───────────┘  └─────────────────────┘
    │  delivery_attempts   │             │
    └──────────┬───────────┘  ┌──────────▼───────────┐
               │              │     Worker Server      │
               │              │    (cmd/worker)        │
               │              │                        │
               │              │  Dequeues tasks        │
               └──────────────►  Fetches channel creds │
                              │  Calls provider API    │
                              │  Records attempt       │
                              └────────────────────────┘
```

### Components

| Component | Purpose |
|---|---|
| `cmd/api` | HTTP server — handles tenant management, channel config CRUD, and notification submission |
| `cmd/worker` | Asynq worker — dequeues notification tasks and delivers them to channels |
| `cmd/admin-bot` | Telegram bot for admin operations (tenant CRUD, notification status, stats) |
| PostgreSQL | Persistent store for all domain data |
| Redis | Asynq task queue backend |

### Request flow

1. A tenant POSTs to `/notifications/send` with a channel config ID and message body.
2. The API server creates a `Notification` record (status: `pending`) and enqueues an Asynq task.
3. The worker picks up the task, fetches the channel credentials from PostgreSQL, and calls the provider API.
4. On success the notification is marked `delivered`. On failure it is marked `retrying` and Asynq re-schedules it with exponential backoff.
5. After exhausting all retries the error handler marks the notification `failed` and Asynq moves the task to the dead letter queue.

---

## Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- [Go 1.25+](https://go.dev/dl/) (for local development)
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI (for local migrations)

### Option A: Full local stack with Docker

```bash
# Start everything (postgres, redis, api, worker)
make docker-up

# Verify
curl http://localhost:8080/health
```

### Option B: Local development (infra in Docker, services native)

```bash
# Start postgres + redis
make infra-up

# Run migrations
make migrate-up

# Start API and worker in parallel
make dev
```

### Option C: One-command dev start

```bash
# Starts infra, runs migrations, launches API server
make dev-api
```

### Seed test data

```bash
make seed
```

This creates a bootstrap tenant:
- **API Key:** `test-api-key-123`
- **API Secret:** `test-secret-12345`

### Verify

```bash
curl http://localhost:8080/health
# {"status":"ok","checks":{"postgres":"ok","redis":"ok"}}
```

---

## Configuration

Configuration is read from environment variables. Copy `.env.example` to `.env` and adjust values.

```bash
cp .env.example .env
```

| Variable | Default | Required | Description |
|---|---|---|---|
| `API_PORT` | `8080` | No | Port the API server listens on |
| `SHUTDOWN_TIMEOUT` | `15s` | No | Graceful shutdown timeout |
| `DATABASE_URL` | — | **Yes** | PostgreSQL connection string |
| `DB_MAX_CONNS` | `25` | No | Maximum connections in the pgxpool |
| `DB_MIN_CONNS` | `5` | No | Minimum idle connections |
| `DB_MAX_CONN_LIFETIME` | `30m` | No | Maximum lifetime of a connection |
| `DB_MAX_CONN_IDLE_TIME` | `5m` | No | Maximum idle time before closing |
| `DB_HEALTH_CHECK_PERIOD` | `30s` | No | Health check interval for idle connections |
| `REDIS_ADDR` | `127.0.0.1:6379` | No | Redis address |
| `REDIS_PASSWORD` | `""` | No | Redis password |
| `REDIS_DB` | `0` | No | Redis database index |
| `JWT_SIGNING_KEY` | — | **Yes** | HMAC-SHA256 signing key (use a random 256-bit value) |
| `JWT_EXPIRATION` | `1h` | No | JWT token lifetime |
| `JWT_ISSUER` | `notifyd` | No | JWT issuer claim |
| `WORKER_CONCURRENCY` | `10` | No | Number of concurrent worker goroutines |
| `MAX_RETRIES` | `5` | No | Maximum delivery attempts per notification |
| `MIN_RETRY_DELAY` | `15s` | No | Minimum delay before first retry |
| `MAX_RETRY_DELAY` | `30m` | No | Maximum delay cap for exponential backoff |
| `LOG_LEVEL` | `info` | No | Log level (`debug`, `info`, `warn`, `error`) |
| `ADMIN_API_KEY` | — | No | Admin API key for tenant management |
| `ADMIN_API_SECRET` | — | No | Admin API secret for tenant management |
| `TELEGRAM_BOT_TOKEN` | — | No | Telegram bot token (for admin bot) |
| `TELEGRAM_ADMIN_CHAT` | — | No | Telegram chat ID authorized for admin bot |

> `JWT_SIGNING_KEY` and `DATABASE_URL` are required. The process will refuse to start without them.

---

## API Reference

**Full API documentation:** [`docs/API.md`](docs/API.md)

### Base URL

```
https://notifyd.fluxintek.com
```

### Response format

All responses use `Content-Type: application/json`.

Success:
```json
{ "...resource fields..." }
```

Error:
```json
{ "error": "human-readable message" }
```

Paginated list:
```json
{
  "data": [...],
  "total": 42,
  "limit": 20,
  "offset": 0
}
```

### Authentication

All endpoints except `GET /health` and `POST /auth/token` require a bearer token:

```
Authorization: Bearer <token>
```

---

### POST /auth/token

Exchange an API key and secret for a JWT.

**Tenant authentication:**

```bash
curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"test-api-key-123","api_secret":"test-secret-12345"}'
```

**Admin authentication:**

```bash
curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"<ADMIN_API_KEY>","api_secret":"<ADMIN_API_SECRET>"}'
```

**Response 200:**

```json
{
  "token": "eyJhbGci...",
  "expires_in": "1h0m0s"
}
```

| Status | Reason |
|---|---|
| 400 | Missing or malformed fields |
| 401 | Unknown API key or incorrect secret |
| 403 | Tenant is disabled |

---

### Channels

Channel configs store the credentials and settings for a specific messaging destination.

#### GET /channels

List all channel configs for the authenticated tenant.

```bash
curl -s https://notifyd.fluxintek.com/channels \
  -H "Authorization: Bearer $TOKEN"
```

#### POST /channels

Create a new channel config.

```bash
# Telegram
curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "telegram",
    "name": "ops-alerts",
    "config": {
      "bot_token": "123456:ABC-DEF...",
      "chat_id": "-1001234567890"
    }
  }'

# Discord
curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "discord",
    "name": "dev-notifications",
    "config": {
      "webhook_url": "https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN"
    }
  }'

# WhatsApp
curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "whatsapp",
    "name": "customer-alerts",
    "config": {
      "phone_number_id": "1234567890",
      "access_token": "EAAxxxxxx...",
      "recipient": "15551234567"
    }
  }'

# Email (BYO-SMTP)
curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "email",
    "name": "ops-email",
    "config": {
      "host": "smtp.example.com",
      "port": 587,
      "username": "alerts@example.com",
      "password": "app-password",
      "from": "alerts@example.com",
      "to": ["ops@example.com"]
    }
  }'
```

Optional delivery preferences:

```json
{
  "delivery_prefs": {
    "priority": "critical",
    "max_retries": 10,
    "format_mode": "markdown"
  }
}
```

| Field | Values |
|-------|--------|
| `priority` | `critical`, `normal`, `low` |
| `format_mode` | `plain`, `markdown`, `html` |
| `max_retries` | non-negative integer |

#### GET /channels/{id}

```bash
curl -s https://notifyd.fluxintek.com/channels/CHANNEL_ID \
  -H "Authorization: Bearer $TOKEN"
```

#### PATCH /channels/{id}

Update a channel config (all fields optional):

```bash
curl -s -X PATCH https://notifyd.fluxintek.com/channels/CHANNEL_ID \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"new-name","is_active":false}'
```

#### DELETE /channels/{id}

```bash
curl -s -X DELETE https://notifyd.fluxintek.com/channels/CHANNEL_ID \
  -H "Authorization: Bearer $TOKEN"
```

---

### Notifications

#### POST /notifications/send

Send a notification to a single channel.

```bash
curl -s -X POST https://notifyd.fluxintek.com/notifications/send \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel_config_id": "a8fd4ddd-c8ba-4b25-b1e2-eacaa8f4db9b",
    "subject": "Deploy Complete",
    "body": "v1.0 deployed successfully to production"
  }'
```

| Field | Required | Description |
|-------|----------|-------------|
| `channel_config_id` | yes | UUID of the channel config |
| `body` | yes | Notification body text |
| `subject` | no | Subject/title line |
| `metadata` | no | Arbitrary JSON metadata |

**Response 202:**

```json
{
  "id": "f01d1800-0a86-428a-9a1c-82968893f204",
  "channel": "telegram",
  "status": "pending",
  "retry_count": 0,
  "max_retries": 5,
  "created_at": "2026-03-02T01:10:13Z"
}
```

#### POST /notifications/send-multi

Send to multiple channels in one request (1–50 items):

```bash
curl -s -X POST https://notifyd.fluxintek.com/notifications/send-multi \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channels": [
      {
        "channel_config_id": "uuid-telegram",
        "subject": "Alert",
        "body": "Server CPU at 95%"
      },
      {
        "channel_config_id": "uuid-discord",
        "body": "Server CPU at 95%"
      }
    ]
  }'
```

**Response 202:** Partial success is possible.

```json
{
  "sent": [ { "...notification..." }, { "...notification..." } ],
  "errors": []
}
```

#### GET /notifications

List notifications with optional filtering:

```bash
# All notifications
curl -s "https://notifyd.fluxintek.com/notifications" \
  -H "Authorization: Bearer $TOKEN"

# Filter by status
curl -s "https://notifyd.fluxintek.com/notifications?status=delivered&limit=10" \
  -H "Authorization: Bearer $TOKEN"

# Filter by channel
curl -s "https://notifyd.fluxintek.com/notifications?channel=telegram&offset=20" \
  -H "Authorization: Bearer $TOKEN"

# Combined
curl -s "https://notifyd.fluxintek.com/notifications?status=failed&channel=discord&limit=50" \
  -H "Authorization: Bearer $TOKEN"
```

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 20 | Page size (1–100) |
| `offset` | int | 0 | Pagination offset |
| `status` | string | — | `pending`, `processing`, `delivered`, `retrying`, `failed` |
| `channel` | string | — | `telegram`, `discord`, `whatsapp`, `email` |

#### GET /notifications/{id}

```bash
curl -s https://notifyd.fluxintek.com/notifications/NOTIFICATION_ID \
  -H "Authorization: Bearer $TOKEN"
```

#### GET /notifications/{id}/attempts

View delivery attempt history:

```bash
curl -s https://notifyd.fluxintek.com/notifications/NOTIFICATION_ID/attempts \
  -H "Authorization: Bearer $TOKEN"
```

```json
[
  {
    "attempt_number": 1,
    "status": "success",
    "duration_ms": 666,
    "attempted_at": "2026-03-02T01:10:13Z"
  }
]
```

#### GET /notifications/{id}/metrics

```bash
curl -s https://notifyd.fluxintek.com/notifications/NOTIFICATION_ID/metrics \
  -H "Authorization: Bearer $TOKEN"
```

---

### Admin: Tenants

Admin endpoints require an admin JWT (obtained with `ADMIN_API_KEY` / `ADMIN_API_SECRET`).

```bash
ADMIN_TOKEN=$(curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"<ADMIN_API_KEY>","api_secret":"<ADMIN_API_SECRET>"}' \
  | jq -r .token)
```

#### POST /admin/tenants

```bash
curl -s -X POST https://notifyd.fluxintek.com/admin/tenants \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name":"Acme Corp","slug":"acme"}'
```

The response includes `api_key` and `api_secret` — store the secret securely, it cannot be retrieved again.

#### GET /admin/tenants

```bash
curl -s "https://notifyd.fluxintek.com/admin/tenants?limit=20&offset=0" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

#### GET /admin/tenants/{id}

```bash
curl -s https://notifyd.fluxintek.com/admin/tenants/TENANT_ID \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

#### PATCH /admin/tenants/{id}

```bash
curl -s -X PATCH https://notifyd.fluxintek.com/admin/tenants/TENANT_ID \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name":"Acme Corporation","is_active":false}'
```

#### DELETE /admin/tenants/{id}

```bash
curl -s -X DELETE https://notifyd.fluxintek.com/admin/tenants/TENANT_ID \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

---

## End-to-End Walkthrough

Complete workflow from tenant creation to notification delivery:

```bash
# 1. Authenticate as admin
ADMIN_TOKEN=$(curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"<ADMIN_API_KEY>","api_secret":"<ADMIN_API_SECRET>"}' \
  | jq -r .token)

# 2. Create a tenant
curl -s -X POST https://notifyd.fluxintek.com/admin/tenants \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"name":"Acme Corp","slug":"acme"}'
# → Save api_key and api_secret from response

# 3. Authenticate as the tenant
TOKEN=$(curl -s -X POST https://notifyd.fluxintek.com/auth/token \
  -H "Content-Type: application/json" \
  -d '{"api_key":"<tenant_api_key>","api_secret":"<tenant_api_secret>"}' \
  | jq -r .token)

# 4. Create a Telegram channel config
CHANNEL_ID=$(curl -s -X POST https://notifyd.fluxintek.com/channels \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "channel": "telegram",
    "name": "ops-alerts",
    "config": {
      "bot_token": "123456:ABC-DEF...",
      "chat_id": "-1001234567890"
    }
  }' | jq -r .id)

# 5. Send a notification
NOTIF_ID=$(curl -s -X POST https://notifyd.fluxintek.com/notifications/send \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "{
    \"channel_config_id\": \"$CHANNEL_ID\",
    \"subject\": \"Deploy Complete\",
    \"body\": \"Production deploy v1.4.2 completed successfully.\"
  }" | jq -r .id)

# 6. Check delivery status (wait a moment for async processing)
sleep 2
curl -s https://notifyd.fluxintek.com/notifications/$NOTIF_ID \
  -H "Authorization: Bearer $TOKEN" | jq .status
# → "delivered"

# 7. View delivery attempts
curl -s https://notifyd.fluxintek.com/notifications/$NOTIF_ID/attempts \
  -H "Authorization: Bearer $TOKEN" | jq .

# 8. Send to multiple channels at once
curl -s -X POST https://notifyd.fluxintek.com/notifications/send-multi \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "{
    \"channels\": [
      {\"channel_config_id\": \"$CHANNEL_ID\", \"subject\": \"Alert\", \"body\": \"CPU at 95%\"},
      {\"channel_config_id\": \"$CHANNEL_ID\", \"body\": \"Disk usage critical\"}
    ]
  }"

# 9. List all delivered notifications
curl -s "https://notifyd.fluxintek.com/notifications?status=delivered" \
  -H "Authorization: Bearer $TOKEN" | jq .

# 10. List failed notifications
curl -s "https://notifyd.fluxintek.com/notifications?status=failed" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Channel Configuration

The `config` field in a channel config is a JSON object validated against the channel type.

### Discord

```json
{
  "webhook_url": "https://discord.com/api/webhooks/<webhook_id>/<webhook_token>"
}
```

| Field | Required | Description |
|---|---|---|
| `webhook_url` | Yes | Full Discord webhook URL |

Messages are sent as Discord webhook payloads. If `subject` is provided it is prepended in bold.

### Telegram

```json
{
  "bot_token": "123456:ABC-DEF...",
  "chat_id": "-1001234567890"
}
```

| Field | Required | Description |
|---|---|---|
| `bot_token` | Yes | Bot API token from @BotFather |
| `chat_id` | Yes | Target chat, channel, or group ID |

Messages are sent via `sendMessage` with `parse_mode: Markdown`.

### WhatsApp

```json
{
  "phone_number_id": "1234567890",
  "access_token": "EAAxxxxxx...",
  "recipient": "15551234567"
}
```

| Field | Required | Description |
|---|---|---|
| `phone_number_id` | Yes | WhatsApp Business phone number ID from Meta |
| `access_token` | Yes | Meta access token with `whatsapp_business_messaging` permission |
| `recipient` | Yes | Recipient phone number in E.164 format (digits only, no `+`) |

### Email

Bring-your-own SMTP: mail is sent from the customer's own domain using their own SMTP credentials, so there's no shared-IP deliverability or DKIM/SPF setup to manage.

```json
{
  "host": "smtp.example.com",
  "port": 587,
  "username": "alerts@example.com",
  "password": "app-password",
  "from": "alerts@example.com",
  "to": ["ops@example.com"],
  "cc": ["escalations@example.com"],
  "reply_to": "noreply@example.com"
}
```

| Field | Required | Description |
|---|---|---|
| `host` | Yes | SMTP server hostname |
| `port` | Yes | SMTP port. `465` negotiates implicit TLS; any other port uses STARTTLS when the server advertises it |
| `username` | Yes | SMTP auth username |
| `password` | Yes | SMTP auth password |
| `from` | Yes | Envelope and header `From` address |
| `to` | Yes | Fixed recipient list — every send through this channel goes to all of these addresses |
| `cc` | No | Additional CC recipients |
| `reply_to` | No | Optional `Reply-To` address |

`subject` is required for email sends (empty or blank returns `400`). `format_mode` controls the MIME body: `plain` sends `text/plain`, `html` sends `text/html`, and `markdown` renders to `text/html` with the original markdown kept as a `text/plain` alternative part.

---

## Telegram Admin Bot

notifyd includes a Telegram bot (`cmd/admin-bot`) for conversational admin operations. It connects directly to the service layer (not the HTTP API), so it works even if the API server is down.

### Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and save the token.
2. Get your Telegram chat ID (send `/start` to [@userinfobot](https://t.me/userinfobot)).
3. Set the environment variables:
   ```bash
   TELEGRAM_BOT_TOKEN="123456:ABC-DEF..."
   TELEGRAM_ADMIN_CHAT="your-chat-id"
   ```
4. Start the bot:
   ```bash
   make admin-bot
   ```

The bot only responds to messages from the configured admin chat ID.

### Commands

| Command | Description |
|---------|-------------|
| `/start`, `/help` | Show the command list |
| `/tenants` | List all tenants (name, slug, active status) |
| `/tenant <id>` | Show tenant details |
| `/create_tenant <name> <slug>` | Create a new tenant (returns api_key + api_secret) |
| `/toggle_tenant <id>` | Toggle a tenant's active status |
| `/delete_tenant <id>` | Delete a tenant (with confirmation) |
| `/notifications <tenant_id>` | List recent notifications for a tenant |
| `/notification <id>` | Show notification details |
| `/stats` | Show total tenants and notification counts by status |

---

## Retry Strategy

Asynq manages retry scheduling using exponential backoff:

- **Max retries** — controlled by `MAX_RETRIES` (default: 5).
- **Backoff** — exponential, bounded by `MAX_RETRY_DELAY` (default: 30m). First retry waits `MIN_RETRY_DELAY` (default: 15s).
- **Per-task timeout** — each delivery attempt has a 30-second deadline.

### Status transitions

| Event | Notification status |
|---|---|
| Task enqueued | `pending` |
| Task dequeued | `processing` |
| Provider call succeeds | `delivered` |
| Provider call fails, retries remain | `retrying` |
| Retries exhausted | `failed` |

### Dead letter queue

When a task exceeds max retries, Asynq moves it to the dead letter queue. The `NotifyErrorHandler` updates the notification to `failed` with the final error message.

### Security note on secrets

Channel credentials are not included in Redis task payloads. The worker fetches them from PostgreSQL at execution time using the `channel_config_id`. Compromising Redis does not expose messaging credentials.

---

## Monitoring

### Health endpoint

```
GET /health
```

Returns HTTP 200 with `{"status":"ok","checks":{"postgres":"ok","redis":"ok"}}` when healthy. Returns HTTP 503 with `"status":"degraded"` if any dependency is down.

### Logs

Both API and worker use [zerolog](https://github.com/rs/zerolog) for structured JSON logging. Set `LOG_LEVEL=debug` for verbose output.

---

## Development

### Prerequisites

- Go 1.25+
- PostgreSQL 16
- Redis 7
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI
- [golangci-lint](https://golangci-lint.run/) (for linting)

### Run locally without Docker

```bash
# 1. Start dependencies
make infra-up

# 2. Run migrations
make migrate-up

# 3. Start API + worker
make dev
```

### Running tests

```bash
make test
```

### End-to-end tests

```bash
# Requires running API + worker
make seed   # Create test tenant
make e2e    # Run e2e test suite
```

---

## Makefile Targets

### Build

| Target | Description |
|--------|-------------|
| `make build` | Compile all binaries (`api`, `worker`, `admin-bot`) to `./bin/` |

### Run (local, no Docker)

| Target | Description |
|--------|-------------|
| `make api` | Run the API server with `go run` |
| `make worker` | Run the async worker with `go run` |
| `make admin-bot` | Run the Telegram admin bot with `go run` |

### Migrations

| Target | Description |
|--------|-------------|
| `make migrate-up` | Apply all pending migrations |
| `make migrate-down` | Roll back the last migration |
| `make migrate-create name=add_xyz` | Create a new migration file pair |
| `make migrate-reset` | Drop all tables then re-apply migrations |

### Quality

| Target | Description |
|--------|-------------|
| `make test` | Run unit tests with race detector |
| `make lint` | Run golangci-lint |
| `make vet` | Run go vet |
| `make check` | Run vet + lint + tests |
| `make coverage` | Run tests with coverage report (outputs `coverage.html`) |

### Infrastructure (Postgres + Redis only)

| Target | Description |
|--------|-------------|
| `make infra-up` | Start Postgres and Redis containers |
| `make infra-down` | Stop Postgres and Redis containers |
| `make infra-logs` | Tail Postgres and Redis logs |

### Local dev (infra + run)

| Target | Description |
|--------|-------------|
| `make dev` | Start infra, run migrations, start API + worker in parallel |
| `make dev-api` | Start infra, run migrations, start API only |
| `make dev-worker` | Start infra, start worker only |

### Docker Compose (full stack)

| Target | Description |
|--------|-------------|
| `make docker-build` | Build all Docker images |
| `make docker-up` | Build and deploy full stack |
| `make docker-down` | Tear down all containers |
| `make docker-logs` | Tail all Docker logs |

### Production deployment

| Target | Description |
|--------|-------------|
| `make prod-up` | Deploy production stack (`docker-compose.prod.yml`) |
| `make prod-down` | Tear down production stack |
| `make prod-logs` | Tail production logs |

### Shared infrastructure deployment

| Target | Description |
|--------|-------------|
| `make deploy-db-init` | Create notifyd user and database on shared Postgres |
| `make deploy` | Deploy to shared infrastructure (`docker-compose.deploy.yml`) |
| `make deploy-down` | Tear down deployed services |
| `make deploy-logs` | Tail deployed service logs |

### Testing & Seeding

| Target | Description |
|--------|-------------|
| `make seed` | Insert a bootstrap tenant for testing |
| `make e2e` | Run end-to-end test suite (requires running API) |

### Load Testing

| Target | Description |
|--------|-------------|
| `make load-test` | Run k6 full scenario load test |
| `make load-test-auth` | Load test auth endpoint |
| `make load-test-send` | Load test notification sending |
| `make load-test-query` | Load test query endpoints |

### Cleanup

| Target | Description |
|--------|-------------|
| `make clean` | Remove build artifacts and stop containers |

---

## Deployment

### Docker images

The multi-stage Dockerfile produces three separate images:

```bash
docker build --target api -t notifyd-api .
docker build --target worker -t notifyd-worker .
docker build --target admin-bot -t notifyd-admin-bot .
```

All images:
- Run as non-root user (`appuser`, UID 10001)
- Use stripped binaries (`-ldflags="-s -w"`)
- Based on `alpine:3.20`
- API image includes `golang-migrate` and runs migrations automatically on startup

### Shared infrastructure

For deployment alongside existing Postgres/Redis (e.g., in a shared infrastructure repo):

```bash
# First time: create database
make deploy-db-init

# Deploy
make deploy

# With admin bot
docker compose -f docker-compose.deploy.yml --profile admin-bot up -d
```

### CI/CD

GitHub Actions workflow (`.github/workflows/ci.yml`) runs:
1. **lint** — `go vet` + `golangci-lint`
2. **test** — `go test -race` with Postgres/Redis service containers
3. **build** — Docker images for all 3 targets, pushed to GHCR on master

---

## Project Structure

```
notifyd/
├── cmd/
│   ├── api/              # API server entry point
│   ├── worker/           # Worker server entry point
│   └── admin-bot/        # Telegram admin bot entry point
├── internal/
│   ├── auth/             # JWT manager, middleware, claims
│   ├── bot/              # Telegram admin bot (commands, formatting)
│   ├── config/           # Environment-based configuration
│   ├── domain/           # Core types, interfaces, errors, status constants
│   ├── handler/          # HTTP handlers (auth, channel, notification, tenant, health)
│   ├── provider/         # Channel providers (Discord, Telegram, WhatsApp, Email)
│   ├── repository/       # PostgreSQL repository implementations
│   ├── router/           # Chi router wiring
│   ├── service/          # Business logic (tenant, channel, notification)
│   └── worker/           # Asynq task definitions, dispatcher, error handler
├── migrations/           # SQL migration files (golang-migrate)
├── scripts/              # Helper scripts (e2e tests, bcrypt tool)
├── loadtest/             # k6 load test scenarios
├── docs/                 # API documentation
├── pkg/
│   └── response/         # Shared HTTP response helpers
├── docker-compose.yml          # Development stack
├── docker-compose.prod.yml     # Standalone production stack
├── docker-compose.deploy.yml   # Shared infrastructure deployment
├── Dockerfile                  # Multi-stage (api, worker, admin-bot)
├── entrypoint.sh               # Auto-migration entrypoint for API
├── go.mod
├── Makefile
└── .github/workflows/ci.yml   # CI/CD pipeline
```

---

## License

MIT License. See [LICENSE](LICENSE) for the full text.
