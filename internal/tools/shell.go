package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/marciniwanicki/craby/internal/config"
)

const shellTimeout = 30 * time.Second

// CommandObserver is called when a shell command is executed
type CommandObserver func(command string)

// ShellTool executes shell commands from an allowlist
type ShellTool struct {
	settings      *config.Settings
	externalTools []*config.ExternalTool
	observer      CommandObserver // Optional callback when commands are executed
}

// NewShellTool creates a new shell tool
func NewShellTool(settings *config.Settings) *ShellTool {
	return &ShellTool{
		settings: settings,
	}
}

// NewShellToolWithExternalTools creates a shell tool with external tool definitions
func NewShellToolWithExternalTools(settings *config.Settings, externalTools []*config.ExternalTool) *ShellTool {
	return &ShellTool{
		settings:      settings,
		externalTools: externalTools,
	}
}

// SetCommandObserver sets a callback that's invoked when any shell command is executed
func (t *ShellTool) SetCommandObserver(observer CommandObserver) {
	t.observer = observer
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Description() string {
	desc := "Execute a shell command. Only commands from the allowlist are permitted: " +
		strings.Join(t.settings.Tools.Shell.Allowlist, ", ")

	// Add external tools
	if len(t.externalTools) > 0 {
		var extNames []string
		for _, ext := range t.externalTools {
			if ext.Access.Type == "shell" {
				extNames = append(extNames, ext.Access.Command)
			}
		}
		if len(extNames) > 0 {
			desc += ", " + strings.Join(extNames, ", ")
		}
	}

	return desc
}

// GetExternalToolsPrompt returns a formatted description of all external tools for the system prompt
func (t *ShellTool) GetExternalToolsPrompt() string {
	if len(t.externalTools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Available External Tools\n\n")
	sb.WriteString("The following specialized tools are available via the shell. ")
	sb.WriteString("IMPORATNT: ALWAYS use the get_command_schema tool to discover available subcommands and options.\n\n")

	for _, ext := range t.externalTools {
		sb.WriteString(fmt.Sprintf("- **%s**: %s", ext.Access.Command, ext.Description))
		if ext.WhenToUse != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", ext.WhenToUse))
		}
		sb.WriteString("\n")
		if ext.Access.Details != "" {
			sb.WriteString(fmt.Sprintf("  - **Important:** %s\n", ext.Access.Details))
		}
	}

	return sb.String()
}

func (t *ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ShellTool) Execute(args map[string]any) (string, error) {
	commandRaw, ok := args["command"]
	if !ok {
		return "", fmt.Errorf("missing required parameter: command")
	}

	command, ok := commandRaw.(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	// Validate command against allowlist
	if err := t.validateCommand(command); err != nil {
		return "", err
	}

	// Notify observer of command execution
	if t.observer != nil {
		t.observer(command)
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Set environment variables if this is an external tool
	if env := t.getExternalToolEnv(command); env != nil {
		cmd.Env = env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Combine output
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("command timed out after %v", shellTimeout)
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// getExternalToolEnv returns the environment variables for an external tool command.
// Returns nil if no external tool matches or no env config is set.
func (t *ShellTool) getExternalToolEnv(command string) []string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}

	baseCmd := parts[0]

	for _, ext := range t.externalTools {
		if ext.Access.Type == "shell" && ext.Access.Command == baseCmd {
			return ext.BuildEnv()
		}
	}

	return nil
}

func (t *ShellTool) validateCommand(command string) error {
	// Check for shell operators that could be used to chain commands
	dangerousPatterns := []string{"&&", "||", ";", "|", "`", "$(", "${", ">", "<"}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(command, pattern) {
			return fmt.Errorf("command contains disallowed pattern: %s", pattern)
		}
	}

	// Extract the base command (first word)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	baseCmd := parts[0]

	// Check if base command is in settings allowlist
	if t.settings.IsCommandAllowed(baseCmd) {
		return nil
	}

	// Check if it's an external tool
	for _, ext := range t.externalTools {
		if ext.Access.Type == "shell" && ext.Access.Command == baseCmd {
			return nil
		}
	}

	return fmt.Errorf("command not in allowlist: %s (allowed: %s)",
		baseCmd, strings.Join(t.settings.Tools.Shell.Allowlist, ", "))
}
