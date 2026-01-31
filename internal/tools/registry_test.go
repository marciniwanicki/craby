package tools

import (
	"errors"
	"testing"
)

func newTestTool(name string, execFunc func(args map[string]any) (string, error)) *mockTool {
	return &mockTool{
		name:        name,
		description: "Test tool: " + name,
		params:      map[string]any{"type": "object"},
		execFunc:    execFunc,
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry := NewRegistry()

	tool := newTestTool("my_tool", func(args map[string]any) (string, error) {
		return "ok", nil
	})

	registry.Register(tool)

	// Get existing tool
	got, ok := registry.Get("my_tool")
	if !ok {
		t.Error("expected to find registered tool")
	}
	if got.Name() != "my_tool" {
		t.Errorf("expected tool name 'my_tool', got %q", got.Name())
	}

	// Get non-existing tool
	_, ok = registry.Get("nonexistent")
	if ok {
		t.Error("expected not to find non-registered tool")
	}
}

func TestRegistry_Execute(t *testing.T) {
	registry := NewRegistry()

	tool := newTestTool("echo_tool", func(args map[string]any) (string, error) {
		msg, _ := args["message"].(string)
		return "echo: " + msg, nil
	})

	registry.Register(tool)

	// Execute existing tool
	result, err := registry.Execute("echo_tool", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "echo: hello" {
		t.Errorf("expected 'echo: hello', got %q", result)
	}

	// Execute non-existing tool
	_, err = registry.Execute("nonexistent", nil)
	if err == nil {
		t.Error("expected error for non-existing tool")
	}
}

func TestRegistry_Execute_ToolError(t *testing.T) {
	registry := NewRegistry()

	expectedErr := errors.New("tool failed")
	tool := newTestTool("failing_tool", func(args map[string]any) (string, error) {
		return "", expectedErr
	})

	registry.Register(tool)

	_, err := registry.Execute("failing_tool", nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	// Empty registry
	if len(registry.List()) != 0 {
		t.Error("expected empty list for new registry")
	}

	// Add tools
	registry.Register(newTestTool("tool1", nil))
	registry.Register(newTestTool("tool2", nil))
	registry.Register(newTestTool("tool3", nil))

	tools := registry.List()
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestRegistry_Definitions(t *testing.T) {
	registry := NewRegistry()

	registry.Register(newTestTool("tool1", nil))
	registry.Register(newTestTool("tool2", nil))

	defs := registry.Definitions()
	if len(defs) != 2 {
		t.Errorf("expected 2 definitions, got %d", len(defs))
	}

	// Each definition should have the correct structure
	for _, def := range defs {
		if def["type"] != "function" {
			t.Error("expected definition type to be 'function'")
		}
		fn, ok := def["function"].(map[string]any)
		if !ok {
			t.Error("expected function to be a map")
		}
		if fn["name"] == nil {
			t.Error("expected function to have a name")
		}
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			tool := newTestTool("tool"+string(rune('0'+n)), nil)
			registry.Register(tool)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			registry.List()
			registry.Definitions()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
