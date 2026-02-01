package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/marciniwanicki/crabby/internal/config"
)

const shellTimeout = 30 * time.Second

// Well-known system commands that don't need help discovery
var wellKnownCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "grep": true,
	"find": true, "wc": true, "sort": true, "uniq": true, "cut": true,
	"echo": true, "printf": true, "date": true, "cal": true,
	"pwd": true, "cd": true, "mkdir": true, "rmdir": true, "rm": true,
	"cp": true, "mv": true, "touch": true, "chmod": true, "chown": true,
	"whoami": true, "id": true, "groups": true, "uname": true,
	"hostname": true, "uptime": true, "ps": true, "top": true, "kill": true,
	"df": true, "du": true, "free": true, "mount": true, "umount": true,
	"ping": true, "curl": true, "wget": true, "ssh": true, "scp": true,
	"tar": true, "zip": true, "unzip": true, "gzip": true, "gunzip": true,
	"sed": true, "awk": true, "tr": true, "diff": true, "patch": true,
	"man": true, "which": true, "whereis": true, "type": true,
	"env": true, "export": true, "set": true, "unset": true,
	"true": true, "false": true, "test": true, "sleep": true,
	"xargs": true, "tee": true, "less": true, "more": true,
}

// ShellTool executes shell commands from an allowlist
type ShellTool struct {
	settings      *config.Settings
	externalTools []*config.ExternalTool
	helpCache     map[string]string
	cacheMu       sync.RWMutex
}

// NewShellTool creates a new shell tool
func NewShellTool(settings *config.Settings) *ShellTool {
	return &ShellTool{
		settings:  settings,
		helpCache: make(map[string]string),
	}
}

// NewShellToolWithExternalTools creates a shell tool with external tool definitions
func NewShellToolWithExternalTools(settings *config.Settings, externalTools []*config.ExternalTool) *ShellTool {
	return &ShellTool{
		settings:      settings,
		externalTools: externalTools,
		helpCache:     make(map[string]string),
	}
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
	sb.WriteString("When you first use any of these tools, the system will automatically discover ")
	sb.WriteString("available subcommands and options by calling --help. Use this discovered information ")
	sb.WriteString("to construct correct commands.\n\n")

	for _, ext := range t.externalTools {
		sb.WriteString(fmt.Sprintf("- **%s**: %s", ext.Access.Command, ext.Description))
		if ext.WhenToUse != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", ext.WhenToUse))
		}
		sb.WriteString("\n")
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

	// Check if this is an external tool that needs discovery
	discoveryInfo := t.runToolDiscoveryIfNeeded(command)

	// Execute with timeout
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)

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

	// Prepend discovery info if this was first use
	if discoveryInfo != "" {
		output = discoveryInfo + "\n---\nCommand output:\n" + output
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
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

// runToolDiscoveryIfNeeded checks if this is an external tool that needs discovery
// and runs a discovery loop to learn the tool's subcommands and options
func (t *ShellTool) runToolDiscoveryIfNeeded(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}

	baseCmd := parts[0]

	// Skip well-known system commands
	if wellKnownCommands[baseCmd] {
		return ""
	}

	// Check if we've already discovered this command
	t.cacheMu.RLock()
	if _, exists := t.helpCache[baseCmd]; exists {
		t.cacheMu.RUnlock()
		return ""
	}
	t.cacheMu.RUnlock()

	// Check if this is an external tool - if so, run full discovery
	var externalTool *config.ExternalTool
	for _, ext := range t.externalTools {
		if ext.Access.Type == "shell" && ext.Access.Command == baseCmd {
			externalTool = ext
			break
		}
	}

	var discoveryText string
	if externalTool != nil {
		// Run full discovery loop for external tools
		discoveryText = t.runExternalToolDiscovery(externalTool)
	} else {
		// Basic discovery for other unknown commands
		discoveryText = t.discoverCommand(baseCmd)
	}

	// Cache the discovery (even if empty)
	t.cacheMu.Lock()
	t.helpCache[baseCmd] = discoveryText
	t.cacheMu.Unlock()

	return discoveryText
}

// runExternalToolDiscovery runs a comprehensive discovery loop for an external tool
func (t *ShellTool) runExternalToolDiscovery(tool *config.ExternalTool) string {
	var result strings.Builder

	baseCmd := tool.Access.Command

	result.WriteString(fmt.Sprintf("=== Tool Discovery: %s ===\n\n", baseCmd))
	result.WriteString(fmt.Sprintf("**Description:** %s\n", tool.Description))
	if tool.WhenToUse != "" {
		result.WriteString(fmt.Sprintf("**When to use:** %s\n\n", tool.WhenToUse))
	}
	result.WriteString("Running discovery loop to learn available commands...\n\n")

	// Step 1: Fetch main help
	result.WriteString("## Step 1: Main command help\n")
	mainHelp := t.fetchSingleHelp(baseCmd, "")
	if mainHelp == "" {
		result.WriteString("Could not fetch help for main command.\n")
		return result.String()
	}
	result.WriteString(mainHelp)
	result.WriteString("\n")

	// Step 2: Parse and discover subcommands
	subcommands := t.parseSubcommands(mainHelp)
	if len(subcommands) == 0 {
		result.WriteString("\n## No subcommands detected\n")
		return result.String()
	}

	result.WriteString(fmt.Sprintf("\n## Step 2: Discovered %d subcommands\n", len(subcommands)))
	result.WriteString("Fetching help for each subcommand...\n\n")

	// Limit subcommands to avoid too much output
	maxSubcommands := 10
	if len(subcommands) > maxSubcommands {
		result.WriteString(fmt.Sprintf("(Limiting to first %d of %d subcommands)\n\n", maxSubcommands, len(subcommands)))
		subcommands = subcommands[:maxSubcommands]
	}

	// Step 3: Fetch help for each subcommand
	for i, sub := range subcommands {
		result.WriteString(fmt.Sprintf("### %d. %s %s\n", i+1, baseCmd, sub))
		subHelp := t.fetchSingleHelp(baseCmd, sub)
		if subHelp != "" {
			// Truncate if too long
			if len(subHelp) > 1000 {
				subHelp = subHelp[:1000] + "\n... (truncated)"
			}
			result.WriteString(subHelp)
		} else {
			result.WriteString("(No help available)")
		}
		result.WriteString("\n\n")
	}

	result.WriteString("=== Discovery complete. Use the information above to construct correct commands. ===\n")

	// Truncate total if needed
	output := result.String()
	if len(output) > 8000 {
		output = output[:8000] + "\n... (discovery output truncated)"
	}

	return output
}

// discoverCommand fetches help for a command and recursively discovers subcommands
func (t *ShellTool) discoverCommand(baseCmd string) string {
	var result strings.Builder

	// Fetch top-level help
	topHelp := t.fetchSingleHelp(baseCmd, "")
	if topHelp == "" {
		return ""
	}

	result.WriteString(fmt.Sprintf("=== Discovered '%s' (first use) ===\n\n", baseCmd))
	result.WriteString("## Main command help:\n")
	result.WriteString(topHelp)
	result.WriteString("\n")

	// Parse for subcommands and fetch their help
	subcommands := t.parseSubcommands(topHelp)
	if len(subcommands) > 0 {
		result.WriteString("\n## Subcommand details:\n")

		// Limit to first 5 subcommands to avoid too much output
		limit := 5
		if len(subcommands) < limit {
			limit = len(subcommands)
		}

		for i := 0; i < limit; i++ {
			sub := subcommands[i]
			subHelp := t.fetchSingleHelp(baseCmd, sub)
			if subHelp != "" {
				result.WriteString(fmt.Sprintf("\n### %s %s:\n", baseCmd, sub))
				// Truncate individual subcommand help
				if len(subHelp) > 800 {
					subHelp = subHelp[:800] + "\n... (truncated)"
				}
				result.WriteString(subHelp)
				result.WriteString("\n")
			}
		}

		if len(subcommands) > limit {
			result.WriteString(fmt.Sprintf("\n... and %d more subcommands\n", len(subcommands)-limit))
		}
	}

	// Truncate total output if too long
	output := result.String()
	if len(output) > 4000 {
		output = output[:4000] + "\n... (truncated)"
	}

	return output
}

// fetchSingleHelp tries to get help for a command or subcommand
func (t *ShellTool) fetchSingleHelp(baseCmd, subcommand string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try different help patterns (including just running the command which sometimes shows help)
	var patterns []string
	if subcommand != "" {
		patterns = []string{"--help", "-h", "help", "-help", ""}
	} else {
		patterns = []string{"--help", "-h", "help", "-help"}
	}

	for _, pattern := range patterns {
		var cmdStr string
		if subcommand != "" {
			if pattern == "" {
				cmdStr = fmt.Sprintf("%s %s", baseCmd, subcommand)
			} else {
				cmdStr = fmt.Sprintf("%s %s %s", baseCmd, subcommand, pattern)
			}
		} else {
			cmdStr = fmt.Sprintf("%s %s", baseCmd, pattern)
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		_ = cmd.Run() // Ignore error - help often exits non-zero

		// Combine stdout and stderr - many tools output help to stderr
		output := stdout.String()
		if stderr.Len() > 0 {
			if output != "" {
				output += "\n"
			}
			output += stderr.String()
		}

		// Check if we got meaningful help - be more lenient
		if t.looksLikeHelp(output) {
			return output
		}
	}

	return ""
}

// looksLikeHelp checks if output appears to be help text
func (t *ShellTool) looksLikeHelp(output string) bool {
	if len(output) < 30 {
		return false
	}

	lower := strings.ToLower(output)

	// Common help indicators
	helpIndicators := []string{
		"usage:", "usage ", "options:", "commands:", "arguments:",
		"flags:", "subcommands:", "available commands",
		"--help", "-h,", "description:", "synopsis:",
		"positional arguments", "optional arguments",
		"examples:", "example:", "run '", "see '",
	}

	matches := 0
	for _, indicator := range helpIndicators {
		if strings.Contains(lower, indicator) {
			matches++
		}
	}

	// Need at least 1 strong indicator, or output is long enough to likely be help
	return matches >= 1 || len(output) > 200
}

// parseSubcommands attempts to extract subcommand names from help text
func (t *ShellTool) parseSubcommands(helpText string) []string {
	var subcommands []string
	lines := strings.Split(helpText, "\n")

	inCommandsSection := false

	for _, line := range lines {
		lineLower := strings.ToLower(line)

		// Detect commands/subcommands section
		if strings.Contains(lineLower, "commands:") ||
			strings.Contains(lineLower, "available commands") ||
			strings.Contains(lineLower, "subcommands:") {
			inCommandsSection = true
			continue
		}

		// End of commands section (empty line or new section)
		if inCommandsSection {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				// Could be end of section, but continue looking
				continue
			}

			// New section header (usually ends with ":" or starts with uppercase word followed by ":")
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				inCommandsSection = false
				continue
			}

			// Parse subcommand: typically "  subcommand    Description..."
			// or "  subcommand, alias   Description..."
			parts := strings.Fields(trimmed)
			if len(parts) >= 1 {
				cmd := parts[0]
				// Clean up: remove trailing comma, skip if it looks like a flag
				cmd = strings.TrimSuffix(cmd, ",")
				if !strings.HasPrefix(cmd, "-") && len(cmd) > 1 && len(cmd) < 30 && isValidSubcommand(cmd) {
					subcommands = append(subcommands, cmd)
				}
			}
		}
	}

	return subcommands
}

// isValidSubcommand checks if a string looks like a valid subcommand name
func isValidSubcommand(s string) bool {
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		isSpecial := r == '-' || r == '_'
		if !isLower && !isUpper && !isDigit && !isSpecial {
			return false
		}
	}
	return true
}
