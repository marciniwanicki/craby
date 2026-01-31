package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/marciniwanicki/crabby/internal/tools"
	"github.com/rs/zerolog"
)

const maxToolIterations = 10

// EventType represents the type of event
type EventType int

const (
	EventText EventType = iota
	EventToolCall
	EventToolResult
)

// Role represents the message role
type Role int

const (
	RoleAssistant Role = iota
	RoleSystem
)

// Event represents a structured event from the agent
type Event struct {
	Type EventType

	// For EventText
	Text string
	Role Role

	// For EventToolCall and EventToolResult
	ToolID   string
	ToolName string

	// For EventToolCall
	ToolArgs string // JSON string

	// For EventToolResult
	ToolOutput  string
	ToolSuccess bool
}

// Message represents a chat message
type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
}

// ToolCall represents a tool call from the model
type ToolCall struct {
	ID       string
	Function FunctionCall
}

// FunctionCall represents the function details in a tool call
type FunctionCall struct {
	Name      string
	Arguments map[string]any
}

// ChatResult represents the result of a chat request
type ChatResult struct {
	Content   string
	ToolCalls []ToolCall
	Done      bool
}

// LLMClient is the interface for LLM communication
type LLMClient interface {
	ChatWithTools(ctx context.Context, messages []Message, tools []any, tokenChan chan<- string) (*ChatResult, error)
}

// Agent handles the LLM + tool execution loop
type Agent struct {
	llm          LLMClient
	registry     *tools.Registry
	logger       zerolog.Logger
	systemPrompt string
}

// NewAgent creates a new agent with the given system prompt
func NewAgent(llm LLMClient, registry *tools.Registry, logger zerolog.Logger, systemPrompt string) *Agent {
	return &Agent{
		llm:          llm,
		registry:     registry,
		logger:       logger,
		systemPrompt: systemPrompt,
	}
}

// SystemPrompt returns the base system prompt
func (a *Agent) SystemPrompt() string {
	return a.systemPrompt
}

// RunOptions contains optional parameters for the agent run
type RunOptions struct {
	History []Message
	Context string
}

// Run executes the agent loop with the given user message and options
// It streams events to eventChan and returns when complete
// Text is buffered and only streamed when it's the final answer (no tool calls)
// Tool calls are streamed immediately
// Returns the updated message history
func (a *Agent) Run(ctx context.Context, userMessage string, opts RunOptions, eventChan chan<- Event) ([]Message, error) {
	defer close(eventChan)

	// Build system prompt, optionally with context
	systemPrompt := a.systemPrompt
	if opts.Context != "" {
		systemPrompt = systemPrompt + "\n\n<context>\n" + opts.Context + "\n</context>"
	}

	// Build messages: system prompt + history + new user message
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, opts.History...)
	messages = append(messages, Message{Role: "user", Content: userMessage})

	toolDefMaps := a.registry.Definitions()
	toolDefs := make([]any, len(toolDefMaps))
	for i, def := range toolDefMaps {
		toolDefs[i] = def
	}

	for i := 0; i < maxToolIterations; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Create a token channel to collect streaming tokens
		tokenChan := make(chan string, 100)
		resultChan := make(chan *ChatResult, 1)
		errChan := make(chan error, 1)

		go func() {
			result, err := a.llm.ChatWithTools(ctx, messages, toolDefs, tokenChan)
			if err != nil {
				errChan <- err
				return
			}
			resultChan <- result
		}()

		// Buffer tokens - we'll only stream them if this is the final answer
		var bufferedTokens []string
		for token := range tokenChan {
			bufferedTokens = append(bufferedTokens, token)
		}

		// Check for errors
		select {
		case err := <-errChan:
			return nil, err
		case result := <-resultChan:
			// If no tool calls, this is the final answer - stream buffered content
			if len(result.ToolCalls) == 0 {
				for _, token := range bufferedTokens {
					eventChan <- Event{
						Type: EventText,
						Text: token,
						Role: RoleAssistant,
					}
				}
				// Add final assistant message and return history (excluding system prompt)
				messages = append(messages, Message{Role: "assistant", Content: result.Content})
				return messages[1:], nil // Skip system prompt
			}

			// Process tool calls - intermediate text is discarded
			a.logger.Debug().
				Int("count", len(result.ToolCalls)).
				Int("buffered_tokens", len(bufferedTokens)).
				Msg("processing tool calls, discarding intermediate text")

			// Add assistant message with tool calls
			messages = append(messages, Message{
				Role:      "assistant",
				Content:   result.Content,
				ToolCalls: result.ToolCalls,
			})

			// Execute each tool call and add results
			for _, tc := range result.ToolCalls {
				// Marshal arguments to JSON string
				argsJSON, _ := json.Marshal(tc.Function.Arguments)

				// Emit tool call event immediately
				eventChan <- Event{
					Type:     EventToolCall,
					ToolID:   tc.ID,
					ToolName: tc.Function.Name,
					ToolArgs: string(argsJSON),
				}

				a.logger.Info().
					Str("tool", tc.Function.Name).
					Interface("args", tc.Function.Arguments).
					Msg("executing tool")

				output, err := a.registry.Execute(tc.Function.Name, tc.Function.Arguments)
				success := err == nil
				if err != nil {
					a.logger.Warn().Err(err).Str("tool", tc.Function.Name).Msg("tool execution failed")
					output = fmt.Sprintf("Error: %v", err)
				}

				// Emit tool result event immediately
				eventChan <- Event{
					Type:        EventToolResult,
					ToolID:      tc.ID,
					ToolName:    tc.Function.Name,
					ToolOutput:  output,
					ToolSuccess: success,
				}

				a.logger.Debug().Str("tool", tc.Function.Name).Str("output", output).Msg("tool result")

				// Add tool result message
				messages = append(messages, Message{
					Role:    "tool",
					Content: output,
				})
			}
		}
	}

	return nil, fmt.Errorf("max tool iterations (%d) exceeded", maxToolIterations)
}
