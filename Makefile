SHELL := /bin/bash

GO ?= go
GOOSE ?= goose
SQLC ?= sqlc
GOLANGCI_LINT ?= golangci-lint
DOCKER_COMPOSE ?= docker compose
DB_DSN ?= root:root@tcp(localhost:3306)/live_revenue?parseTime=true&multiStatements=true

.PHONY: tools
tools:
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
	$(GO) install github.com/pressly/goose/v3/cmd/goose@v3.27.0
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1

.PHONY: compose-validate
compose-validate:
	$(DOCKER_COMPOSE) config -q

.PHONY: up
up:
	bash scripts/up_stack.sh

.PHONY: up-observability
up-observability:
	bash scripts/up_observability.sh

.PHONY: down
down:
	$(DOCKER_COMPOSE) down --remove-orphans

.PHONY: clean
clean:
	$(DOCKER_COMPOSE) down -v --remove-orphans
	rm -rf .docker-data/mysql .docker-data/kafka

.PHONY: sqlc-generate
sqlc-generate:
	$(SQLC) generate

.PHONY: goose-up
goose-up:
	$(GOOSE) -dir migrations mysql "$(DB_DSN)" up

.PHONY: goose-down
goose-down:
	$(GOOSE) -dir migrations mysql "$(DB_DSN)" down

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: race
race:
	$(GO) test -race ./...

.PHONY: k6-smoke
k6-smoke:
	k6 run scripts/k6/smoke.js

.PHONY: k6-benchmark
k6-benchmark:
	k6 run scripts/k6/benchmark.js

.PHONY: demo
demo:
	bash scripts/demo.sh

.PHONY: demo-reset
demo-reset:
	bash scripts/reset_demo_state.sh

.PHONY: demo-hard-idempotency
demo-hard-idempotency:
	bash scripts/demo_idempotency.sh

.PHONY: demo-hard-recovery
demo-hard-recovery:
	bash scripts/demo_recovery.sh
