VERSION ?= dev
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

.PHONY: dev build test lint generate ci

dev:
	npm --prefix web run dev & go run ./cmd/inventory; kill %1

build:
	npm --prefix web ci
	npm --prefix web run build
	go build -ldflags "$(LDFLAGS)" -o bin/inventory ./cmd/inventory

test:
	go test ./...
	npm --prefix web test

lint:
	go vet ./...
	npm --prefix web run check

generate:
	@:

ci: lint test build
