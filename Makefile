# ─── Configuration ───────────────────────────────────────────────────────────
DATABASE_URL   ?= postgres://notifyd:password@localhost:5432/notifyd?sslmode=disable
REDIS_ADDR     ?= localhost:6379
JWT_SIGNING_KEY?= local-dev-key
API_PORT       ?= 8080
LOG_LEVEL      ?= info

DOCKER_COMPOSE  = docker compose
MIGRATE         = migrate

# ─── Phony targets ───────────────────────────────────────────────────────────
.PHONY: help build api worker admin-bot \
        migrate-up migrate-down migrate-create migrate-reset \
        test lint vet check \
        infra-up infra-down infra-logs \
        dev dev-api dev-worker \
        docker-up docker-down docker-logs docker-build \
        prod-up prod-down prod-logs \
        e2e seed clean \
        load-test load-test-auth load-test-send load-test-query coverage

# ─── Default ─────────────────────────────────────────────────────────────────
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ─── Build ───────────────────────────────────────────────────────────────────
build: ## Compile all binaries to ./bin/
	@mkdir -p bin
	CGO_ENABLED=0 go build -o bin/api    ./cmd/api
	CGO_ENABLED=0 go build -o bin/worker ./cmd/worker
	CGO_ENABLED=0 go build -o bin/admin-bot ./cmd/admin-bot

# ─── Run (local, no Docker) ─────────────────────────────────────────────────
api: ## Run the API server
	DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" JWT_SIGNING_KEY="$(JWT_SIGNING_KEY)" \
	API_PORT="$(API_PORT)" LOG_LEVEL="$(LOG_LEVEL)" go run ./cmd/api

worker: ## Run the async worker
	DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" JWT_SIGNING_KEY="$(JWT_SIGNING_KEY)" \
	LOG_LEVEL="$(LOG_LEVEL)" go run ./cmd/worker

admin-bot: ## Run the Telegram admin bot
	DATABASE_URL="$(DATABASE_URL)" JWT_SIGNING_KEY="$(JWT_SIGNING_KEY)" \
	LOG_LEVEL="$(LOG_LEVEL)" go run ./cmd/admin-bot

# ─── Migrations ──────────────────────────────────────────────────────────────
migrate-up: ## Apply all pending migrations
	$(MIGRATE) -path ./migrations -database "$(DATABASE_URL)" up

migrate-down: ## Roll back the last migration
	$(MIGRATE) -path ./migrations -database "$(DATABASE_URL)" down 1

migrate-create: ## Create a new migration (usage: make migrate-create name=add_xyz)
	$(MIGRATE) create -ext sql -dir ./migrations -seq $(name)

migrate-reset: ## Drop all tables then re-apply migrations
	$(MIGRATE) -path ./migrations -database "$(DATABASE_URL)" drop -f
	$(MIGRATE) -path ./migrations -database "$(DATABASE_URL)" up

# ─── Quality ─────────────────────────────────────────────────────────────────
test: ## Run unit tests with race detector
	go test ./... -v -race -count=1

lint: ## Run golangci-lint
	golangci-lint run ./...

vet: ## Run go vet
	go vet ./...

check: vet lint test ## Run vet + lint + tests

# ─── Infrastructure (Postgres + Redis only) ──────────────────────────────────
infra-up: ## Start Postgres and Redis containers
	$(DOCKER_COMPOSE) up -d postgres redis
	@echo "Waiting for services to be healthy..."
	@until $(DOCKER_COMPOSE) exec -T postgres pg_isready -U notifyd > /dev/null 2>&1; do sleep 1; done
	@until $(DOCKER_COMPOSE) exec -T redis redis-cli ping > /dev/null 2>&1; do sleep 1; done
	@echo "Postgres and Redis are ready."

infra-down: ## Stop Postgres and Redis containers
	$(DOCKER_COMPOSE) down

infra-logs: ## Tail Postgres and Redis logs
	$(DOCKER_COMPOSE) logs -f postgres redis

# ─── Local dev (infra + migrate + run) ───────────────────────────────────────
dev-api: infra-up migrate-up api ## Start infra, run migrations, start API

dev-worker: infra-up worker ## Start infra, start worker

dev: ## Start full local stack (infra + migrate + API & worker in parallel)
	@$(MAKE) infra-up
	@$(MAKE) migrate-up
	@echo "Starting API and worker..."
	@trap 'kill 0' INT TERM; \
		$(MAKE) api & \
		$(MAKE) worker & \
		wait

# ─── Full Docker deployment ──────────────────────────────────────────────────
docker-build: ## Build all Docker images
	$(DOCKER_COMPOSE) build

docker-up: ## Deploy full stack via Docker Compose
	$(DOCKER_COMPOSE) up -d --build

docker-down: ## Tear down all Docker containers
	$(DOCKER_COMPOSE) down

docker-logs: ## Tail all Docker logs
	$(DOCKER_COMPOSE) logs -f

# ─── Production deployment ─────────────────────────────────────────────────────
prod-up: ## Deploy production stack
	$(DOCKER_COMPOSE) -f docker-compose.prod.yml up -d --build

prod-down: ## Tear down production stack
	$(DOCKER_COMPOSE) -f docker-compose.prod.yml down

prod-logs: ## Tail production logs
	$(DOCKER_COMPOSE) -f docker-compose.prod.yml logs -f

# ─── Load Testing ────────────────────────────────────────────────────────────
load-test: ## Run k6 load test (full scenario)
	k6 run loadtest/full_scenario.js

load-test-auth: ## Load test auth endpoint
	k6 run loadtest/auth.js

load-test-send: ## Load test notification sending
	k6 run loadtest/notifications.js

load-test-query: ## Load test query endpoints
	k6 run loadtest/queries.js

# ─── Coverage ────────────────────────────────────────────────────────────────
coverage: ## Run tests with coverage report
	go test ./... -race -coverprofile=coverage.out -covermode=atomic
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ─── End-to-end tests ────────────────────────────────────────────────────────
seed: ## Insert a bootstrap tenant for testing
	@echo "Creating bootstrap tenant..."
	@HASH=$$(go run ./scripts/bcrypt.go test-secret-12345) && \
	$(DOCKER_COMPOSE) exec -T postgres psql -U notifyd -d notifyd -c \
		"INSERT INTO tenants (id, name, slug, api_key, api_secret, is_active, created_at, updated_at) \
		 VALUES ('00000000-0000-0000-0000-000000000001', 'Test Company', 'test-co', 'test-api-key-123', '$$HASH', true, NOW(), NOW()) \
		 ON CONFLICT (slug) DO NOTHING;"
	@echo "Bootstrap tenant ready."
	@echo "  API Key:    test-api-key-123"
	@echo "  API Secret: test-secret-12345"

e2e: ## Run end-to-end test suite (requires running API)
	@bash scripts/e2e_test.sh

# ─── Cleanup ─────────────────────────────────────────────────────────────────
clean: ## Remove build artifacts and stop containers
	rm -rf bin/
	rm -f coverage.out coverage.html
	$(DOCKER_COMPOSE) down -v
