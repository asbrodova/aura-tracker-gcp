BINARY      := aura-tracker-gcp
VERSION     ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo "dev")
LDFLAGS     := -ldflags="-X main.version=$(VERSION) -s -w"
TESTFLAGS   := -race
LINT_VERSION := v2.12.2

.DEFAULT_GOAL := help

.PHONY: build test test-cover lint vet tidy smoke clean install-lint help

build: ## Build the binary
	go build $(LDFLAGS) -o $(BINARY) ./cmd/aura-tracker-gcp

test: ## Run tests with race detector
	go test $(TESTFLAGS) ./...

test-cover: ## Run tests and emit HTML coverage report
	go test $(TESTFLAGS) -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

lint: install-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy and verify go.mod / go.sum
	go mod tidy
	go mod verify

smoke: ## Verify MCP protocol compliance and print tool count (no GCP credentials required)
	@go test -run TestToolsListProtocolCompliance -v ./internal/mcp/ 2>&1 \
	  | grep -E "PASS|FAIL|tools registered|^ok|^---"

install-lint: ## Install golangci-lint if not present
	@test -f $(GOLANGCI_LINT) || \
	  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh \
	  | sh -s -- -b "$$(go env GOPATH)/bin" $(LINT_VERSION)

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out coverage.html

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
