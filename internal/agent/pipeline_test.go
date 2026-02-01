package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/marciniwanicki/craby/internal/tools"
	"github.com/rs/zerolog"
)

// mockPipelineLLMClient implements PipelineLLMClient for testing
type mockPipelineLLMClient struct {
	chatMessagesResponses []string
	chatMessagesCount     int
	messages              [][]Message
}

func (m *mockPipelineLLMClient) ChatWithTools(ctx context.Context, messages []Message, toolDefs []any, tokenChan chan<- string) (*ChatResult, error) {
	if tokenChan != nil {
		defer close(tokenChan)
	}
	return &ChatResult{Content: "not used in pipeline", Done: true}, nil
}

func (m *mockPipelineLLMClient) ChatMessages(ctx context.Context, messages []Message, tokenChan chan<- string) (string, error) {
	m.messages = append(m.messages, messages)

	if m.chatMessagesCount >= len(m.chatMessagesResponses) {
		if tokenChan != nil {
			close(tokenChan)
		}
		return "", errors.New("no more mock responses")
	}

	resp := m.chatMessagesResponses[m.chatMessagesCount]
	m.chatMessagesCount++

	// Stream tokens if channel provided
	if tokenChan != nil {
		tokenChan <- resp
		close(tokenChan)
	}

	return resp, nil
}

func pipelineTestLogger() zerolog.Logger {
	return zerolog.New(os.Stderr).Level(zerolog.Disabled)
}

func TestPipeline_SimpleResponse(t *testing.T) {
	// Simple query that needs no tools - plan immediately indicates ready_to_answer
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Planning response (no tools, ready to answer)
			`<plan>
  <intent>Answer a simple math question</intent>
  <complexity>simple</complexity>
  <needs_tools>false</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context>
    <item>Basic arithmetic</item>
  </context>
  <steps></steps>
</plan>`,
			// Synthesis response
			"The answer to 2+2 is 4.",
		},
	}

	registry := tools.NewRegistry()
	templates := PipelineTemplates{
		Planning:  "You are in planning mode. {{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "You are a helpful assistant.",
		User:      "User profile here.",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	history, err := pipeline.Run(context.Background(), "What is 2+2?", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Should have plan generated event and text events
	foundPlan := false
	foundText := false
	for _, e := range events {
		if e.Type == EventPlanGenerated {
			foundPlan = true
			if e.Plan == nil {
				t.Error("plan event should have plan")
			} else if e.Plan.Complexity != ComplexitySimple {
				t.Errorf("expected simple complexity, got %q", e.Plan.Complexity)
			}
		}
		if e.Type == EventText && strings.Contains(e.Text, "4") {
			foundText = true
		}
	}

	if !foundPlan {
		t.Error("expected plan generated event")
	}
	if !foundText {
		t.Error("expected text event with answer")
	}

	// History should have user message and assistant response
	if len(history) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(history))
	}

	if history[0].Role != "user" {
		t.Errorf("expected first message to be user, got %q", history[0].Role)
	}

	if history[1].Role != "assistant" {
		t.Errorf("expected second message to be assistant, got %q", history[1].Role)
	}
}

func TestPipeline_SingleTool(t *testing.T) {
	// Query that needs one tool call - iterative planning
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Iteration 1: Planning response with tool execution
			`<plan>
  <intent>Get current time</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context>
    <item>User wants the time</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>test_tool</tool>
      <purpose>Get time</purpose>
      <args>
        <arg name="input">time</arg>
      </args>
    </step>
  </steps>
</plan>`,
			// Iteration 2: Ready to answer after tool execution
			`<plan>
  <intent>Get current time</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context>
    <item>Have time result from tool</item>
  </context>
  <steps></steps>
</plan>`,
			// Synthesis response
			"The current time is 12:00.",
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name: "test_tool",
		execFunc: func(args map[string]any) (string, error) {
			return "12:00 PM", nil
		},
	})

	templates := PipelineTemplates{
		Planning:  "You are in planning mode. {{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "You are a helpful assistant.",
		User:      "User profile here.",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "What time is it?", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect events
	var events []Event
	for event := range eventChan {
		events = append(events, event)
	}

	// Should have tool call and result events
	foundToolCall := false
	foundToolResult := false
	planCount := 0
	for _, e := range events {
		if e.Type == EventPlanGenerated {
			planCount++
		}
		if e.Type == EventToolCall && e.ToolName == "test_tool" {
			foundToolCall = true
		}
		if e.Type == EventToolResult && e.ToolSuccess {
			foundToolResult = true
		}
	}

	if planCount != 2 {
		t.Errorf("expected 2 plan events (iterative), got %d", planCount)
	}
	if !foundToolCall {
		t.Error("expected tool call event")
	}
	if !foundToolResult {
		t.Error("expected tool result event")
	}
}

func TestPipeline_MultiStep(t *testing.T) {
	// Query that needs multiple tool calls in one iteration
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Iteration 1: Planning response with both tools
			`<plan>
  <intent>List and read file</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_1">
      <tool>list_tool</tool>
      <purpose>List files</purpose>
      <args></args>
    </step>
    <step id="step_2" depends_on="step_1">
      <tool>read_tool</tool>
      <purpose>Read first file</purpose>
      <args>
        <arg name="file">test.txt</arg>
      </args>
    </step>
  </steps>
</plan>`,
			// Iteration 2: Ready to answer
			`<plan>
  <intent>List and read file</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context></context>
  <steps></steps>
</plan>`,
			// Synthesis response
			"Found files and read contents.",
		},
	}

	registry := tools.NewRegistry()
	executionOrder := []string{}

	registry.Register(&testTool{
		name: "list_tool",
		execFunc: func(args map[string]any) (string, error) {
			executionOrder = append(executionOrder, "list_tool")
			return "test.txt", nil
		},
	})
	registry.Register(&testTool{
		name: "read_tool",
		execFunc: func(args map[string]any) (string, error) {
			executionOrder = append(executionOrder, "read_tool")
			return "file contents", nil
		},
	})

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "Assistant",
		User:      "User",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "List files and read first one", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain events
	for range eventChan {
	}

	// Verify execution order (step_1 before step_2)
	if len(executionOrder) != 2 {
		t.Fatalf("expected 2 tool executions, got %d", len(executionOrder))
	}

	if executionOrder[0] != "list_tool" {
		t.Errorf("expected list_tool first, got %q", executionOrder[0])
	}

	if executionOrder[1] != "read_tool" {
		t.Errorf("expected read_tool second, got %q", executionOrder[1])
	}
}

func TestPipeline_ValidationFailure(t *testing.T) {
	// Plan references non-existent tool
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Planning response with unknown tool
			`<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_1">
      <tool>nonexistent_tool</tool>
      <purpose>Do something</purpose>
      <args></args>
    </step>
  </steps>
</plan>`,
		},
	}

	registry := tools.NewRegistry() // Empty registry

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "Assistant",
		User:      "User",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Test", RunOptions{}, eventChan)
	if err == nil {
		t.Error("expected validation error for unknown tool")
	}

	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %v", err)
	}

	// Drain events
	for range eventChan {
	}
}

func TestPipeline_ToolExecutionError(t *testing.T) {
	// Tool returns error but pipeline continues
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Planning response
			`<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_1">
      <tool>failing_tool</tool>
      <purpose>Do something</purpose>
      <args></args>
    </step>
  </steps>
</plan>`,
			// Iteration 2: Ready to answer (even with error)
			`<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context></context>
  <steps></steps>
</plan>`,
			// Synthesis response (error should be reported)
			"The tool failed with an error.",
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name: "failing_tool",
		execFunc: func(args map[string]any) (string, error) {
			return "", errors.New("tool failed")
		},
	})

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "Assistant",
		User:      "User",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Test", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v (tool errors should be handled gracefully)", err)
	}

	// Collect events
	foundFailedResult := false
	for event := range eventChan {
		if event.Type == EventToolResult && !event.ToolSuccess {
			foundFailedResult = true
		}
	}

	if !foundFailedResult {
		t.Error("expected failed tool result event")
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	// This mock returns context.Canceled when context is already canceled
	llm := &cancelAwareMockLLM{}

	registry := tools.NewRegistry()
	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{HISTORY}} {{USER_HINTS}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "Assistant",
		User:      "User",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(ctx, "Test", RunOptions{}, eventChan)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// Drain events
	for range eventChan {
	}
}

// cancelAwareMockLLM returns context.Canceled when context is canceled
type cancelAwareMockLLM struct{}

func (m *cancelAwareMockLLM) ChatWithTools(ctx context.Context, messages []Message, toolDefs []any, tokenChan chan<- string) (*ChatResult, error) {
	if tokenChan != nil {
		close(tokenChan)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &ChatResult{Content: "", Done: true}, nil
}

func (m *cancelAwareMockLLM) ChatMessages(ctx context.Context, messages []Message, tokenChan chan<- string) (string, error) {
	if tokenChan != nil {
		close(tokenChan)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", nil
}

func TestPipeline_WithHistory(t *testing.T) {
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			`<plan>
  <intent>Test</intent>
  <complexity>simple</complexity>
  <needs_tools>false</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context></context>
  <steps></steps>
</plan>`,
			"Response based on history.",
		},
	}

	registry := tools.NewRegistry()
	templates := PipelineTemplates{
		Planning:  "History: {{HISTORY}} {{TOOL_RESULTS}}",
		Synthesis: "{{IDENTITY}} {{USER}} {{HISTORY}} {{TOOL_RESULTS}}",
		Identity:  "Assistant",
		User:      "User",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	opts := RunOptions{
		History: []Message{
			{Role: "user", Content: "Previous question"},
			{Role: "assistant", Content: "Previous answer"},
		},
	}

	history, err := pipeline.Run(context.Background(), "New question", opts, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain events
	for range eventChan {
	}

	// History should include old + new
	if len(history) != 4 {
		t.Fatalf("expected 4 messages in history, got %d", len(history))
	}

	if history[2].Content != "New question" {
		t.Errorf("expected 'New question', got %q", history[2].Content)
	}
}

func TestPipeline_ExecutionOrder_NoDependencies(t *testing.T) {
	// Steps with no dependencies should still execute
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			`<plan>
  <intent>Test</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_a">
      <tool>tool_a</tool>
      <purpose>A</purpose>
      <args></args>
    </step>
    <step id="step_b">
      <tool>tool_b</tool>
      <purpose>B</purpose>
      <args></args>
    </step>
  </steps>
</plan>`,
			`<plan>
  <intent>Test</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context></context>
  <steps></steps>
</plan>`,
			"Done.",
		},
	}

	registry := tools.NewRegistry()
	executionOrder := []string{}

	registry.Register(&testTool{
		name: "tool_a",
		execFunc: func(args map[string]any) (string, error) {
			executionOrder = append(executionOrder, "a")
			return "a", nil
		},
	})
	registry.Register(&testTool{
		name: "tool_b",
		execFunc: func(args map[string]any) (string, error) {
			executionOrder = append(executionOrder, "b")
			return "b", nil
		},
	})

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{TOOL_RESULTS}}",
		Synthesis: "{{TOOL_RESULTS}}",
		Identity:  "",
		User:      "",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Test", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain events
	for range eventChan {
	}

	// Both should execute (order may vary when no deps)
	if len(executionOrder) != 2 {
		t.Errorf("expected 2 executions, got %d", len(executionOrder))
	}
}

func TestPipeline_CircularDependency(t *testing.T) {
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			`<plan>
  <intent>Test</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_1" depends_on="step_2">
      <tool>test_tool</tool>
      <purpose>A</purpose>
      <args></args>
    </step>
    <step id="step_2" depends_on="step_1">
      <tool>test_tool</tool>
      <purpose>B</purpose>
      <args></args>
    </step>
  </steps>
</plan>`,
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name:     "test_tool",
		execFunc: func(args map[string]any) (string, error) { return "", nil },
	})

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{TOOL_RESULTS}}",
		Synthesis: "{{TOOL_RESULTS}}",
		Identity:  "",
		User:      "",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Test", RunOptions{}, eventChan)
	if err == nil {
		t.Error("expected error for circular dependency")
	}

	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("expected 'circular dependency' error, got: %v", err)
	}

	// Drain events
	for range eventChan {
	}
}

func TestPipeline_IterativePlanning(t *testing.T) {
	// Test that iterative planning works - multiple plan-execute cycles
	iteration := 0
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Iteration 1: Discover schema
			`<plan>
  <intent>Get tube status</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context>
    <item>Need to discover tfl command first</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>get_schema</tool>
      <purpose>Discover tfl subcommands</purpose>
      <args>
        <arg name="command">tfl</arg>
      </args>
    </step>
  </steps>
</plan>`,
			// Iteration 2: Now know the subcommand, discover its args
			`<plan>
  <intent>Get tube status</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context>
    <item>Found status subcommand, need to learn its arguments</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>get_schema</tool>
      <purpose>Learn tfl status arguments</purpose>
      <args>
        <arg name="command">tfl status</arg>
      </args>
    </step>
  </steps>
</plan>`,
			// Iteration 3: Execute the command
			`<plan>
  <intent>Get tube status</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context>
    <item>Know how to call tfl status</item>
  </context>
  <steps>
    <step id="step_1">
      <tool>shell</tool>
      <purpose>Get tube status</purpose>
      <args>
        <arg name="command">tfl status</arg>
      </args>
    </step>
  </steps>
</plan>`,
			// Iteration 4: Ready to answer
			`<plan>
  <intent>Get tube status</intent>
  <complexity>multi_step</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context>
    <item>Have tube status results</item>
  </context>
  <steps></steps>
</plan>`,
			// Synthesis
			"All tube lines are running normally.",
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name: "get_schema",
		execFunc: func(args map[string]any) (string, error) {
			iteration++
			if iteration == 1 {
				return `{"subcommands": ["status", "arrivals"]}`, nil
			}
			return `{"flags": ["--line"]}`, nil
		},
	})
	registry.Register(&testTool{
		name: "shell",
		execFunc: func(args map[string]any) (string, error) {
			return "All lines: Good Service", nil
		},
	})

	templates := PipelineTemplates{
		Planning:  "{{TOOLS}} {{TOOL_RESULTS}}",
		Synthesis: "{{TOOL_RESULTS}}",
		Identity:  "",
		User:      "",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Show tube status", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count events
	planCount := 0
	toolCallCount := 0
	for event := range eventChan {
		if event.Type == EventPlanGenerated {
			planCount++
		}
		if event.Type == EventToolCall {
			toolCallCount++
		}
	}

	// Should have 4 plan iterations
	if planCount != 4 {
		t.Errorf("expected 4 plan iterations, got %d", planCount)
	}

	// Should have 3 tool calls (2 schema + 1 shell)
	if toolCallCount != 3 {
		t.Errorf("expected 3 tool calls, got %d", toolCallCount)
	}
}

func TestPipeline_ToolResultsInPlanningPrompt(t *testing.T) {
	// Verify that tool results are passed to subsequent planning iterations
	llm := &mockPipelineLLMClient{
		chatMessagesResponses: []string{
			// Iteration 1
			`<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>false</ready_to_answer>
  <context></context>
  <steps>
    <step id="step_1">
      <tool>test_tool</tool>
      <purpose>Get data</purpose>
      <args></args>
    </step>
  </steps>
</plan>`,
			// Iteration 2 - should see tool results
			`<plan>
  <intent>Test</intent>
  <complexity>tool</complexity>
  <needs_tools>true</needs_tools>
  <ready_to_answer>true</ready_to_answer>
  <context></context>
  <steps></steps>
</plan>`,
			"Done.",
		},
	}

	registry := tools.NewRegistry()
	registry.Register(&testTool{
		name:     "test_tool",
		execFunc: func(args map[string]any) (string, error) { return "UNIQUE_TOOL_OUTPUT_12345", nil },
	})

	templates := PipelineTemplates{
		Planning:  "Results: {{TOOL_RESULTS}}",
		Synthesis: "{{TOOL_RESULTS}}",
		Identity:  "",
		User:      "",
	}

	pipeline := NewPipeline(llm, registry, pipelineTestLogger(), templates)
	eventChan := make(chan Event, 100)

	_, err := pipeline.Run(context.Background(), "Test", RunOptions{}, eventChan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain events
	for range eventChan {
	}

	// Check the captured messages from the mock
	if len(llm.messages) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(llm.messages))
	}

	// The second planning call should contain the tool output in the system prompt
	secondCallMessages := llm.messages[1]
	foundToolOutput := false
	for _, msg := range secondCallMessages {
		if msg.Role == "system" && strings.Contains(msg.Content, "UNIQUE_TOOL_OUTPUT_12345") {
			foundToolOutput = true
			break
		}
	}

	if !foundToolOutput {
		t.Error("second planning prompt should contain tool output from first iteration")
	}
}
