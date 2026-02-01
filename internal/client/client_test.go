package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestFormatToolCall_ShellTool(t *testing.T) {
	result := formatToolCall("shell", `{"command":"date"}`)

	// Should contain the lightning bolt
	if !strings.Contains(result, "⚡") {
		t.Error("expected lightning bolt icon")
	}

	// Should contain capitalized tool name
	if !strings.Contains(result, "Shell") {
		t.Error("expected capitalized tool name 'Shell'")
	}

	// Should contain the command - check for "date" wrapped in parentheses with color codes
	if !strings.Contains(result, "date") {
		t.Error("expected command 'date' in output")
	}

	// Should NOT contain raw JSON
	if strings.Contains(result, `"command"`) {
		t.Error("should not contain raw JSON for shell tool")
	}
}

func TestFormatToolCall_ShellToolWithArgs(t *testing.T) {
	result := formatToolCall("shell", `{"command":"echo hello world"}`)

	// Check that the command is present (color codes may be interspersed)
	if !strings.Contains(result, "echo hello world") {
		t.Errorf("expected command 'echo hello world' in output, got: %s", result)
	}
}

func TestFormatToolCall_OtherTool(t *testing.T) {
	result := formatToolCall("other_tool", `{"param":"value"}`)

	// Should contain the lightning bolt
	if !strings.Contains(result, "⚡") {
		t.Error("expected lightning bolt icon")
	}

	// Should contain capitalized tool name
	if !strings.Contains(result, "Other_tool") {
		t.Error("expected capitalized tool name")
	}

	// Should contain raw arguments for non-shell tools
	if !strings.Contains(result, `{"param":"value"}`) {
		t.Error("expected raw JSON arguments for non-shell tool")
	}
}

func TestFormatToolCall_InvalidJSON(t *testing.T) {
	result := formatToolCall("shell", "invalid json")

	// Should still format without crashing
	if !strings.Contains(result, "⚡") {
		t.Error("expected lightning bolt icon")
	}

	// Should fall back to showing raw arguments
	if !strings.Contains(result, "invalid json") {
		t.Error("expected raw arguments on JSON parse failure")
	}
}

func TestFormatToolCall_ColorCodes(t *testing.T) {
	result := formatToolCall("shell", `{"command":"date"}`)

	// Should contain ANSI color codes
	if !strings.Contains(result, "\033[") {
		t.Error("expected ANSI color codes")
	}

	// Should contain reset code
	if !strings.Contains(result, colorReset) {
		t.Error("expected color reset code")
	}

	// Should contain light yellow for icon
	if !strings.Contains(result, colorLightYellow) {
		t.Error("expected light yellow color code")
	}

	// Should contain white bold for tool name
	if !strings.Contains(result, colorWhiteBold) {
		t.Error("expected white bold color code")
	}
}

func TestFormatToolCall_NewLines(t *testing.T) {
	result := formatToolCall("shell", `{"command":"date"}`)

	// Should end with double newline for spacing
	if !strings.HasSuffix(result, "\n\n") {
		t.Error("expected trailing double newline")
	}
}

func TestFormatToolCall_EmptyCommand(t *testing.T) {
	result := formatToolCall("shell", `{"command":""}`)

	// Should handle empty command gracefully - check for Shell and parentheses
	if !strings.Contains(result, "Shell") {
		t.Error("expected tool name 'Shell'")
	}
	// The parentheses will have color codes between them
	if !strings.Contains(result, "(") || !strings.Contains(result, ")") {
		t.Error("expected parentheses in output")
	}
}

func TestFormatToolCall_MissingCommand(t *testing.T) {
	result := formatToolCall("shell", `{"other":"value"}`)

	// Should fall back to raw JSON when command field is missing
	if !strings.Contains(result, `{"other":"value"}`) {
		t.Error("expected raw JSON when command field is missing")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient(8787)

	if client == nil {
		t.Fatal("expected client to be created")
	}

	if client.baseURL != "http://localhost:8787" {
		t.Errorf("expected baseURL 'http://localhost:8787', got %q", client.baseURL)
	}

	if client.wsURL != "ws://localhost:8787" {
		t.Errorf("expected wsURL 'ws://localhost:8787', got %q", client.wsURL)
	}
}

func TestNewClient_DifferentPort(t *testing.T) {
	client := NewClient(9000)

	if client.baseURL != "http://localhost:9000" {
		t.Errorf("expected baseURL 'http://localhost:9000', got %q", client.baseURL)
	}
}

func TestVerbosityConstants(t *testing.T) {
	// Verify verbosity levels are distinct
	if VerbosityNormal == VerbosityQuiet {
		t.Error("VerbosityNormal should not equal VerbosityQuiet")
	}
	if VerbosityNormal == VerbosityVerbose {
		t.Error("VerbosityNormal should not equal VerbosityVerbose")
	}
	if VerbosityQuiet == VerbosityVerbose {
		t.Error("VerbosityQuiet should not equal VerbosityVerbose")
	}
}

func TestIsRunning_DaemonRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	client := NewClient(port)

	if !client.IsRunning(context.Background()) {
		t.Error("expected IsRunning to return true when daemon responds with 200")
	}
}

func TestIsRunning_DaemonNotRunning(t *testing.T) {
	// Use a port that's definitely not listening
	client := NewClient(59999)

	if client.IsRunning(context.Background()) {
		t.Error("expected IsRunning to return false when daemon is not running")
	}
}

func TestIsRunning_DaemonReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	client := NewClient(port)

	if client.IsRunning(context.Background()) {
		t.Error("expected IsRunning to return false when daemon returns 500")
	}
}

func TestShutdown_Success(t *testing.T) {
	shutdownCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" && r.Method == http.MethodPost {
			shutdownCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	client := NewClient(port)

	err := client.Shutdown(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if !shutdownCalled {
		t.Error("expected shutdown endpoint to be called")
	}
}

func TestShutdown_DaemonNotRunning(t *testing.T) {
	// Use a port that's definitely not listening
	client := NewClient(59999)

	err := client.Shutdown(context.Background())
	if err == nil {
		t.Error("expected error when daemon is not running")
	}

	if !strings.Contains(err.Error(), "failed to connect to daemon") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestShutdown_DaemonReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	client := NewClient(port)

	err := client.Shutdown(context.Background())
	if err == nil {
		t.Error("expected error when daemon returns 500")
	}

	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status error, got: %v", err)
	}
}

func TestShutdown_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response - won't matter since context is canceled
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	port := extractPort(t, server.URL)
	client := NewClient(port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Shutdown(ctx)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestRenderMarkdown_Bold(t *testing.T) {
	input := "This is **bold** text"
	result := renderMarkdown(input)

	// Should contain the word "bold" without raw markers
	if !strings.Contains(result, "bold") {
		t.Error("expected 'bold' text in output")
	}
	if strings.Contains(result, "**") {
		t.Error("should not contain raw ** markers")
	}
}

func TestRenderMarkdown_InlineCode(t *testing.T) {
	input := "Run `go test` command"
	result := renderMarkdown(input)

	if !strings.Contains(result, "go test") {
		t.Error("expected code content in output")
	}
}

func TestRenderMarkdown_Header(t *testing.T) {
	input := "# Main Header"
	result := renderMarkdown(input)

	// Glamour adds ANSI codes, just check key words are present
	if !strings.Contains(result, "Main") || !strings.Contains(result, "Header") {
		t.Errorf("expected header text in output, got %q", result)
	}
}

func TestRenderMarkdown_PlainText(t *testing.T) {
	input := "Just plain text"
	result := renderMarkdown(input)

	// Glamour may add formatting, check key words are present
	if !strings.Contains(result, "Just") || !strings.Contains(result, "plain") || !strings.Contains(result, "text") {
		t.Errorf("expected plain text words in output, got %q", result)
	}
}

func TestRenderMarkdown_Link(t *testing.T) {
	input := "Check out [Google](https://google.com) for more"
	result := renderMarkdown(input)

	// Glamour renders links - should contain the URL
	if !strings.Contains(result, "google.com") {
		t.Errorf("expected URL in output, got: %q", result)
	}
}

func TestMarkdownStreamer_Buffering(t *testing.T) {
	var buf strings.Builder
	ms := newMarkdownStreamer(&buf)

	// Write chunks
	ms.Write("Hello ")
	ms.Write("**world**")
	ms.Flush()

	result := buf.String()
	if !strings.Contains(result, "world") {
		t.Error("expected 'world' in output")
	}
	if strings.Contains(result, "**") {
		t.Error("should not contain raw ** markers")
	}
}

func TestMarkdownStreamer_EmptyFlush(t *testing.T) {
	var buf strings.Builder
	ms := newMarkdownStreamer(&buf)

	// Flush without writing anything
	ms.Flush()

	if buf.Len() != 0 {
		t.Error("expected empty output for empty flush")
	}
}

// extractPort extracts the port number from an httptest server URL
func extractPort(t *testing.T, url string) int {
	t.Helper()
	// URL format: http://127.0.0.1:PORT
	parts := strings.Split(url, ":")
	if len(parts) < 3 {
		t.Fatalf("unexpected URL format: %s", url)
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("failed to parse port from URL %s: %v", url, err)
	}
	return port
}
