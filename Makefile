.DEFAULT_GOAL := run

GO ?= go
PYTHON ?= python3
DEMO_ORIGINS ?= http://127.0.0.1:8090,http://localhost:8090
DEMO_WEBSEARCH_ALLOW_REQUEST_PROVIDERS ?= true
DEMO_WEBSEARCH_PROVIDER_VISIBILITY ?= public

.PHONY: run run-websearch run-webfetch run-demo check test test-race vet build

run:
	$(MAKE) -j3 run-websearch run-webfetch run-demo

run-websearch:
	cd websearch && \
		WEBSEARCH_CORS_ALLOWED_ORIGINS="$(DEMO_ORIGINS)" \
		WEBSEARCH_ALLOW_REQUEST_PROVIDERS="$(DEMO_WEBSEARCH_ALLOW_REQUEST_PROVIDERS)" \
		WEBSEARCH_RESPONSE_PROVIDER_VISIBILITY="$(DEMO_WEBSEARCH_PROVIDER_VISIBILITY)" \
		$(GO) run ./cmd/websearch-api -config config.yaml

run-webfetch:
	cd webfetch && WEBFETCH_CORS_ALLOWED_ORIGINS="$(DEMO_ORIGINS)" $(GO) run ./cmd/webfetch-api -config config.yaml

run-demo:
	cd demo && $(PYTHON) -m http.server 8090

test:
	cd runtime && go test ./...
	cd websearch && go test ./...
	cd webfetch && go test ./...

test-race:
	cd runtime && go test -race ./...
	cd websearch && go test -race ./...
	cd webfetch && go test -race ./...

vet:
	cd runtime && go vet ./...
	cd websearch && go vet ./...
	cd webfetch && go vet ./...

build:
	cd websearch && go build -o ../bin/websearch-api ./cmd/websearch-api
	cd webfetch && go build -o ../bin/webfetch-api ./cmd/webfetch-api

check: test test-race vet build
