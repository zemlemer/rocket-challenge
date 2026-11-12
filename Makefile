ROCKETS_PG_PORT ?= 15432
TEST_DATABASE_URL ?= postgres://rockets:rockets@localhost:$(ROCKETS_PG_PORT)/rockets?sslmode=disable
export ROCKETS_PG_PORT

.PHONY: build test test-integration test-e2e lint db-up db-down

build:
	go build -o bin/rockets ./cmd/rockets

# Unit tests; integration tests are skipped unless TEST_DATABASE_URL is set.
test:
	go test -race ./...

# Requires a running database: make db-up
test-integration:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -count=1 ./...

# End-to-end tests against the compose stack (started if needed). They change
# the stack's data and restart server containers. The simulator run is
# included when ROCKETS_SIMULATOR points at the challenge's binary.
# The simulator from the challenge ZIP, if placed in tools/ (gitignored).
ROCKETS_SIMULATOR ?= $(wildcard $(CURDIR)/tools/rockets-simulator)
export ROCKETS_SIMULATOR

test-e2e:
	docker compose up --build -d
	go test -tags e2e -count=1 -v ./test/e2e/

lint:
	go vet ./...
	golangci-lint run ./...
	golangci-lint run --build-tags e2e ./test/...

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down
