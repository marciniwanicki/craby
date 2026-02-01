package tools

import (
	"context"
	"testing"

	"github.com/marciniwanicki/craby/internal/config"
)

func TestListCommandsTool_Name(t *testing.T) {
	tool := NewListCommandsTool(config.DefaultSettings(), nil, nil)
	if tool.Name() != "list_available_commands" {
		t.Errorf("expected 'list_available_commands', got '%s'", tool.Name())
	}
}

func TestListCommandsTool_Description(t *testing.T) {
	tool := NewListCommandsTool(config.DefaultSettings(), nil, nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestListCommandsTool_Execute_All(t *testing.T) {
	settings := config.DefaultSettings()
	tool := NewListCommandsTool(settings, nil, nil)

	result, err := tool.Execute(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain allowlist section
	if !contains(result, "Shell Allowlist") {
		t.Error("expected result to contain 'Shell Allowlist'")
	}
}

func TestListCommandsTool_Execute_WithCategory(t *testing.T) {
	settings := config.DefaultSettings()
	tool := NewListCommandsTool(settings, nil, nil)

	result, err := tool.Execute(map[string]any{"category": "allowlist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Shell Allowlist") {
		t.Error("expected result to contain 'Shell Allowlist'")
	}
}

func TestListCommandsTool_Execute_WithExternalTools(t *testing.T) {
	settings := config.DefaultSettings()
	externalTools := []*config.ExternalTool{
		{
			Name:        "test-tool",
			Description: "A test tool",
			Access: config.ToolAccess{
				Type:    "shell",
				Command: "test-cmd",
			},
		},
	}
	tool := NewListCommandsTool(settings, externalTools, nil)

	result, err := tool.Execute(map[string]any{"category": "external"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "test-cmd") {
		t.Error("expected result to contain external tool command")
	}
}

func TestGetCommandSchemaTool_Name(t *testing.T) {
	tool := NewGetCommandSchemaTool(config.DefaultSettings(), nil, nil)
	if tool.Name() != "get_command_schema" {
		t.Errorf("expected 'get_command_schema', got '%s'", tool.Name())
	}
}

func TestGetCommandSchemaTool_Description(t *testing.T) {
	tool := NewGetCommandSchemaTool(config.DefaultSettings(), nil, nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestGetCommandSchemaTool_Execute_MissingCommand(t *testing.T) {
	tool := NewGetCommandSchemaTool(config.DefaultSettings(), nil, nil)

	_, err := tool.Execute(map[string]any{})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestGetCommandSchemaTool_Execute_DisallowedCommand(t *testing.T) {
	tool := NewGetCommandSchemaTool(config.DefaultSettings(), nil, nil)

	_, err := tool.Execute(map[string]any{"command": "rm"})
	if err == nil {
		t.Error("expected error for disallowed command")
	}
}

func TestGetCommandSchemaTool_Execute_AllowedCommand_NoLLM(t *testing.T) {
	settings := config.DefaultSettings()
	tool := NewGetCommandSchemaTool(settings, nil, nil)

	// Without LLM, should return raw help
	result, err := tool.Execute(map[string]any{"command": "ls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain some help content (or error about no LLM)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// MockLLM for testing schema generation
type mockSchemaLLM struct {
	response string
	err      error
}

func (m *mockSchemaLLM) SimpleChat(_ context.Context, _, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestGetCommandSchemaTool_Execute_WithMockLLM(t *testing.T) {
	settings := config.DefaultSettings()
	mockLLM := &mockSchemaLLM{
		response: `{
			"name": "ls",
			"description": "List directory contents",
			"flags": [
				{"name": "-l", "description": "Long format"}
			],
			"examples": ["ls -la"]
		}`,
	}
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{"command": "ls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain schema info
	if !contains(result, "ls") {
		t.Error("expected result to contain command name")
	}
}

func TestGetCommandSchemaTool_isCommandAllowed(t *testing.T) {
	settings := config.DefaultSettings()
	tool := NewGetCommandSchemaTool(settings, nil, nil)

	tests := []struct {
		command string
		allowed bool
	}{
		{"ls", true},      // In default allowlist
		{"git", true},     // In safe commands
		{"docker", true},  // In safe commands
		{"kubectl", true}, // In safe commands
		{"rm", false},     // Not in allowlist or safe commands
		{"malicious", false},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			result := tool.isCommandAllowed(tc.command)
			if result != tc.allowed {
				t.Errorf("isCommandAllowed(%s) = %v, want %v", tc.command, result, tc.allowed)
			}
		})
	}
}

// =============================================================================
// TFL CLI Tests - Tests for get_command_schema using the tfl CLI as an example
// =============================================================================

// tflMainHelpSchema is a mock LLM response for "tfl --help"
const tflMainHelpSchema = `{
	"name": "tfl",
	"description": "A command-line interface for Transport for London services",
	"subcommands": [
		{"name": "check", "description": "Check if API key is configured and valid"},
		{"name": "completion", "description": "Generate the autocompletion script for the specified shell"},
		{"name": "departures", "description": "Show departures from a station"},
		{"name": "disruptions", "description": "Show service disruptions"},
		{"name": "search", "description": "Search for stations"},
		{"name": "status", "description": "Show tube line status"}
	],
	"flags": [
		{"name": "--format", "short": "", "description": "Output format: text or json", "type": "string", "required": false},
		{"name": "--help", "short": "-h", "description": "Help for tfl", "type": "boolean", "required": false},
		{"name": "--key", "short": "", "description": "TfL API key (or set TFL_APP_KEY env var)", "type": "string", "required": false}
	],
	"arguments": [],
	"examples": [
		"tfl status",
		"tfl departures \"liverpool street\"",
		"tfl search paddington"
	]
}`

// tflDeparturesHelpSchema is a mock LLM response for "tfl departures --help"
const tflDeparturesHelpSchema = `{
	"name": "tfl departures",
	"description": "Show upcoming departures from a station",
	"subcommands": [],
	"flags": [
		{"name": "--help", "short": "-h", "description": "Help for departures", "type": "boolean", "required": false},
		{"name": "--limit", "short": "-n", "description": "Maximum number of departures to show", "type": "number", "required": false},
		{"name": "--match", "short": "-m", "description": "Fuzzy filter by line name and/or destination", "type": "string", "required": false},
		{"name": "--time", "short": "-t", "description": "Show departures at or after this time (HH:MM)", "type": "string", "required": false},
		{"name": "--format", "short": "", "description": "Output format: text or json", "type": "string", "required": false},
		{"name": "--key", "short": "", "description": "TfL API key (or set TFL_APP_KEY env var)", "type": "string", "required": false}
	],
	"arguments": [
		{"name": "station", "description": "The station name to get departures from", "required": true},
		{"name": "line", "description": "Optional line filter", "required": false}
	],
	"examples": [
		"tfl departures \"liverpool street\"",
		"tfl departures \"liverpool street\" elizabeth",
		"tfl departures paddington -n 5",
		"tfl departures paddington --time 14:30"
	]
}`

// settingsWithTFL creates test settings that allow the tfl command
func settingsWithTFL() *config.Settings {
	settings := config.DefaultSettings()
	settings.Tools.Shell.Allowlist = append(settings.Tools.Shell.Allowlist, "tfl")
	return settings
}

// mockTFLSchemaLLM returns different schemas based on the help text content
type mockTFLSchemaLLM struct {
	callCount int
	responses map[string]string
}

func newMockTFLSchemaLLM() *mockTFLSchemaLLM {
	return &mockTFLSchemaLLM{
		responses: map[string]string{
			"tfl":            tflMainHelpSchema,
			"tfl departures": tflDeparturesHelpSchema,
		},
	}
}

func (m *mockTFLSchemaLLM) SimpleChat(_ context.Context, _, userMessage string) (string, error) {
	m.callCount++

	// Determine which schema to return based on the command being documented
	// Check for "tfl departures" at the start of the help text request (after the backtick)
	if contains(userMessage, "for `tfl departures`") {
		return m.responses["tfl departures"], nil
	}
	return m.responses["tfl"], nil
}

func TestGetCommandSchemaTool_TFL_MainCommand(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{"command": "tfl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the result contains expected schema elements
	expectedElements := []string{
		"tfl",
		"Transport for London",
		"departures",
		"status",
		"--format",
		"--key",
	}

	for _, elem := range expectedElements {
		if !contains(result, elem) {
			t.Errorf("expected result to contain %q", elem)
		}
	}
}

func TestGetCommandSchemaTool_TFL_Subcommand(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	// Using the simplified single command argument format
	result, err := tool.Execute(map[string]any{
		"command": "tfl departures",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the result contains departures-specific elements
	expectedElements := []string{
		"departures",
		"station",
		"--limit",
		"--match",
		"--time",
		"liverpool street",
	}

	for _, elem := range expectedElements {
		if !contains(result, elem) {
			t.Errorf("expected result to contain %q", elem)
		}
	}
}

func TestGetCommandSchemaTool_TFL_WithCache(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()

	// Test without cache to verify LLM is called each time
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	// First call - should use LLM
	_, err := tool.Execute(map[string]any{"command": "tfl"})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	firstCallCount := mockLLM.callCount

	if firstCallCount != 1 {
		t.Errorf("expected 1 LLM call on first request, got %d", firstCallCount)
	}

	// Second call without cache - should call LLM again
	_, err = tool.Execute(map[string]any{"command": "tfl"})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if mockLLM.callCount != 2 {
		t.Errorf("expected 2 LLM calls without cache, got %d", mockLLM.callCount)
	}
}

// Note: Cache-related tests removed - caching is disabled during development

func TestGetCommandSchemaTool_TFL_FormatsSubcommands(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{"command": "tfl"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify subcommands are formatted properly
	subcommands := []string{"check", "departures", "disruptions", "search", "status"}
	for _, sub := range subcommands {
		// Should contain subcommand in the formatted output
		if !contains(result, sub) {
			t.Errorf("expected formatted output to contain subcommand %q", sub)
		}
	}
}

func TestGetCommandSchemaTool_TFL_FormatsFlags(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{
		"command": "tfl departures",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify flags are formatted with both long and short forms
	expectedFlags := []struct {
		long  string
		short string
	}{
		{"--limit", "-n"},
		{"--match", "-m"},
		{"--time", "-t"},
	}

	for _, flag := range expectedFlags {
		if !contains(result, flag.long) {
			t.Errorf("expected formatted output to contain flag %q", flag.long)
		}
		if !contains(result, flag.short) {
			t.Errorf("expected formatted output to contain short flag %q", flag.short)
		}
	}
}

func TestGetCommandSchemaTool_TFL_FormatsArguments(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{
		"command": "tfl departures",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify arguments section exists
	if !contains(result, "Arguments") {
		t.Error("expected formatted output to contain Arguments section")
	}

	// Verify required argument is marked
	if !contains(result, "station") {
		t.Error("expected formatted output to contain 'station' argument")
	}
}

func TestGetCommandSchemaTool_TFL_FormatsExamples(t *testing.T) {
	settings := settingsWithTFL()
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	result, err := tool.Execute(map[string]any{
		"command": "tfl departures",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify examples section exists
	if !contains(result, "Examples") {
		t.Error("expected formatted output to contain Examples section")
	}

	// Verify specific examples
	expectedExamples := []string{
		"liverpool street",
		"-n 5",
		"--time 14:30",
	}

	for _, ex := range expectedExamples {
		if !contains(result, ex) {
			t.Errorf("expected formatted output to contain example %q", ex)
		}
	}
}

func TestGetCommandSchemaTool_TFL_NotInAllowlist(t *testing.T) {
	// Use default settings without tfl in allowlist
	settings := config.DefaultSettings()
	tool := NewGetCommandSchemaTool(settings, nil, nil)

	_, err := tool.Execute(map[string]any{"command": "tfl"})
	if err == nil {
		t.Error("expected error when tfl is not in allowlist")
	}

	if !contains(err.Error(), "not in allowlist") {
		t.Errorf("expected 'not in allowlist' error, got: %v", err)
	}
}

func TestGetCommandSchemaTool_TFL_AsExternalTool(t *testing.T) {
	settings := config.DefaultSettings()

	// Add tfl as an external tool instead of in allowlist
	externalTools := []*config.ExternalTool{
		{
			Name:        "tfl",
			Description: "Transport for London CLI",
			Access: config.ToolAccess{
				Type:    "shell",
				Command: "tfl",
			},
		},
	}

	// Create tool that checks external tools
	mockLLM := newMockTFLSchemaLLM()
	tool := NewGetCommandSchemaTool(settings, nil, mockLLM)

	// Should fail because tfl is not in allowlist (external tools don't auto-allow discovery)
	// This tests that we need to explicitly allow commands
	_, err := tool.Execute(map[string]any{"command": "tfl"})
	if err == nil {
		t.Error("expected error - external tools don't auto-allow discovery")
	}

	_ = externalTools // External tools would need separate handling
}

// Helper functions for TFL tests

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
