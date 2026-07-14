.DEFAULT_GOAL := run

GO ?= go
BINARY ?= bin/search-api
Q ?= golang
COMPOSE ?= $(shell if docker compose version >/dev/null 2>&1; then echo "docker compose"; elif command -v docker-compose >/dev/null 2>&1; then echo "docker-compose"; fi)

.PHONY: run build test test-race vet check smoke require-compose docker-build docker-up docker-down docker-logs docker-ps help
.NOTPARALLEL: check

run: ## Run the Gin API locally
	$(GO) run ./cmd/server

build: ## Build the API binary
	mkdir -p $(dir $(BINARY))
	$(GO) build -o $(BINARY) ./cmd/server

test: ## Run unit and integration tests
	$(GO) test ./...

test-race: ## Run tests with the race detector
	$(GO) test ./... -race

vet: ## Run Go static analysis
	$(GO) vet ./...

check: test test-race vet build ## Run all verification steps

smoke: ## Query the running API (Q=golang)
	./scripts/live-smoke.sh "$(Q)"

require-compose:
	@if [ -z "$(COMPOSE)" ]; then echo "Docker Compose not found: install the plugin or docker-compose" >&2; exit 1; fi

docker-build: require-compose ## Build the Docker image
	$(COMPOSE) build

docker-up: require-compose ## Build and start the Docker service
	$(COMPOSE) up -d --build

docker-down: require-compose ## Stop containers and preserve volumes
	$(COMPOSE) down

docker-logs: require-compose ## Follow API container logs
	$(COMPOSE) logs -f search-api

docker-ps: require-compose ## Show Compose service status
	$(COMPOSE) ps

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
