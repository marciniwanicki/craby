.PHONY: build proto clean install deps format lint test ready help

# Binary names
BINARY_NAME=craby

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod

# Proto parameters
PROTOC=protoc
PROTO_DIR=internal/api
PROTO_FILES=$(PROTO_DIR)/messages.proto

# Build flags
LDFLAGS=-ldflags "-s -w"

.DEFAULT_GOAL := help

all: proto build ## Build everything (proto + binary)

build: ## Build the craby binary
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/craby

proto: ## Generate protobuf Go code
	$(PROTOC) --go_out=. --go_opt=paths=source_relative $(PROTO_FILES)

clean: ## Remove build artifacts
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f $(PROTO_DIR)/*.pb.go

install: build ## Install binary to $GOPATH/bin
	mv $(BINARY_NAME) $(GOPATH)/bin/

deps: ## Download and tidy dependencies
	$(GOMOD) download
	$(GOMOD) tidy

format: ## Format code with goimports
	go run golang.org/x/tools/cmd/goimports@latest -w .

lint: ## Run linters
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...

test: ## Run tests
	go test -v -race ./...

ready: ## Run all checks before PR (format, proto, lint, test, build)
	@echo "==> Formatting code..."
	@$(MAKE) format
	@echo "==> Generating protobuf..."
	@$(MAKE) proto
	@echo "==> Running linter..."
	@$(MAKE) lint
	@echo "==> Running tests..."
	@$(MAKE) test
	@echo "==> Building..."
	@$(MAKE) build
	@echo "==> All checks passed!"

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
