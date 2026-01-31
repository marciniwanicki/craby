package agent

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/marciniwanicki/crabby/internal/config"
	"github.com/marciniwanicki/crabby/internal/tools"
	"github.com/rs/zerolog"
)

// mockLLMClient is a mock implementation of LLMClient for testing
type mockLLMClient struct {
	responses []ChatResult
	callCount int
	messages  [][]Message
}

func (m *mockLLMClient) ChatWithTools(ctx context.Context, messages []Message, toolDefs []any, tokenChan chan<- string) (*ChatResult, error) {
	if tokenChan != nil {
		defer close(tokenChan)
	}

	// Store messages for inspection
	m.messages = append(m.messages, messages)

	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}

	resp := m.responses[m.callCount]
	m.callCount++

	// Stream tokens if content is present
	if tokenChan != nil && resp.Content != "" {
		tokenChan <- resp.Content
	}

	return &resp, nil
}

func testLogger() zerolog.Logger {
	return zerolog.New(os.Stderr).Level(zerolog.Disabled)
}

func TestNewAgent(t *testing.T) {
	llm := &mockLLMClient{}
	registry := tools.NewRegistry()
	logger := testLogger()

	agent := NewAgent(llm, registry, logger)

	if agent == nil {
		t.Fatal("expected agent to be created")
	}
}

func TestAgent_Run_SimpleResponse(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{Content: "Hello, world!", Done: true},
		},
	}
	registry := tools.NewRegistry()
	agent := NewAgent(llm, registry, testLogger())

	eventChan := make(chan Event, 10)

	err := agent.Run(context.Background(), "Hi", eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Should have at least one text event
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	found := false
	for _, e := range events {
		if e.Type == EventText && e.Text == "Hello, world!" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find text event with 'Hello, world!'")
	}
}

func TestAgent_Run_WithToolCall(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{
				Content: "Let me check the date.",
				ToolCalls: []ToolCall{
					{
						ID: "call_1",
						Function: FunctionCall{
							Name:      "test_tool",
							Arguments: map[string]any{"input": "test"},
						},
					},
				},
				Done: false,
			},
			{
				Content: "The tool returned: test result",
				Done:    true,
			},
		},
	}

	registry := tools.NewRegistry()
	// Register a test tool
	registry.Register(&testTool{
		name: "test_tool",
		execFunc: func(args map[string]any) (string, error) {
			return "test result", nil
		},
	})

	agent := NewAgent(llm, registry, testLogger())
	eventChan := make(chan Event, 20)

	err := agent.Run(context.Background(), "Call the tool", eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Check for tool call event
	foundToolCall := false
	foundToolResult := false
	for _, e := range events {
		if e.Type == EventToolCall && e.ToolName == "test_tool" {
			foundToolCall = true
		}
		if e.Type == EventToolResult && e.ToolSuccess {
			foundToolResult = true
		}
	}

	if !foundToolCall {
		t.Error("expected to find tool call event")
	}
	if !foundToolResult {
		t.Error("expected to find tool result event")
	}
}

func TestAgent_Run_ToolError(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{
				ToolCalls: []ToolCall{
					{
						ID: "call_1",
						Function: FunctionCall{
							Name:      "failing_tool",
							Arguments: map[string]any{},
						},
					},
				},
				Done: false,
			},
			{
				Content: "The tool failed.",
				Done:    true,
			},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name: "failing_tool",
		execFunc: func(args map[string]any) (string, error) {
			return "", errors.New("tool execution failed")
		},
	})

	agent := NewAgent(llm, registry, testLogger())
	eventChan := make(chan Event, 20)

	err := agent.Run(context.Background(), "Call the failing tool", eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Check for failed tool result event
	foundFailedResult := false
	for _, e := range events {
		if e.Type == EventToolResult && !e.ToolSuccess {
			foundFailedResult = true
			break
		}
	}

	if !foundFailedResult {
		t.Error("expected to find failed tool result event")
	}
}

func TestAgent_Run_ContextCancellation(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{Content: "This should not complete", Done: true},
		},
	}

	registry := tools.NewRegistry()
	agent := NewAgent(llm, registry, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	eventChan := make(chan Event, 10)

	err := agent.Run(ctx, "Hi", eventChan)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestAgent_Run_MessagesIncludeSystemPrompt(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{Content: "Response", Done: true},
		},
	}

	registry := tools.NewRegistry()
	agent := NewAgent(llm, registry, testLogger())

	eventChan := make(chan Event, 10)
	_ = agent.Run(context.Background(), "User message", eventChan)

	// Drain events
	for range eventChan {
	}

	// Check that messages include system prompt
	if len(llm.messages) == 0 {
		t.Fatal("expected messages to be recorded")
	}

	msgs := llm.messages[0]
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages (system + user)")
	}

	if msgs[0].Role != "system" {
		t.Errorf("expected first message to be system, got %q", msgs[0].Role)
	}

	if msgs[1].Role != "user" {
		t.Errorf("expected second message to be user, got %q", msgs[1].Role)
	}

	if msgs[1].Content != "User message" {
		t.Errorf("expected user message content 'User message', got %q", msgs[1].Content)
	}
}

func TestAgent_Run_WithShellTool(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResult{
			{
				ToolCalls: []ToolCall{
					{
						ID: "call_1",
						Function: FunctionCall{
							Name:      "shell",
							Arguments: map[string]any{"command": "echo hello"},
						},
					},
				},
				Done: false,
			},
			{
				Content: "Done",
				Done:    true,
			},
		},
	}

	settings := &config.Settings{
		Tools: config.ToolsSettings{
			Shell: config.ShellSettings{
				Enabled:   true,
				Allowlist: []string{"echo"},
			},
		},
	}

	registry := tools.NewRegistry()
	registry.Register(tools.NewShellTool(settings))

	agent := NewAgent(llm, registry, testLogger())
	eventChan := make(chan Event, 20)

	err := agent.Run(context.Background(), "Run echo", eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Check for successful shell result
	foundShellResult := false
	for _, e := range events {
		if e.Type == EventToolResult && e.ToolName == "shell" && e.ToolSuccess {
			if e.ToolOutput == "" {
				t.Error("expected shell output")
			}
			foundShellResult = true
			break
		}
	}

	if !foundShellResult {
		t.Error("expected to find successful shell result event")
	}
}

// testTool is a simple tool implementation for testing
type testTool struct {
	name     string
	execFunc func(args map[string]any) (string, error)
}

func (t *testTool) Name() string        { return t.name }
func (t *testTool) Description() string { return "Test tool" }
func (t *testTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t *testTool) Execute(args map[string]any) (string, error) {
	return t.execFunc(args)
}
