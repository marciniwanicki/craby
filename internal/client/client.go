package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/marciniwanicki/crabby/internal/api"
	"google.golang.org/protobuf/proto"
)

// ANSI color codes
const (
	colorReset     = "\033[0m"
	colorYellow    = "\033[33m"
	colorWhiteBold = "\033[1;37m"
	colorWhite     = "\033[37m"
)

// Verbosity levels
type Verbosity int

const (
	VerbosityNormal  Verbosity = iota // Show text + minimal tool info
	VerbosityQuiet                    // Only show assistant text
	VerbosityVerbose                  // Show everything including tool details
)

// Client handles communication with the daemon
type Client struct {
	baseURL string
	wsURL   string
}

// NewClient creates a new client
func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://localhost:%d", port),
		wsURL:   fmt.Sprintf("ws://localhost:%d", port),
	}
}

// ChatOptions configures chat behavior
type ChatOptions struct {
	Verbosity Verbosity
}

// Chat sends a message and streams the response to the provided writer
func (c *Client) Chat(ctx context.Context, message string, output io.Writer, opts ChatOptions) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL+"/ws/chat", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	// Send request
	req := &api.ChatRequest{
		Message: message,
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Read streaming response
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, respData, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			return fmt.Errorf("failed to read response: %w", err)
		}

		var resp api.ChatResponse
		if err := proto.Unmarshal(respData, &resp); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		switch payload := resp.Payload.(type) {
		case *api.ChatResponse_Text:
			// Always show assistant text
			if payload.Text.Role == api.Role_ASSISTANT {
				fmt.Fprint(output, payload.Text.Content)
			} else if opts.Verbosity == VerbosityVerbose {
				// Show system messages only in verbose mode
				fmt.Fprint(output, payload.Text.Content)
			}

		case *api.ChatResponse_ToolCall:
			if opts.Verbosity != VerbosityQuiet {
				fmt.Fprint(output, formatToolCall(payload.ToolCall.Name, payload.ToolCall.Arguments))
			}

		case *api.ChatResponse_ToolResult:
			if opts.Verbosity == VerbosityVerbose {
				status := "✓"
				if !payload.ToolResult.Success {
					status = "✗"
				}
				// Truncate long output
				out := payload.ToolResult.Output
				if len(out) > 200 {
					out = out[:200] + "..."
				}
				fmt.Fprintf(output, "%s %s\n", status, out)
			}

		case *api.ChatResponse_Done:
			fmt.Fprintln(output)
			return nil

		case *api.ChatResponse_Error:
			return fmt.Errorf("server error: %s", payload.Error)
		}
	}
}

// Status checks the daemon status
func (c *Client) Status(ctx context.Context) (*api.StatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/status", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var status api.StatusResponse
	if err := proto.Unmarshal(data, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// IsRunning checks if the daemon is running
func (c *Client) IsRunning(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Shutdown requests the daemon to stop
func (c *Client) Shutdown(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/shutdown", nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	return nil
}

// formatToolCall formats a tool call for display
func formatToolCall(name, arguments string) string {
	// Capitalize tool name
	displayName := strings.ToUpper(name[:1]) + name[1:]

	// Parse arguments to extract relevant info for shell tool
	var args map[string]any
	if name == "shell" && json.Unmarshal([]byte(arguments), &args) == nil {
		if cmd, ok := args["command"].(string); ok {
			return fmt.Sprintf("\n%s⚡%s %s%s%s(%s%s%s)\n\n",
				colorYellow, colorReset,
				colorWhiteBold, displayName, colorReset,
				colorWhite, cmd, colorReset)
		}
	}

	// Default format for other tools
	return fmt.Sprintf("\n%s⚡%s %s%s%s(%s%s%s)\n\n",
		colorYellow, colorReset,
		colorWhiteBold, displayName, colorReset,
		colorWhite, arguments, colorReset)
}
