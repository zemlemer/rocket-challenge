ROCKETS_PG_PORT ?= 15432
TEST_DATABASE_URL ?= postgres://rockets:rockets@localhost:$(ROCKETS_PG_PORT)/rockets?sslmode=disable
export ROCKETS_PG_PORT

.PHONY: build test test-integration lint db-up db-down

build:
	go build -o bin/rockets ./cmd/rockets

# Unit tests; integration tests are skipped unless TEST_DATABASE_URL is set.
test:
	go test -race ./...

# Requires a running database: make db-up
test-integration:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -count=1 ./...

lint:
	go vet ./...
	golangci-lint run ./...

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down
