.DEFAULT_GOAL := run

.PHONY: run check test vet build

run:
	go run ./cmd/webfetch-api -config config.yaml

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/webfetch-api

check: test vet build
