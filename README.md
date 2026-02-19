# notifyd

notifyd is a multi-tenant notification delivery service written in Go. It receives notification requests via a REST API, routes them to third-party messaging channels (Discord, Telegram, WhatsApp), and guarantees delivery through a Redis-backed queue with automatic retries and exponential backoff.

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
- [Project Structure](#project-structure)
- [License](#license)

---

## Features

- **Multi-tenant** — each tenant has its own API key, API secret, and isolated channel configurations.
- **JWT authentication** — tenants exchange their API key and secret for a short-lived JWT. All protected endpoints require a valid bearer token.
- **Three delivery channels** — Discord (webhook), Telegram (Bot API), and WhatsApp (Meta Cloud API).
- **Guaranteed delivery** — notifications are enqueued in Redis via [Asynq](https://github.com/hibiken/asynq). The worker processes them asynchronously, independent of the API server.
- **Exponential backoff retries** — failed deliveries are retried automatically up to a configurable maximum. Permanently failed tasks move to Asynq's dead letter queue.
- **Delivery attempt tracking** — every attempt (success or failure) is recorded in PostgreSQL with timing, HTTP response data, and error messages.
- **Notification status lifecycle** — `pending` → `processing` → `delivered` / `retrying` → `failed`.
- **PostgreSQL storage** — tenants, channel configs, notifications, and delivery attempts all persist in PostgreSQL with proper foreign-key constraints and indexes.
- **Docker-based deployment** — a single `docker compose up` starts every component.
- **Asynqmon dashboard** — built-in web UI for monitoring queues, retrying dead-letter tasks, and inspecting workers.
- **Secrets never leave the database** — the worker fetches channel credentials from PostgreSQL at dispatch time, so secrets are not stored in Redis task payloads.

---

## Architecture

```
                          ┌──────────────────────────────────┐
                          │          REST clients             │
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
    │      PostgreSQL      │  │        Redis         │  │      Asynqmon        │
    │  tenants             │  │   Asynq task queue   │  │  Queue dashboard    │
    │  channel_configs     │  │                      │  │  (:8081)            │
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
| `cmd/admin-bot` | Optional Telegram bot for admin operations (tenant CRUD, notification status) |
| PostgreSQL | Persistent store for all domain data |
| Redis | Asynq task queue backend |
| Asynqmon | Read-only web UI for inspecting queue state |

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
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI (for running migrations)

### 1. Start all services

```bash
docker compose up -d --build
```

This starts PostgreSQL, Redis, the API server (port 8080), the worker, and Asynqmon (port 8081).

### 2. Run database migrations

```bash
DATABASE_URL="postgres://notifyd:password@localhost:5432/notifyd?sslmode=disable" \
  make migrate-up
```

The API is now reachable at `http://localhost:8080`.

### 3. Verify the health endpoint

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{"status": "ok"}
```

---

## Configuration

Configuration is read from environment variables. The API and worker processes share most variables; a few are server-specific.

| Variable | Default | Required | Description |
|---|---|---|---|
| `API_PORT` | `8080` | No | Port the API server listens on |
| `SHUTDOWN_TIMEOUT` | `15s` | No | Graceful shutdown timeout (applies to both API and worker) |
| `DATABASE_URL` | — | **Yes** | PostgreSQL connection string (e.g. `postgres://user:pass@host:5432/db?sslmode=disable`) |
| `DB_MAX_CONNS` | `25` | No | Maximum connections in the pgxpool |
| `DB_MIN_CONNS` | `5` | No | Minimum idle connections in the pgxpool |
| `DB_MAX_CONN_LIFETIME` | `30m` | No | Maximum lifetime of a connection before recycling |
| `DB_MAX_CONN_IDLE_TIME` | `5m` | No | Maximum time a connection can sit idle before being closed |
| `DB_HEALTH_CHECK_PERIOD` | `30s` | No | How often idle connections are health-checked |
| `REDIS_ADDR` | `127.0.0.1:6379` | No | Redis address |
| `REDIS_PASSWORD` | `""` | No | Redis password |
| `REDIS_DB` | `0` | No | Redis database index |
| `JWT_SIGNING_KEY` | — | **Yes** | HMAC-SHA256 signing key for JWTs. Use a random 256-bit value in production. |
| `JWT_EXPIRATION` | `1h` | No | JWT token lifetime |
| `JWT_ISSUER` | `notifyd` | No | JWT issuer claim |
| `WORKER_CONCURRENCY` | `10` | No | Number of concurrent worker goroutines |
| `MAX_RETRIES` | `5` | No | Maximum delivery attempts per notification |
| `MIN_RETRY_DELAY` | `15s` | No | Minimum delay before the first retry |
| `MAX_RETRY_DELAY` | `30m` | No | Maximum delay cap for exponential backoff |
| `LOG_LEVEL` | `info` | No | Zerolog log level (`debug`, `info`, `warn`, `error`) |

> `JWT_SIGNING_KEY` and `DATABASE_URL` are required. The process will refuse to start without them.

---

## API Reference

### Base URL

```
http://localhost:8080
```

### Response format

All responses use `Content-Type: application/json`.

Success:

```json
{ ...resource fields... }
```

Error:

```json
{
  "error": "human-readable message"
}
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

All endpoints except `GET /health` and `POST /auth/token` require a bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

---

### POST /auth/token

Exchange an API key and secret for a JWT.

**Request**

```json
{
  "api_key": "string",
  "api_secret": "string"
}
```

**Response 200**

```json
{
  "token": "eyJhbGci...",
  "expires_in": "1h0m0s"
}
```

**Error responses**

| Status | Reason |
|---|---|
| 400 | Missing or malformed fields |
| 401 | Unknown API key or incorrect secret |
| 403 | Tenant is disabled |

---

### Channels

Channel configs store the credentials and settings for a specific messaging destination. A tenant can have multiple configs of the same channel type (e.g., multiple Discord webhooks).

Channel config names must be unique per tenant per channel type.

---

#### GET /channels

List all channel configs for the authenticated tenant.

**Response 200**

```json
[
  {
    "id": "uuid",
    "tenant_id": "uuid",
    "channel": "discord",
    "name": "ops-alerts",
    "config": { "webhook_url": "https://discord.com/api/webhooks/..." },
    "is_active": true,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

#### POST /channels

Create a new channel config.

**Request**

```json
{
  "channel": "discord",
  "name": "ops-alerts",
  "config": {
    "webhook_url": "https://discord.com/api/webhooks/123/abc"
  }
}
```

The `config` field is validated against the channel type before the record is saved. See [Channel Configuration](#channel-configuration) for the required fields per channel.

**Response 201** — the created channel config object.

---

#### GET /channels/{id}

Get a single channel config by UUID. Returns 404 if it belongs to a different tenant.

**Response 200** — the channel config object.

---

#### PATCH /channels/{id}

Update a channel config. All fields are optional; only provided fields are changed.

**Request**

```json
{
  "name": "new-name",
  "config": { "webhook_url": "https://discord.com/api/webhooks/456/xyz" },
  "is_active": false
}
```

**Response 200** — the updated channel config object.

---

#### DELETE /channels/{id}

Delete a channel config. Returns 204 on success.

---

### Notifications

---

#### POST /notifications/send

Send a notification to a single channel.

**Request**

```json
{
  "channel_config_id": "uuid",
  "subject": "Optional subject line",
  "body": "Message body (required)",
  "metadata": {}
}
```

The `body` field is required. `subject` and `metadata` are optional. `metadata` is stored as-is and forwarded to the worker but is not sent to the provider.

**Response 202** — the created notification object. Delivery happens asynchronously.

```json
{
  "id": "uuid",
  "tenant_id": "uuid",
  "channel_config_id": "uuid",
  "channel": "discord",
  "subject": "Optional subject line",
  "body": "Message body",
  "status": "pending",
  "retry_count": 0,
  "max_retries": 5,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

#### POST /notifications/send-multi

Send the same logical message to multiple channels in a single request.

**Request**

```json
{
  "channels": [
    {
      "channel_config_id": "uuid-discord",
      "subject": "Alert",
      "body": "Deployment complete"
    },
    {
      "channel_config_id": "uuid-telegram",
      "body": "Deployment complete"
    }
  ]
}
```

**Response 202**

```json
{
  "sent": [
    { ...notification object... },
    { ...notification object... }
  ],
  "errors": []
}
```

If some channels fail validation, the successfully enqueued notifications are returned in `sent` and the failures are listed in `errors`. The HTTP status is always 202 as long as the request is structurally valid.

---

#### GET /notifications

List notifications for the authenticated tenant with optional filtering and pagination.

**Query parameters**

| Parameter | Type | Description |
|---|---|---|
| `status` | string | Filter by status: `pending`, `processing`, `delivered`, `retrying`, `failed` |
| `channel` | string | Filter by channel type: `discord`, `telegram`, `whatsapp` |
| `limit` | integer | Page size (default 20) |
| `offset` | integer | Page offset (default 0) |

**Response 200** — paginated list response.

---

#### GET /notifications/{id}

Get a single notification by UUID. Returns 404 if it belongs to a different tenant.

**Response 200** — the notification object.

---

#### GET /notifications/{id}/attempts

Get the delivery attempt history for a notification.

**Response 200**

```json
[
  {
    "id": "uuid",
    "notification_id": "uuid",
    "attempt_number": 1,
    "status": "failure",
    "error_message": "discord API returned 429: ...",
    "provider_response": { ... },
    "duration_ms": 312,
    "attempted_at": "2024-01-01T00:00:05Z"
  },
  {
    "id": "uuid",
    "notification_id": "uuid",
    "attempt_number": 2,
    "status": "success",
    "provider_response": { ... },
    "duration_ms": 198,
    "attempted_at": "2024-01-01T00:01:30Z"
  }
]
```

---

### Admin: Tenants

Admin endpoints are also JWT-protected. In the current implementation there is no role distinction between a regular tenant token and an admin token — access control at the admin boundary is a deployment concern.

---

#### POST /admin/tenants

Create a new tenant. The API key and secret are generated server-side and returned **only at creation time**. Store the secret securely — it cannot be retrieved again.

**Request**

```json
{
  "name": "Acme Corp",
  "slug": "acme"
}
```

The `slug` must be unique and is used as the JWT `tenant_slug` claim. It should be URL-safe (lowercase alphanumeric and hyphens).

**Response 201**

```json
{
  "tenant": {
    "id": "uuid",
    "name": "Acme Corp",
    "slug": "acme",
    "api_key": "a3f1...",
    "is_active": true,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  },
  "api_key": "a3f1...",
  "api_secret": "9b2c..."
}
```

---

#### GET /admin/tenants

List all tenants with pagination.

**Query parameters:** `limit`, `offset`

**Response 200** — paginated list response.

---

#### GET /admin/tenants/{id}

Get a single tenant by UUID.

**Response 200** — the tenant object.

---

#### PATCH /admin/tenants/{id}

Update a tenant's name or active status.

**Request**

```json
{
  "name": "Acme Corporation",
  "is_active": false
}
```

**Response 200** — the updated tenant object.

---

#### DELETE /admin/tenants/{id}

Delete a tenant. All associated channel configs are cascade-deleted. Returns 204 on success.

---

## End-to-End Walkthrough

The following curl commands demonstrate the full lifecycle: create a tenant, authenticate, configure a Discord channel, send a notification, and check its delivery status.

### 1. Create a tenant

```bash
curl -s -X POST http://localhost:8080/admin/tenants \
  -H "Authorization: Bearer <admin-token>" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp", "slug": "acme"}' | jq .
```

Save the `api_key` and `api_secret` from the response. The secret is shown only once.

### 2. Get a JWT

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{
    "api_key": "<api_key>",
    "api_secret": "<api_secret>"
  }' | jq -r .token)
```

### 3. Create a Discord channel config

```bash
curl -s -X POST http://localhost:8080/channels \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "discord",
    "name": "ops-alerts",
    "config": {
      "webhook_url": "https://discord.com/api/webhooks/YOUR_ID/YOUR_TOKEN"
    }
  }' | jq .
```

Save the `id` from the response as `CHANNEL_ID`.

### 4. Send a notification

```bash
curl -s -X POST http://localhost:8080/notifications/send \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"channel_config_id\": \"$CHANNEL_ID\",
    \"subject\": \"Deployment finished\",
    \"body\": \"Production deploy v1.4.2 completed successfully.\"
  }" | jq .
```

The response has `"status": "pending"`. Save the `id` as `NOTIFICATION_ID`.

### 5. Check delivery status

```bash
curl -s http://localhost:8080/notifications/$NOTIFICATION_ID \
  -H "Authorization: Bearer $TOKEN" | jq .status
```

After the worker processes the task this will read `"delivered"`. To see each attempt:

```bash
curl -s http://localhost:8080/notifications/$NOTIFICATION_ID/attempts \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Channel Configuration

The `config` field in a channel config is a JSON object. Its shape depends on the channel type. The API validates the config against the expected schema before saving.

### Discord

```json
{
  "webhook_url": "https://discord.com/api/webhooks/<webhook_id>/<webhook_token>"
}
```

| Field | Required | Description |
|---|---|---|
| `webhook_url` | Yes | Full Discord webhook URL |

Messages are sent as Discord webhook payloads. If `subject` is provided it is prepended to the body in bold: `**subject**\nbody`.

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

Messages are sent via `sendMessage` with `parse_mode: Markdown`. If `subject` is provided it is prepended in bold: `*subject*\nbody`.

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

Messages are sent via the Meta Cloud API v19.0 as plain text. If `subject` is provided it is prepended in bold: `*subject*\nbody`.

---

## Telegram Admin Bot

notifyd includes an optional Telegram bot (`cmd/admin-bot`) that provides a conversational interface for tenant management and notification monitoring. The bot connects directly to the service layer (not the HTTP API), so it works even if the API server is down.

### Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and save the token.
2. Get your Telegram chat ID (send `/start` to [@userinfobot](https://t.me/userinfobot)).
3. Set the environment variables:
   ```bash
   export TELEGRAM_BOT_TOKEN="123456:ABC-DEF..."
   export TELEGRAM_ADMIN_CHAT="your-chat-id"
   ```
4. Start the bot:
   ```bash
   make admin-bot
   ```

The bot only responds to messages from the configured admin chat ID. All other messages are rejected.

### Commands

| Command | Description |
|---------|-------------|
| `/start`, `/help` | Show the command list |
| `/tenants` | List all tenants (name, slug, active status) |
| `/tenant <id>` | Show tenant details |
| `/create_tenant <name> <slug>` | Create a new tenant (returns api_key + api_secret) |
| `/toggle_tenant <id>` | Toggle a tenant's active status |
| `/delete_tenant <id>` | Delete a tenant (with inline keyboard confirmation) |
| `/notifications <tenant_id>` | List recent notifications for a tenant |
| `/notification <id>` | Show notification details |
| `/stats` | Show total tenants and notification counts by status |

### Docker

The admin bot runs as an optional Docker Compose profile:

```bash
docker compose --profile admin-bot up -d
```

Set `TELEGRAM_BOT_TOKEN` and `TELEGRAM_ADMIN_CHAT` in your environment or `.env` file before starting.

---

## Retry Strategy

Asynq manages retry scheduling using exponential backoff. notifyd configures each task with:

- **Max retries** — controlled by `MAX_RETRIES` (default: 5). A notification can be attempted at most `MAX_RETRIES + 1` times (one initial attempt plus up to five retries).
- **Backoff formula** — Asynq's default exponential backoff: delay grows with each retry, bounded by `MAX_RETRY_DELAY` (default: 30 minutes). The first retry waits approximately `MIN_RETRY_DELAY` (default: 15 seconds).
- **Per-task timeout** — each delivery attempt has a 30-second deadline before the context is cancelled.

### Status transitions during retry

| Event | Notification status |
|---|---|
| Task dequeued | `processing` |
| Provider call succeeds | `delivered` |
| Provider call fails, retries remain | `retrying` |
| Retries exhausted | `failed` |

### Dead letter queue

When a task exceeds its maximum retry count, Asynq moves it to the dead letter queue. The `NotifyErrorHandler` intercepts this event and updates the notification status to `failed` with the final error message. Dead-letter tasks are visible in the Asynqmon dashboard and can be re-enqueued manually from there.

### Security note on secrets

Channel credentials are intentionally not included in the Redis task payload. The worker fetches them from PostgreSQL at execution time using the `channel_config_id` stored in the task. This means compromising the Redis instance does not expose messaging credentials.

---

## Monitoring

### Asynqmon

A read-only Asynqmon instance runs at [http://localhost:8081](http://localhost:8081). It provides:

- Queue metrics (active, pending, scheduled, retry, dead letter)
- Per-task inspection and manual retry
- Worker server status

### Health endpoint

```
GET /health
```

Returns HTTP 200 with `{"status": "ok", "checks": {"postgres": "ok", "redis": "ok"}}` when all backends are reachable. Returns HTTP 503 with `"status": "degraded"` if any dependency is down. Suitable for use as a Docker or load-balancer health check.

### Logs

Both the API and worker use [zerolog](https://github.com/rs/zerolog) for structured JSON logging. Set `LOG_LEVEL` to `debug` for verbose output including per-request details.

---

## Development

### Prerequisites

- Go 1.25 or later
- PostgreSQL 16
- Redis 7
- [golang-migrate](https://github.com/golang-migrate/migrate) CLI
- [golangci-lint](https://golangci-lint.run/) (for linting)

### Run locally without Docker

**1. Start dependencies**

```bash
# PostgreSQL and Redis via Docker (infrastructure only)
docker compose up -d postgres redis
```

**2. Export environment variables**

```bash
export DATABASE_URL="postgres://notifyd:password@localhost:5432/notifyd?sslmode=disable"
export REDIS_ADDR="localhost:6379"
export JWT_SIGNING_KEY="local-dev-key-change-in-production"
```

**3. Run migrations**

```bash
make migrate-up
```

**4. Start the API server**

```bash
make api
```

**5. Start the worker** (in a separate terminal)

```bash
make worker
```

### Makefile targets

| Target | Description |
|---|---|
| `make api` | Run the API server with `go run` |
| `make worker` | Run the worker with `go run` |
| `make migrate-up` | Apply all pending migrations |
| `make migrate-down` | Roll back the most recent migration |
| `make migrate-create name=<name>` | Create a new migration file pair |
| `make test` | Run all tests with race detection |
| `make lint` | Run golangci-lint |
| `make docker-up` | Build images and start all services |
| `make docker-down` | Stop all services |
| `make docker-logs` | Tail logs for all services |

### Running tests

```bash
make test
```

---

## Project Structure

```
notifyd/
├── cmd/
│   ├── api/           # API server entry point
│   ├── worker/        # Worker server entry point
│   └── admin-bot/     # Telegram admin bot entry point
├── internal/
│   ├── auth/          # JWT manager and HTTP middleware
│   ├── bot/           # Telegram admin bot (commands, formatting)
│   ├── config/        # Environment-based configuration
│   ├── domain/        # Core types, interfaces, and status constants
│   ├── handler/       # HTTP handlers (auth, channel, notification, tenant, health)
│   ├── provider/      # Channel provider implementations (Discord, Telegram, WhatsApp)
│   ├── repository/    # PostgreSQL repository implementations
│   ├── router/        # Chi router wiring
│   ├── service/       # Business logic (tenant, channel, notification services)
│   └── worker/        # Asynq task definitions, dispatcher, and error handler
├── migrations/        # SQL migration files (golang-migrate)
├── pkg/
│   └── response/      # Shared HTTP response helpers
├── docker-compose.yml
├── Dockerfile
├── go.mod
└── Makefile
```

---

## License

MIT License. See [LICENSE](LICENSE) for the full text.
