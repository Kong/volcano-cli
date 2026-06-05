SHELL := /usr/bin/env bash

BINARY      := volcano
PKG         := github.com/Kong/volcano-cli
VERSION_PKG := $(PKG)/internal/version
CONFIG_PKG  := $(PKG)/internal/config
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DEFAULT_API_URL ?= https://api.volcano.dev
FIRST_PARTY_DEVICE_CLIENT_ID ?=

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE) \
	-X $(CONFIG_PKG).compiledDefaultAPIURL=$(DEFAULT_API_URL) \
	-X $(CONFIG_PKG).compiledFirstPartyDeviceClientID=$(FIRST_PARTY_DEVICE_CLIENT_ID)

.PHONY: all build test api-e2e-smoke api-e2e-cloud localmode-e2e lint tidy check clean help

all: build

build: ## Build the volcano binary into ./$(BINARY)
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/volcano

test: ## Run unit tests
	go test ./...

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

check: lint test ## Run lint + test

clean: ## Remove build artifacts
	rm -f $(BINARY)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
