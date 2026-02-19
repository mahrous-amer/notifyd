.PHONY: api worker admin-bot migrate-up migrate-down test lint docker-up docker-down

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

admin-bot:
	go run ./cmd/admin-bot

migrate-up:
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

migrate-create:
	migrate create -ext sql -dir ./migrations -seq $(name)

test:
	go test ./... -v -race -count=1

lint:
	golangci-lint run ./...

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
