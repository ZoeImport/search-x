.DEFAULT_GOAL := run

.PHONY: run check test vet build

run:
	go run ./cmd/websearch-api -config config.yaml

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/websearch-api

check: test vet build
