# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make ready      # Pre-PR checklist: format → proto → lint → test → build
make build      # Compile binary to ./craby
make test       # Run tests with race detection: go test -v -race ./...
make lint       # Run golangci-lint
make format     # Format with goimports
make proto      # Regenerate protobuf: internal/api/messages.pb.go
make deps       # Download and tidy Go modules
make install    # Install to $GOPATH/bin
```

Run a single test: `go test -v -race ./internal/agent -run TestAgentLoop`

## Architecture

Craby is a daemon-based AI assistant using WebSocket communication and Ollama for local LLM inference.

```
CLI (craby) ←── WebSocket/Protobuf ──→ Daemon (crabyd) ←── HTTP ──→ Ollama
```

### Key Layers

- **CLI** (`cmd/craby/`): Cobra commands - chat, daemon, status, stop
- **Client** (`internal/client/`): WebSocket connection, protobuf encoding, response streaming
- **Daemon** (`internal/daemon/`): HTTP/WebSocket server, Ollama client integration
- **Agent** (`internal/agent/`): LLM + tool execution loop (max 10 iterations), event streaming
- **Tools** (`internal/tools/`): Registry pattern, shell tool with command allowlisting
- **Config** (`internal/config/`): JSON settings and embedded markdown templates

### Protocol

Protobuf messages defined in `internal/api/messages.proto`:
- `ChatRequest`: user message + optional session_id
- `ChatResponse`: oneof TextChunk, ToolCall, ToolResult, Done, Error

### Tool System

Tools implement the `Tool` interface (Name, Description, Parameters, Execute). The shell tool validates commands against an allowlist and blocks shell operators (`&&`, `|`, `;`, etc.).

### Configuration

User config lives in `~/.craby/`:
- `settings.json`: tool permissions, shell allowlist
- `identity.md`, `user.md`: AI personality templates (auto-created from `templates/`)

CLI flags: `--port` (8787), `--ollama-url` (localhost:11434), `--model` (qwen2.5:14b)

## Linting

golangci-lint v2 with: errcheck, govet, staticcheck, gosec, gocritic, errorlint, bodyclose, nilerr. Generated `*.pb.go` files are excluded.
