.PHONY: build proto clean install deps format lint help

# Binary names
BINARY_NAME=crabby

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

build: ## Build the crabby binary
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/crabby

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
	golangci-lint run ./...

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
