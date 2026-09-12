# crossplane-mcp-server
#
# Run `make help` for the list of targets.

SHELL := /usr/bin/env bash -o errexit -o pipefail -o nounset

BINARY      := crossplane-mcp-server
MODULE      := github.com/ravibagri5/crossplane-mcp-server
PKG_VERSION := $(MODULE)/pkg/version

OUT_DIR   := bin
DIST_DIR  := dist

VERSION    ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo 0.0.0-dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG_VERSION).Version=$(VERSION) \
	-X $(PKG_VERSION).Commit=$(COMMIT) \
	-X $(PKG_VERSION).BuildDate=$(BUILD_DATE)

GOLANGCI_LINT_VERSION := v2.13.2
GOVULNCHECK_VERSION   := latest

.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build
build: ## Build the binary into ./bin
	@mkdir -p $(OUT_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT_DIR)/$(BINARY) ./cmd/$(BINARY)
	@echo "built $(OUT_DIR)/$(BINARY) $(VERSION)"

.PHONY: install
install: ## Install the binary into $GOPATH/bin
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BINARY)

.PHONY: run
run: ## Run the server over stdio against the current kubeconfig context
	go run ./cmd/$(BINARY)

.PHONY: tools
tools: ## Print the tools this build exposes
	go run ./cmd/$(BINARY) tools

.PHONY: image
image: ## Build the container image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t ghcr.io/ravibagri5/$(BINARY):$(VERSION) .

##@ Test

.PHONY: test
test: ## Run the unit tests
	go test -race -count=1 ./...

.PHONY: test-coverage
test-coverage: ## Run the unit tests and report coverage
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: inspector
inspector: build ## Run the MCP Inspector against a local build
	npx @modelcontextprotocol/inspector $(OUT_DIR)/$(BINARY)

##@ Quality

.PHONY: fmt
fmt: ## Format the code
	go fmt ./...
	go run golang.org/x/tools/cmd/goimports@latest -w -local $(MODULE) .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

.PHONY: vulncheck
vulncheck: ## Check dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: verify
verify: tidy fmt ## Fail if tidy or fmt produced changes
	@git diff --exit-code -- go.mod go.sum || \
		(echo "go.mod or go.sum is not tidy, run 'make tidy'" && exit 1)
	@git diff --exit-code || \
		(echo "code is not formatted, run 'make fmt'" && exit 1)

.PHONY: check
check: verify vet lint test ## Run everything CI runs

##@ Housekeeping

.PHONY: clean
clean: ## Remove build output
	rm -rf $(OUT_DIR) $(DIST_DIR) coverage.out
