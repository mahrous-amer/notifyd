FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Download golang-migrate for database migrations
FROM alpine:3.20 AS migrate-downloader
ARG TARGETARCH=amd64
RUN apk add --no-cache curl && \
    curl -fsSL "https://github.com/golang-migrate/migrate/releases/download/v4.18.2/migrate.linux-${TARGETARCH}.tar.gz" | \
    tar -xz -C /usr/local/bin/

# ─── API ────────────────────────────────────────────────────
FROM builder AS api-builder
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /api ./cmd/api

FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates curl && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup -h /home/appuser -s /sbin/nologin
COPY --from=migrate-downloader /usr/local/bin/migrate /usr/local/bin/migrate
COPY --from=api-builder /api /api
COPY migrations /migrations
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:8080/health || exit 1
USER appuser
ENTRYPOINT ["/entrypoint.sh"]
CMD ["/api"]

# ─── Worker ─────────────────────────────────────────────────
FROM builder AS worker-builder
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /worker ./cmd/worker

FROM alpine:3.20 AS worker
RUN apk add --no-cache ca-certificates && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup -h /home/appuser -s /sbin/nologin
COPY --from=worker-builder /worker /worker
USER appuser
CMD ["/worker"]

# ─── Admin Bot ──────────────────────────────────────────────
FROM builder AS admin-bot-builder
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /admin-bot ./cmd/admin-bot

FROM alpine:3.20 AS admin-bot
RUN apk add --no-cache ca-certificates && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup -h /home/appuser -s /sbin/nologin
COPY --from=admin-bot-builder /admin-bot /admin-bot
USER appuser
CMD ["/admin-bot"]
