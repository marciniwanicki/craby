package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gorilla/websocket"
	"github.com/marciniwanicki/craby/internal/api"
	"google.golang.org/protobuf/proto"
)

// ANSI color codes
const (
	colorReset       = "\033[0m"
	colorLightYellow = "\033[93m"
	colorYellow      = "\033[33m"
	colorWhiteBold   = "\033[1;37m"
	colorWhite       = "\033[37m"
	colorGray        = "\033[90m"
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

// ANSI cursor control
const (
	cursorHide = "\033[?25l"
	cursorShow = "\033[?25h"
)

// spinner displays an animated spinner while waiting
type spinner struct {
	frames   []string
	interval time.Duration
	output   io.Writer
	stop     chan struct{}
	done     chan struct{}
	pause    chan chan struct{}
	resume   chan struct{}
	mu       sync.Mutex
	running  bool
	isPaused bool
	pausedMu sync.Mutex
}

func newSpinner(output io.Writer) *spinner {
	return &spinner{
		frames:   []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		interval: 80 * time.Millisecond,
		output:   output,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		pause:    make(chan chan struct{}),
		resume:   make(chan struct{}, 1),
	}
}

func (s *spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		defer close(s.done)
		fmt.Fprint(s.output, cursorHide) // Hide cursor
		i := 0
		paused := false
		for {
			select {
			case <-s.stop:
				if !paused {
					fmt.Fprint(s.output, "\r\033[K") // Clear the line
				}
				fmt.Fprint(s.output, cursorShow) // Show cursor
				return
			case ack := <-s.pause:
				if !paused {
					fmt.Fprint(s.output, "\r\033[K") // Clear the line
					paused = true
					s.pausedMu.Lock()
					s.isPaused = true
					s.pausedMu.Unlock()
				}
				close(ack) // Signal that pause is complete
			case <-s.resume:
				paused = false
				s.pausedMu.Lock()
				s.isPaused = false
				s.pausedMu.Unlock()
			default:
				if !paused {
					fmt.Fprintf(s.output, "\r%s%s %s(thinking…)%s", colorLightYellow, s.frames[i%len(s.frames)], colorGray, colorReset)
					i++
				}
				time.Sleep(s.interval)
			}
		}
	}()
}

func (s *spinner) Pause() {
	s.pausedMu.Lock()
	if s.isPaused {
		s.pausedMu.Unlock()
		return
	}
	s.pausedMu.Unlock()

	ack := make(chan struct{})
	s.pause <- ack
	<-ack // Wait for acknowledgment that line is cleared
}

func (s *spinner) Resume() {
	select {
	case s.resume <- struct{}{}:
	default:
	}
}

func (s *spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stop)
	<-s.done
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

	// Start spinner while waiting for response
	spin := newSpinner(output)
	spin.Start()
	spinnerStopped := false
	stopSpinner := func() {
		if !spinnerStopped {
			spin.Stop()
			spinnerStopped = true
		}
	}
	defer stopSpinner()

	// Markdown streamer for buffered rendering
	mdStream := newMarkdownStreamer(output)

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
			spin.Pause()
			// Always show assistant text
			if payload.Text.Role == api.Role_ASSISTANT {
				mdStream.Write(payload.Text.Content)
			} else if opts.Verbosity == VerbosityVerbose {
				// Show system messages only in verbose mode
				mdStream.Write(payload.Text.Content)
			}

		case *api.ChatResponse_ToolCall:
			spin.Pause()
			mdStream.Flush() // Flush before tool output
			if opts.Verbosity != VerbosityQuiet {
				fmt.Fprint(output, formatToolCall(payload.ToolCall.Name, payload.ToolCall.Arguments))
			}
			spin.Resume()

		case *api.ChatResponse_ToolResult:
			spin.Pause()
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
			spin.Resume()

		case *api.ChatResponse_ShellCommand:
			// Shell command output is now handled by ToolCall event
			// No need to print separately

		case *api.ChatResponse_Done:
			stopSpinner()
			mdStream.Flush() // Flush remaining content
			fmt.Fprintln(output)
			return nil

		case *api.ChatResponse_Error:
			stopSpinner()
			mdStream.Flush()
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

// GetContext retrieves the current context from the daemon
func (c *Client) GetContext(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/context", nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var contextResp api.ContextResponse
	if err := proto.Unmarshal(data, &contextResp); err != nil {
		return "", err
	}

	return contextResp.Context, nil
}

// SetContext sets the context on the daemon
func (c *Client) SetContext(ctx context.Context, context string) error {
	reqBody := &api.ContextRequest{Context: context}
	data, err := proto.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/context", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

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

// History retrieves the conversation history from the daemon
func (c *Client) History(ctx context.Context) (*api.HistoryResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/history", nil)
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

	var history api.HistoryResponse
	if err := proto.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	return &history, nil
}

// PrintHistory fetches and displays the conversation history
func (c *Client) PrintHistory(ctx context.Context) error {
	history, err := c.History(ctx)
	if err != nil {
		return err
	}

	if len(history.Messages) == 0 {
		fmt.Printf("%sNo conversation history yet.%s\n\n", colorGray, colorReset)
		return nil
	}

	for _, msg := range history.Messages {
		switch msg.Role {
		case api.Role_USER:
			fmt.Printf("%sUser:%s %s\n\n", colorYellow, colorReset, msg.Content)
		case api.Role_ASSISTANT:
			fmt.Printf("%sAssistant:%s %s\n\n", colorGray, colorReset, msg.Content)
		}
	}

	return nil
}

// ExecuteTool runs a tool directly with the given arguments
func (c *Client) ExecuteTool(ctx context.Context, name string, args map[string]any) (*api.ToolRunResponse, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal arguments: %w", err)
	}

	reqBody := &api.ToolRunRequest{
		Name:      name,
		Arguments: string(argsJSON),
	}
	data, err := proto.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/tool/run", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned status %d", resp.StatusCode)
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var toolResp api.ToolRunResponse
	if err := proto.Unmarshal(respData, &toolResp); err != nil {
		return nil, err
	}

	return &toolResp, nil
}

// ListTools returns all registered tools
func (c *Client) ListTools(ctx context.Context) (*api.ToolListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/tool/list", nil)
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

	var toolList api.ToolListResponse
	if err := proto.Unmarshal(data, &toolList); err != nil {
		return nil, err
	}

	return &toolList, nil
}

// formatToolCall formats a tool call for display
func formatToolCall(name, arguments string) string {
	// Format tool name: replace underscores with spaces and capitalize each word
	displayName := formatToolName(name)

	// Parse arguments
	var args map[string]any
	_ = json.Unmarshal([]byte(arguments), &args)

	// Special formatting for specific tools
	switch name {
	case "shell":
		// Don't show command for shell - it's redundant
		if cmd, ok := args["command"].(string); ok {
			return fmt.Sprintf("%s⚡%s%s%s%s(%s%s%s)\n",
				colorLightYellow, colorReset,
				colorWhiteBold, displayName, colorReset,
				colorWhite, cmd, colorReset)
		}

	case "get_command_schema":
		// Show just the command, not JSON
		if cmd, ok := args["command"].(string); ok {
			return fmt.Sprintf("%s⚡%s%s%s%s(%s%s%s)\n",
				colorLightYellow, colorReset,
				colorWhiteBold, displayName, colorReset,
				colorWhite, cmd, colorReset)
		}
	}

	// Default format for other tools
	return fmt.Sprintf("%s⚡%s%s%s%s(%s%s%s)\n",
		colorLightYellow, colorReset,
		colorWhiteBold, displayName, colorReset,
		colorWhite, arguments, colorReset)
}

// formatToolName converts a tool name like "get_command_schema" to "Get Command Schema"
func formatToolName(name string) string {
	// Replace underscores with spaces
	words := strings.Split(name, "_")

	// Capitalize each word
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// markdownRenderer handles glamour-based markdown rendering
var markdownRenderer *glamour.TermRenderer

func init() {
	var err error
	markdownRenderer, err = glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(0), // No word wrap, let terminal handle it
	)
	if err != nil {
		// Fallback: renderer will be nil, we'll output plain text
		markdownRenderer = nil
	}
}

// markdownStreamer buffers text for markdown rendering
type markdownStreamer struct {
	output io.Writer
	buffer strings.Builder
}

func newMarkdownStreamer(output io.Writer) *markdownStreamer {
	return &markdownStreamer{output: output}
}

// Write adds text to the buffer
func (m *markdownStreamer) Write(text string) {
	m.buffer.WriteString(text)
}

// Flush renders and outputs the buffered markdown
func (m *markdownStreamer) Flush() {
	if m.buffer.Len() == 0 {
		return
	}

	text := m.buffer.String()
	m.buffer.Reset()

	rendered := renderMarkdown(text)
	// Add newline before answer to separate from question
	fmt.Fprint(m.output, "\n"+rendered)
}

// renderMarkdown converts markdown to styled terminal output using glamour
func renderMarkdown(text string) string {
	if markdownRenderer == nil {
		return text
	}

	rendered, err := markdownRenderer.Render(text)
	if err != nil {
		return text
	}

	// Glamour adds extra newlines, trim them
	return strings.TrimSpace(rendered)
}
