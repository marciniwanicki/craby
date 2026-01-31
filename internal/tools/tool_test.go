package tools

import "testing"

// mockTool is a simple tool for testing
type mockTool struct {
	name        string
	description string
	params      map[string]any
	execFunc    func(args map[string]any) (string, error)
}

func (m *mockTool) Name() string                                { return m.name }
func (m *mockTool) Description() string                         { return m.description }
func (m *mockTool) Parameters() map[string]any                  { return m.params }
func (m *mockTool) Execute(args map[string]any) (string, error) { return m.execFunc(args) }

func TestDefinition(t *testing.T) {
	tool := &mockTool{
		name:        "test_tool",
		description: "A test tool",
		params: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type": "string",
				},
			},
		},
	}

	def := Definition(tool)

	// Check structure
	if def["type"] != "function" {
		t.Errorf("expected type 'function', got %v", def["type"])
	}

	fn, ok := def["function"].(map[string]any)
	if !ok {
		t.Fatal("expected function to be a map")
	}

	if fn["name"] != "test_tool" {
		t.Errorf("expected name 'test_tool', got %v", fn["name"])
	}

	if fn["description"] != "A test tool" {
		t.Errorf("expected description 'A test tool', got %v", fn["description"])
	}

	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatal("expected parameters to be a map")
	}

	if params["type"] != "object" {
		t.Errorf("expected parameters type 'object', got %v", params["type"])
	}
}
