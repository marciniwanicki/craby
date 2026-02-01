package daemon

import (
	"testing"

	"github.com/marciniwanicki/craby/internal/agent"
	"github.com/marciniwanicki/craby/internal/tools"
	"github.com/rs/zerolog"
)

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestHandler_Context(t *testing.T) {
	registry := tools.NewRegistry()
	agnt := agent.NewAgent(nil, registry, testLogger(), "system prompt")
	handler := NewHandler(agnt, nil, testLogger())

	// Initially empty
	if got := handler.Context(); got != "" {
		t.Errorf("expected empty context, got %q", got)
	}

	// Set context
	handler.SetContext("custom context")
	if got := handler.Context(); got != "custom context" {
		t.Errorf("expected 'custom context', got %q", got)
	}

	// Clear context
	handler.SetContext("")
	if got := handler.Context(); got != "" {
		t.Errorf("expected empty context after clear, got %q", got)
	}
}

func TestHandler_FullContext(t *testing.T) {
	registry := tools.NewRegistry()
	agnt := agent.NewAgent(nil, registry, testLogger(), "system prompt")
	handler := NewHandler(agnt, nil, testLogger())

	// Without user context, should return just system prompt
	if got := handler.FullContext(); got != "system prompt" {
		t.Errorf("expected 'system prompt', got %q", got)
	}

	// With user context, should include it wrapped in tags
	handler.SetContext("user context")
	expected := "system prompt\n\n<context>\nuser context\n</context>"
	if got := handler.FullContext(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestHandler_History(t *testing.T) {
	registry := tools.NewRegistry()
	agnt := agent.NewAgent(nil, registry, testLogger(), "system prompt")
	handler := NewHandler(agnt, nil, testLogger())

	// Initially empty
	if got := handler.History(); len(got) != 0 {
		t.Errorf("expected empty history, got %d items", len(got))
	}
}
