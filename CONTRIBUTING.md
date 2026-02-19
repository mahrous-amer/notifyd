# Contributing to notifyd

Thank you for your interest in contributing to notifyd.

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Set up your development environment (see [Development](README.md#development) in the README)
4. Create a feature branch from `main`

## Development Workflow

```bash
# Start dependencies
docker compose up -d postgres redis

# Export environment variables
export DATABASE_URL="postgres://notifyd:password@localhost:5432/notifyd?sslmode=disable"
export REDIS_ADDR="localhost:6379"
export JWT_SIGNING_KEY="local-dev-key"

# Run migrations
make migrate-up

# Run the API server
make api

# Run the worker (separate terminal)
make worker

# Run tests
make test

# Run linter
make lint
```

## Code Style

- Follow standard Go conventions and `gofmt` formatting
- Keep functions focused on a single responsibility
- Use meaningful names that describe intent
- Write table-driven tests where applicable
- Handle errors explicitly; never discard errors silently

## Pull Request Process

1. Ensure your code compiles cleanly: `go build ./...`
2. Ensure `go vet ./...` passes with no warnings
3. Add or update tests for your changes
4. Update the README if your change affects the API, configuration, or deployment
5. Keep commits focused and write clear commit messages
6. Open a pull request with a description of what changed and why

## Adding a New Channel Provider

notifyd is designed to make adding new notification channels straightforward:

1. Create a new file in `internal/provider/` (e.g., `slack.go`)
2. Implement the `Provider` interface:
   - `Type() string` -- return the channel name
   - `ValidateConfig(json.RawMessage) error` -- validate channel-specific config
   - `Send(ctx, config, request) (*SendResponse, error)` -- deliver the message
3. Register the provider in both `cmd/api/main.go` and `cmd/worker/main.go`
4. Add the channel type to the `channel_type` enum in a new migration
5. Update the README with the new channel's config format

## Reporting Issues

- Use GitHub Issues for bug reports and feature requests
- Include steps to reproduce for bugs
- Include relevant logs and configuration (redact secrets)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
