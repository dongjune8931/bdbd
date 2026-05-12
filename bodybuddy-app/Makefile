.PHONY: build test lint \
	run-user-service run-score-service run-analysis-worker run-notification-worker \
	docker-build migrate-up migrate-down

# Build all binaries.
build:
	go build ./...

# Run all tests.
test:
	go test ./...

# Lint with golangci-lint (install: https://golangci-lint.run/usage/install/).
lint:
	golangci-lint run ./...

# Run individual services locally.
# Requires all env vars to be set (see each service's config struct).
run-user-service:
	go run ./cmd/user-service/

run-score-service:
	go run ./cmd/score-service/

run-analysis-worker:
	go run ./cmd/analysis-worker/

run-notification-worker:
	go run ./cmd/notification-worker/

# Build Docker images for all services (tagged :dev).
docker-build:
	docker build -f deploy/docker/Dockerfile.user-service         -t bodybuddy/user-service:dev .
	docker build -f deploy/docker/Dockerfile.score-service        -t bodybuddy/score-service:dev .
	docker build -f deploy/docker/Dockerfile.analysis-worker      -t bodybuddy/analysis-worker:dev .
	docker build -f deploy/docker/Dockerfile.notification-worker  -t bodybuddy/notification-worker:dev .

# Apply migrations against the local Postgres container (assumes docker-compose is up).
migrate-up:
	docker exec -i bodybuddy-postgres psql -U bodybuddy -d bodybuddy < migrations/000001_init.up.sql

# Roll back migrations.
migrate-down:
	docker exec -i bodybuddy-postgres psql -U bodybuddy -d bodybuddy < migrations/000001_init.down.sql
