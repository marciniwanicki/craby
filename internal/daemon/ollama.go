package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/marciniwanicki/craby/internal/agent"
	"github.com/marciniwanicki/craby/internal/config"
)

// OllamaClient handles communication with the Ollama API
type OllamaClient struct {
	baseURL       string
	model         string
	httpClient    *http.Client
	llmCallLogger *config.LLMCallLogger
}

// OllamaRequest represents a chat request to Ollama
type OllamaRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Tools    []any           `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

// OllamaMessage represents a message in the Ollama chat format
type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

// OllamaToolCall represents a tool call from the model
type OllamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Function OllamaFunctionCall `json:"function"`
}

// OllamaFunctionCall represents the function details in a tool call
type OllamaFunctionCall struct {
	Index     int            `json:"index,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// OllamaResponse represents a streaming response from Ollama
type OllamaResponse struct {
	Model     string        `json:"model"`
	Message   OllamaMessage `json:"message"`
	Done      bool          `json:"done"`
	Error     string        `json:"error,omitempty"`
	CreatedAt string        `json:"created_at"`
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(baseURL, model string, llmCallLogger *config.LLMCallLogger) *OllamaClient {
	return &OllamaClient{
		baseURL:       baseURL,
		model:         model,
		httpClient:    &http.Client{},
		llmCallLogger: llmCallLogger,
	}
}

// Chat sends a message to Ollama and streams the response
func (c *OllamaClient) Chat(ctx context.Context, message string, tokenChan chan<- string) error {
	startTime := time.Now()
	defer close(tokenChan)

	req := OllamaRequest{
		Model: c.model,
		Messages: []OllamaMessage{
			{Role: "user", Content: message},
		},
		Stream: true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var contentBuilder bytes.Buffer
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ollamaResp OllamaResponse
		if err := json.Unmarshal(line, &ollamaResp); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if ollamaResp.Error != "" {
			return fmt.Errorf("ollama error: %s", ollamaResp.Error)
		}

		if ollamaResp.Message.Content != "" {
			contentBuilder.WriteString(ollamaResp.Message.Content)
			tokenChan <- ollamaResp.Message.Content
		}

		if ollamaResp.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	// Log the LLM call
	agentMessages := []agent.Message{
		{Role: "user", Content: message},
	}
	c.logCall("chat", agentMessages, nil, &agent.ChatResult{Content: contentBuilder.String()}, "", startTime)

	return nil
}

// ChatWithTools sends messages with tools to Ollama and streams the response
// Implements agent.LLMClient interface
func (c *OllamaClient) ChatWithTools(ctx context.Context, messages []agent.Message, tools []any, tokenChan chan<- string) (*agent.ChatResult, error) {
	startTime := time.Now()

	// Close the token channel when done
	if tokenChan != nil {
		defer close(tokenChan)
	}
	// Convert agent messages to Ollama messages
	ollamaMessages := make([]OllamaMessage, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = OllamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			ollamaMessages[i].ToolCalls = make([]OllamaToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				ollamaMessages[i].ToolCalls[j] = OllamaToolCall{
					Function: OllamaFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
	}

	req := OllamaRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Tools:    tools,
		Stream:   true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	result := &agent.ChatResult{}
	var contentBuilder bytes.Buffer

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ollamaResp OllamaResponse
		if err := json.Unmarshal(line, &ollamaResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if ollamaResp.Error != "" {
			return nil, fmt.Errorf("ollama error: %s", ollamaResp.Error)
		}

		// Accumulate content
		if ollamaResp.Message.Content != "" {
			contentBuilder.WriteString(ollamaResp.Message.Content)
			if tokenChan != nil {
				tokenChan <- ollamaResp.Message.Content
			}
		}

		// Collect tool calls and convert to agent format
		if len(ollamaResp.Message.ToolCalls) > 0 {
			for _, tc := range ollamaResp.Message.ToolCalls {
				result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
					ID: tc.ID,
					Function: agent.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}

		if ollamaResp.Done {
			result.Done = true
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	result.Content = contentBuilder.String()

	// Log the LLM call
	c.logCall("chat_with_tools", messages, tools, result, "", startTime)

	return result, nil
}

// Health checks if Ollama is healthy and the model is available
func (c *OllamaClient) Health(ctx context.Context) (bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return false, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// Model returns the configured model name
func (c *OllamaClient) Model() string {
	return c.model
}

// SimpleChat makes a simple chat completion call without tools.
// Implements tools.LLMClient interface for tool discovery.
func (c *OllamaClient) SimpleChat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	startTime := time.Now()

	messages := []OllamaMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}

	req := OllamaRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false, // Non-streaming for simplicity
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if ollamaResp.Error != "" {
		return "", fmt.Errorf("ollama error: %s", ollamaResp.Error)
	}

	// Log the LLM call
	agentMessages := []agent.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
	c.logCall("simple_chat", agentMessages, nil, &agent.ChatResult{Content: ollamaResp.Message.Content}, "", startTime)

	return ollamaResp.Message.Content, nil
}

// logCall logs an LLM call to a markdown file
func (c *OllamaClient) logCall(callType string, messages []agent.Message, tools []any, result *agent.ChatResult, errMsg string, startTime time.Time) {
	if c.llmCallLogger == nil {
		return
	}

	// Convert messages
	msgLogs := make([]config.LLMMessageLog, len(messages))
	for i, msg := range messages {
		msgLogs[i] = config.LLMMessageLog{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Extract tool names
	var toolNames []string
	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]any); ok {
			if fn, ok := toolMap["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					toolNames = append(toolNames, name)
				}
			}
		}
	}

	// Convert tool calls
	var toolCallLogs []config.LLMToolCallLog
	if result != nil {
		for _, tc := range result.ToolCalls {
			argsJSON, _ := json.MarshalIndent(tc.Function.Arguments, "", "  ")
			toolCallLogs = append(toolCallLogs, config.LLMToolCallLog{
				Name:      tc.Function.Name,
				Arguments: string(argsJSON),
			})
		}
	}

	response := ""
	if result != nil {
		response = result.Content
	}

	call := config.LLMCallLog{
		Type:       callType,
		Model:      c.model,
		Messages:   msgLogs,
		Tools:      toolNames,
		Response:   response,
		ToolCalls:  toolCallLogs,
		Error:      errMsg,
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	_ = c.llmCallLogger.Log(call)
}
