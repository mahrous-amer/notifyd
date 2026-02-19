FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM builder AS api-builder
RUN CGO_ENABLED=0 go build -o /api ./cmd/api

FROM builder AS worker-builder
RUN CGO_ENABLED=0 go build -o /worker ./cmd/worker

FROM builder AS admin-bot-builder
RUN CGO_ENABLED=0 go build -o /admin-bot ./cmd/admin-bot

FROM alpine:3.20 AS api
RUN apk add --no-cache ca-certificates
COPY --from=api-builder /api /api
COPY migrations /migrations
EXPOSE 8080
CMD ["/api"]

FROM alpine:3.20 AS worker
RUN apk add --no-cache ca-certificates
COPY --from=worker-builder /worker /worker
CMD ["/worker"]

FROM alpine:3.20 AS admin-bot
RUN apk add --no-cache ca-certificates
COPY --from=admin-bot-builder /admin-bot /admin-bot
CMD ["/admin-bot"]
