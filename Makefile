VERSION ?= dev
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2

.PHONY: dev build test lint generate generate-check ci smoke docker-smoke docker-multiarch

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
	sqlc generate

generate-check: generate
	git diff --exit-code

smoke:
	sh scripts/smoke.sh

docker-smoke:
	sh scripts/docker-smoke.sh

docker-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -t inventory:multiarch .

ci: lint test build

# Runs the AI test set against the endpoint in INVENTORY_AI_VISION_* / INVENTORY_AI_TEXT_*.
# Manual, not part of CI: make ai-eval CASES=~/inventory-ai-cases [EVALFLAGS="-mode strict -v"]
.PHONY: ai-eval
ai-eval:
	go run ./cmd/ai-eval $(if $(CASES),-cases $(CASES)) $(EVALFLAGS)
