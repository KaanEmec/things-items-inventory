VERSION ?= dev
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2

.PHONY: dev build test lint generate generate-check ci

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
	go run $(STATICCHECK) ./...
	npm --prefix web run check

generate:
	@:

generate-check: generate
	git diff --exit-code

ci: lint test build
