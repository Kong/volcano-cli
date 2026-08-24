SHELL := /usr/bin/env bash

BINARY      := volcano
PKG         := github.com/Kong/volcano-cli
VERSION_PKG := $(PKG)/internal/version
CONFIG_PKG  := $(PKG)/internal/config
LOCALMODE_PKG := $(PKG)/internal/localmode
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DEFAULT_API_URL ?= https://api.volcano.dev
DEFAULT_WEB_URL ?= https://volcano.dev
# Local-mode server image baked into `make build`; override with DEFAULT_LOCAL_IMAGE.
DEFAULT_LOCAL_IMAGE ?= kong/volcano:local-nightly
FIRST_PARTY_DEVICE_CLIENT_ID ?=

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE) \
	-X $(CONFIG_PKG).compiledDefaultAPIURL=$(DEFAULT_API_URL) \
	-X $(CONFIG_PKG).compiledDefaultWebURL=$(DEFAULT_WEB_URL) \
	-X $(CONFIG_PKG).compiledFirstPartyDeviceClientID=$(FIRST_PARTY_DEVICE_CLIENT_ID) \
	-X $(LOCALMODE_PKG).defaultVolcanoImage=$(DEFAULT_LOCAL_IMAGE)

.PHONY: all build local test test-installer api-e2e-smoke api-e2e-cloud localmode-e2e lint tidy check clean help openapi-generate openapi-generated-check

all: build

build: ## Build the volcano binary into ./$(BINARY)
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/volcano

local: ## Build volcano using variables loaded from .env.local
	@if [ ! -f .env.local ]; then \
		echo ".env.local not found. Create one with VOLCANO_WEB_URL=... and VOLCANO_API_URL=..."; \
		exit 1; \
	fi; \
	set -a; source .env.local; set +a; \
	if [ -z "$${FIRST_PARTY_DEVICE_CLIENT_ID:-}" ] && [ -n "$${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID:-}" ]; then \
		export FIRST_PARTY_DEVICE_CLIENT_ID="$${VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID}"; \
	fi; \
	if [ -z "$${DEFAULT_API_URL:-}" ] && [ -n "$${VOLCANO_API_URL:-}" ]; then \
		export DEFAULT_API_URL="$${VOLCANO_API_URL}"; \
	fi; \
	if [ -z "$${DEFAULT_WEB_URL:-}" ] && [ -n "$${VOLCANO_WEB_URL:-}" ]; then \
		export DEFAULT_WEB_URL="$${VOLCANO_WEB_URL}"; \
	fi; \
	if [ -z "$${DEFAULT_LOCAL_IMAGE:-}" ]; then \
		export DEFAULT_LOCAL_IMAGE="$${VOLCANO_IMAGE:-kong/volcano:local-nightly}"; \
	fi; \
	$(MAKE) build

test: test-installer ## Run unit tests
	go test ./...

test-installer: ## Test the release installer
	bash scripts/install-volcano.test.sh

openapi-generate: ## Regenerate the API client from the vendored OpenAPI contract
	go generate ./internal/apiclient

openapi-generated-check: ## Verify generated API client code is current
	@set -e; \
	before="$$(git hash-object internal/apiclient/client.gen.go)"; \
	$(MAKE) openapi-generate; \
	after="$$(git hash-object internal/apiclient/client.gen.go)"; \
	if [ "$$before" != "$$after" ]; then \
		echo "ERROR: generated API client is out of date; run 'make openapi-generate' and commit the result"; \
		git --no-pager diff --stat -- internal/apiclient/client.gen.go; \
		exit 1; \
	fi
	@echo "Generated API client is up to date"

api-e2e-smoke: ## Run CLI API smoke tests against VOLCANO_API_URL and VOLCANO_MGMT_URL
	VOLCANO_API_E2E=1 go test ./tests/e2e/api -run '^TestAPIE2ESmoke' -count=1 -timeout 45m

api-e2e-cloud: ## Run provisioning CLI API E2E tests against VOLCANO_API_URL and VOLCANO_MGMT_URL
	VOLCANO_API_E2E=1 go test ./tests/e2e/api -run '^TestAPIE2E(Smoke|Cloud)' -count=1 -timeout 240m

localmode-e2e: ## Run destructive local-mode Docker smoke tests
	VOLCANO_LOCALMODE_E2E=1 go test ./tests/e2e/localmode -run TestLocalModeE2ESmoke -count=1 -timeout 20m

lint: ## Run golangci-lint (includes gofmt, goimports, and go vet)
	go tool golangci-lint run ./...
	go tool golangci-lint fmt --diff ./...

tidy: ## Run go mod tidy
	go mod tidy

check: openapi-generated-check lint test ## Run generated-code check, lint, and test

clean: ## Remove build artifacts
	rm -f $(BINARY)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
