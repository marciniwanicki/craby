package daemon

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/marciniwanicki/crabby/internal/agent"
	"github.com/marciniwanicki/crabby/internal/api"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// Handler manages WebSocket connections and message handling
type Handler struct {
	agent   *agent.Agent
	logger  zerolog.Logger
	history []agent.Message
	context string
}

// NewHandler creates a new handler
func NewHandler(agent *agent.Agent, logger zerolog.Logger) *Handler {
	return &Handler{
		agent:  agent,
		logger: logger,
	}
}

// History returns the current conversation history
func (h *Handler) History() []agent.Message {
	return h.history
}

// Context returns the current user-set context string
func (h *Handler) Context() string {
	return h.context
}

// FullContext returns the complete context (system prompt + user context)
func (h *Handler) FullContext() string {
	base := h.agent.SystemPrompt()
	if h.context == "" {
		return base
	}
	return base + "\n\n<context>\n" + h.context + "\n</context>"
}

// SetContext sets the context string
func (h *Handler) SetContext(ctx string) {
	h.context = ctx
}

// HandleChat processes a chat WebSocket connection
func (h *Handler) HandleChat(conn *websocket.Conn) {
	defer conn.Close()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			// Treat EOF, unexpected EOF, and normal close as clean disconnects
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
				errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
				h.logger.Debug().Msg("client disconnected")
			} else {
				h.logger.Error().Err(err).Msg("failed to read message")
			}
			return
		}

		if messageType != websocket.BinaryMessage {
			h.logger.Warn().Int("type", messageType).Msg("received non-binary message")
			continue
		}

		var req api.ChatRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			h.logger.Error().Err(err).Msg("failed to unmarshal request")
			h.sendError(conn, "invalid request format")
			continue
		}

		h.logger.Info().Str("message", req.Message).Msg("received chat request")

		if err := h.processChat(conn, req.Message); err != nil {
			h.logger.Error().Err(err).Msg("failed to process chat")
			h.sendError(conn, err.Error())
		}
	}
}

func (h *Handler) processChat(conn *websocket.Conn, message string) error {
	ctx := context.Background()
	eventChan := make(chan agent.Event, 100)

	opts := agent.RunOptions{
		History: h.history,
		Context: h.context,
	}

	resultChan := make(chan []agent.Message, 1)
	errChan := make(chan error, 1)
	go func() {
		history, err := h.agent.Run(ctx, message, opts, eventChan)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- history
	}()

	// Stream events to client
	for event := range eventChan {
		var resp *api.ChatResponse

		switch event.Type {
		case agent.EventText:
			role := api.Role_ASSISTANT
			if event.Role == agent.RoleSystem {
				role = api.Role_SYSTEM
			}
			resp = &api.ChatResponse{
				Payload: &api.ChatResponse_Text{
					Text: &api.TextChunk{
						Content: event.Text,
						Role:    role,
					},
				},
			}

		case agent.EventToolCall:
			resp = &api.ChatResponse{
				Payload: &api.ChatResponse_ToolCall{
					ToolCall: &api.ToolCall{
						Id:        event.ToolID,
						Name:      event.ToolName,
						Arguments: event.ToolArgs,
					},
				},
			}

		case agent.EventToolResult:
			resp = &api.ChatResponse{
				Payload: &api.ChatResponse_ToolResult{
					ToolResult: &api.ToolResult{
						Id:      event.ToolID,
						Name:    event.ToolName,
						Output:  event.ToolOutput,
						Success: event.ToolSuccess,
					},
				},
			}
		}

		if resp != nil {
			if err := h.sendResponse(conn, resp); err != nil {
				return err
			}
		}
	}

	// Check for errors or get updated history
	select {
	case err := <-errChan:
		return err
	case history := <-resultChan:
		h.history = history
	}

	// Send done signal
	resp := &api.ChatResponse{
		Payload: &api.ChatResponse_Done{Done: true},
	}
	return h.sendResponse(conn, resp)
}

func (h *Handler) sendResponse(conn *websocket.Conn, resp *api.ChatResponse) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func (h *Handler) sendError(conn *websocket.Conn, errMsg string) {
	resp := &api.ChatResponse{
		Payload: &api.ChatResponse_Error{Error: errMsg},
	}
	data, err := proto.Marshal(resp)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to marshal error response")
		return
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		h.logger.Error().Err(err).Msg("failed to send error response")
	}
}
