package client

import (
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

	// Should contain yellow for icon
	if !strings.Contains(result, colorYellow) {
		t.Error("expected yellow color code")
	}

	// Should contain white bold for tool name
	if !strings.Contains(result, colorWhiteBold) {
		t.Error("expected white bold color code")
	}
}

func TestFormatToolCall_NewLines(t *testing.T) {
	result := formatToolCall("shell", `{"command":"date"}`)

	// Should start with newline
	if !strings.HasPrefix(result, "\n") {
		t.Error("expected leading newline")
	}

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
