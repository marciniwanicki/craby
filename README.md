# 🦀 crabby

[![main](https://github.com/marciniwanicki/crabby/actions/workflows/main.yml/badge.svg)](https://github.com/marciniwanicki/crabby/actions/workflows/main.yml)

An open-source personal AI assistant designed for experimental learning and daily utility.

## Architecture

```
┌─────────────┐     WebSocket      ┌─────────────┐      HTTP       ┌─────────────┐
│   CLI       │◄──────────────────►│   Daemon    │◄───────────────►│   Ollama    │
│  (crabby)   │    (protobuf)      │  (crabbyd)  │   (streaming)   │  (Qwen 2.5) │
└─────────────┘                    └─────────────┘                 └─────────────┘
```

Crabby uses a daemon architecture where a background server handles communication with Ollama, allowing fast responses and persistent connections.

## Prerequisites

- [Go 1.25+](https://golang.org/dl/)
- [Ollama](https://ollama.ai/) running locally
- A model pulled (default: `qwen2.5:14b`)

```bash
# Start Ollama (if not running)
ollama serve

# Pull the default model
ollama pull qwen2.5:14b
```

## Installation

```bash
# Clone the repository
git clone https://github.com/marciniwanicki/crabby.git
cd crabby

# Build
make build

# Or install to $GOPATH/bin
make install
```

## Usage

### Start the Daemon

The daemon must be running before using chat commands:

```bash
crabby daemon
```

### Chat

**Interactive mode** - start a conversation:

```bash
crabby
```

**One-shot mode** - send a single message:

```bash
crabby "What is the capital of France?"
```

In interactive mode, type your messages and press Enter. Type `exit` to leave or `Ctrl+C` to interrupt.

### Check Status

```bash
crabby status
```

Shows daemon status, version, model name, and Ollama health.

### Stop the Daemon

```bash
crabby stop
```

Gracefully shuts down the daemon.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8787` | Daemon listen port |
| `--ollama-url` | `http://localhost:11434` | Ollama API endpoint |
| `--model` | `qwen2.5:14b` | Model to use for chat |

Example with custom settings:

```bash
# Start daemon with different model
crabby daemon --model llama3.2 --port 9000

# Chat using the custom port
crabby --port 9000 "Hello!"
```

## Commands

| Command | Description |
|---------|-------------|
| `crabby` | Start interactive chat |
| `crabby "message"` | Send a one-shot message |
| `crabby daemon` | Start the daemon server |
| `crabby status` | Check daemon and Ollama status |
| `crabby stop` | Stop the running daemon |

## Customization

Crabby uses templates stored in `~/.crabby/` to customize the AI's behavior:

| File | Purpose |
|------|---------|
| `~/.crabby/identity.md` | Agent personality and guidelines |
| `~/.crabby/user.md` | User profile and context |
| `~/.crabby/settings.json` | Tool permissions and allowlist |

Templates are created automatically on first run. Edit them to personalize the assistant, then restart the daemon to apply changes.

## Development

```bash
make build      # Build the binary
make test       # Run tests
make lint       # Run linters
make format     # Format code
make proto      # Generate protobuf files
make help       # Show all targets
```

## License

Crabby is released under version 2.0 of the [Apache License](LICENSE).
