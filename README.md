# 🦀 craby

[![main](https://github.com/marciniwanicki/craby/actions/workflows/main.yml/badge.svg)](https://github.com/marciniwanicki/craby/actions/workflows/main.yml)

An open-source personal AI assistant designed for experimental learning and daily utility.

## Architecture

```
┌─────────────┐     WebSocket      ┌─────────────┐      HTTP       ┌─────────────┐
│   CLI       │◄──────────────────►│   Daemon    │◄───────────────►│   Ollama    │
│  (craby)    │    (protobuf)      │  (crabyd)   │   (streaming)   │  (Qwen 2.5) │
└─────────────┘                    └─────────────┘                 └─────────────┘
```

Craby uses a daemon architecture where a background server handles communication with Ollama, allowing fast responses and persistent connections.

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
git clone https://github.com/marciniwanicki/craby.git
cd craby

# Build
make build

# Or install to $GOPATH/bin
make install
```

## Usage

### Start the Daemon

The daemon must be running before using chat commands:

```bash
craby daemon
```

### Chat

**Interactive mode** - start a conversation:

```bash
craby
```

**One-shot mode** - send a single message:

```bash
craby "What is the capital of France?"
```

In interactive mode, type your messages and press Enter. Type `/exit` to leave or `Ctrl+C` to interrupt.

### Chat Commands

While in interactive mode, you can use special commands:

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/exit` | Leave the chat |
| `/terminate` | Stop the daemon and exit |
| `/tools` | List available external tools |
| `/history` | Show conversation history |
| `/context` | Show full context sent to the LLM |
| `/context <text>` | Add custom context for subsequent messages |
| `/context clear` | Clear custom context |

### Check Status

```bash
craby status
```

Shows daemon status, version, model name, and Ollama health.

### Stop the Daemon

```bash
craby terminate
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
craby daemon --model llama3.2 --port 9000

# Chat using the custom port
craby --port 9000 "Hello!"
```

## Commands

| Command | Description |
|---------|-------------|
| `craby` | Start interactive chat |
| `craby "message"` | Send a one-shot message |
| `craby daemon` | Start the daemon server |
| `craby status` | Check daemon and Ollama status |
| `craby terminate` | Stop the running daemon |
| `craby tools` | List loaded external tools |

## Customization

Craby uses templates stored in `~/.craby/` to customize the AI's behavior:

| File | Purpose |
|------|---------|
| `~/.craby/identity.md` | Agent personality and guidelines |
| `~/.craby/user.md` | User profile and context |
| `~/.craby/settings.json` | Tool permissions and allowlist |

Templates are created automatically on first run. Edit them to personalize the assistant, then restart the daemon to apply changes.

## External Tools

Craby can integrate with external CLI tools. Define tools in `~/.craby/tools/<name>/<name>.yaml`:

```yaml
name: mytool
description: "Description of what the tool does"
when_to_use: "When the user asks about X"

access:
  type: shell
  command: mytool

check:
  command: "mytool --version"
```

When the agent first uses an external tool, it automatically discovers available subcommands by calling `--help` and uses that information to construct correct commands.

Use `craby tools` or `/tools` in chat to see loaded tools and their status.

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

Craby is released under version 2.0 of the [Apache License](LICENSE).
