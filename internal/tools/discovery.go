package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/marciniwanicki/craby/internal/config"
)

const discoverySchemaTimeout = 30 * time.Second

// SchemaGeneratorLLM is the interface for generating schemas from help text
type SchemaGeneratorLLM interface {
	SimpleChat(ctx context.Context, systemPrompt, userMessage string) (string, error)
}

// ListCommandsTool lists available commands that can be discovered
type ListCommandsTool struct {
	settings      *config.Settings
	externalTools []*config.ExternalTool
	schemaCache   *config.SchemaCache
}

// NewListCommandsTool creates a new list commands tool
func NewListCommandsTool(settings *config.Settings, externalTools []*config.ExternalTool, cache *config.SchemaCache) *ListCommandsTool {
	return &ListCommandsTool{
		settings:      settings,
		externalTools: externalTools,
		schemaCache:   cache,
	}
}

func (t *ListCommandsTool) Name() string {
	return "list_available_commands"
}

func (t *ListCommandsTool) Description() string {
	return `Lists all available CLI commands that can be used.
Returns commands from the allowlist, external tools, and previously discovered commands.
Use this to find out what tools are available before attempting to use them.
After finding a command you want to use, call get_command_schema to learn its parameters.`
}

func (t *ListCommandsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category": map[string]any{
				"type":        "string",
				"description": "Optional filter: 'allowlist', 'external', 'cached', or 'all' (default)",
				"enum":        []string{"all", "allowlist", "external", "cached"},
			},
		},
		"required": []string{},
	}
}

func (t *ListCommandsTool) Execute(args map[string]any) (string, error) {
	category := "all"
	if cat, ok := args["category"].(string); ok && cat != "" {
		category = cat
	}

	var result strings.Builder
	result.WriteString("# Available Commands\n\n")

	// Allowlist commands
	if category == "all" || category == "allowlist" {
		result.WriteString("## Shell Allowlist\n")
		result.WriteString("These are pre-approved shell commands:\n")
		for _, cmd := range t.settings.Tools.Shell.Allowlist {
			result.WriteString(fmt.Sprintf("- `%s`\n", cmd))
		}
		result.WriteString("\n")
	}

	// External tools
	if category == "all" || category == "external" {
		if len(t.externalTools) > 0 {
			result.WriteString("## External Tools\n")
			result.WriteString("Specialized tools with full documentation:\n")
			for _, ext := range t.externalTools {
				result.WriteString(fmt.Sprintf("- `%s`: %s\n", ext.Access.Command, ext.Description))
			}
			result.WriteString("\n")
		}
	}

	// Cached schemas
	if category == "all" || category == "cached" {
		if t.schemaCache != nil {
			cached, err := t.schemaCache.List()
			if err == nil && len(cached) > 0 {
				result.WriteString("## Previously Discovered\n")
				result.WriteString("Commands with cached schemas (ready to use):\n")
				for _, cmd := range cached {
					result.WriteString(fmt.Sprintf("- `%s`\n", cmd))
				}
				result.WriteString("\n")
			}
		}
	}

	result.WriteString("---\n")
	result.WriteString("Use `get_command_schema` with a command name to learn its parameters.\n")

	return result.String(), nil
}

// GetCommandSchemaTool discovers and returns the schema for a CLI command
type GetCommandSchemaTool struct {
	settings    *config.Settings
	schemaCache *config.SchemaCache
	llm         SchemaGeneratorLLM
}

// NewGetCommandSchemaTool creates a new get command schema tool
func NewGetCommandSchemaTool(settings *config.Settings, cache *config.SchemaCache, llm SchemaGeneratorLLM) *GetCommandSchemaTool {
	return &GetCommandSchemaTool{
		settings:    settings,
		schemaCache: cache,
		llm:         llm,
	}
}

func (t *GetCommandSchemaTool) Name() string {
	return "get_command_schema"
}

func (t *GetCommandSchemaTool) Description() string {
	return `Generates a structured JSON schema for a CLI command by analyzing its "--help" output. Use this tool for pre-execution discovery to ensure correct syntax and parameter handling.

## Operational Procedure
1.  **Top-Down Analysis:** Always start with the base command (e.g., "tfl").
2.  **Recursive Logic:** Identify available subcommands from the top-level schema. **Never** hallucinate or guess subcommand names.
3.  **Incremental Detail:** To investigate a subcommand, call the tool with the subcommand (e.g., {"command":"tfl", "subcommand": "status"}) only after the parent command has confirmed its existence.

## Output Requirements
The tool must return a valid **JSON Schema** containing:
* "parameters": Object defining flags, options, and positional arguments.
* "types": Mapping of inputs to "string", "boolean", "integer", or "enum" based on help text.
* "subcommands": A list of valid child commands for further exploration.
* "descriptions": Brief documentation for each element.

## Strict Constraints
* **No Execution:** Do not attempt to run the command with the shell tool until the parameter schema is fully resolved.
* **Incremental Only:** If a command has deep nesting, you must resolve one level at a time.`
}

func (t *GetCommandSchemaTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command name to discover (e.g., 'docker', 'git', 'kubectl')",
			},
			"subcommand": map[string]any{
				"type":        "string",
				"description": "Optional subcommand to get detailed schema for (e.g., 'run' for 'docker run')",
			},
		},
		"required": []string{"command"},
	}
}

func (t *GetCommandSchemaTool) Execute(args map[string]any) (string, error) {
	commandRaw, ok := args["command"]
	if !ok {
		return "", fmt.Errorf("missing required parameter: command")
	}
	command, ok := commandRaw.(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	subcommand := ""
	if sub, ok := args["subcommand"].(string); ok {
		subcommand = sub
	}

	// Note: caching disabled during development
	// TODO: re-enable caching once schema generation is stable

	// Validate command is allowed
	if !t.isCommandAllowed(command) {
		return "", fmt.Errorf("command not in allowlist: %s", command)
	}

	// Get help text
	helpText, err := t.getHelpText(command, subcommand)
	if err != nil {
		return "", fmt.Errorf("failed to get help for %s: %w", command, err)
	}

	// Generate schema using LLM
	schema, err := t.generateSchema(command, subcommand, helpText)
	if err != nil {
		// Fall back to returning raw help if LLM fails
		return fmt.Sprintf("# %s Help\n\nCould not generate schema: %v\n\nRaw help:\n```\n%s\n```",
			command, err, helpText), nil
	}

	return t.formatSchema(command, subcommand, schema, helpText), nil
}

func (t *GetCommandSchemaTool) isCommandAllowed(command string) bool {
	// Check settings allowlist
	if t.settings.IsCommandAllowed(command) {
		return true
	}

	// Always allow discovering common system commands
	safeCommands := map[string]bool{
		"git": true, "docker": true, "kubectl": true, "npm": true,
		"yarn": true, "pnpm": true, "cargo": true, "go": true,
		"python": true, "pip": true, "node": true, "ruby": true,
		"make": true, "cmake": true, "gradle": true, "mvn": true,
	}

	return safeCommands[command]
}

func (t *GetCommandSchemaTool) getHelpText(command, subcommand string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build command
	var cmdStr string
	if subcommand != "" {
		cmdStr = fmt.Sprintf("%s %s --help", command, subcommand)
	} else {
		cmdStr = fmt.Sprintf("%s --help", command)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run() // Ignore error - help often exits non-zero

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if len(output) < 20 {
		return "", fmt.Errorf("no help output available")
	}

	// Truncate if too long
	if len(output) > 8000 {
		output = output[:8000] + "\n... (truncated)"
	}

	return output, nil
}

func (t *GetCommandSchemaTool) generateSchema(command, subcommand, helpText string) (map[string]any, error) {
	if t.llm == nil {
		return nil, fmt.Errorf("no LLM available for schema generation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), discoverySchemaTimeout)
	defer cancel()

	systemPrompt := `# Role
You are a CLI Documentation Parser. Your task is to transform raw "--help" output into a precise, machine-readable JSON schema. All descriptions and text in the output MUST be in English.

# Task
Analyze the provided help text and output ONLY a valid JSON object.

# Schema Specification
{
  "name": "full_command_path",
  "description": "Concise summary of purpose in English",
  "subcommands": [
    {
      "name": "name", 
      "description": "description in English"
    }
  ],
  "flags": [
    {
      "name": "--long-name",
      "short": "-s",
      "description": "description in English",
      "type": "boolean | string | number | array",
      "required": false,
      "default": "value or null"
    }
  ],
  "arguments": [
    {
      "name": "arg_name",
      "description": "description in English",
      "required": true,
      "variadic": false
    }
  ],
  "examples": [
    "example usage 1",
    "example usage 2"
  ]
}

# Strict Guidelines
1. **Language**: The entire output must be in English, regardless of the language of the source help text.
2. **Type Precision**: 
   - Use "boolean" for "switches" (flags with no value).
   - Use "string" or "number" for options that require an argument (e.g., "--port 80").
   - Use "array" if a flag can be passed multiple times.
3. **Completeness**: Include all subcommands. For flags, prioritize the 10 most relevant if the list is exhaustive.
4. **Required vs. Optional**: Infer "required: true" if the help text uses angle brackets like "<item>" or explicitly states a field is mandatory.
5. **Variadic Arguments**: Mark "variadic: true" for arguments that accept multiple values (e.g., "[files...]").
6. **No Prose**: Output the JSON block only. Do not include introductory text, conversational filler, or markdown code blocks in your response.`

	cmdName := command
	if subcommand != "" {
		cmdName = command + " " + subcommand
	}

	userMessage := fmt.Sprintf("Convert this help text for `%s` into a JSON schema:\n\n```\n%s\n```", cmdName, helpText)

	response, err := t.llm.SimpleChat(ctx, systemPrompt, userMessage)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse JSON response
	response = strings.TrimSpace(response)

	// Try to extract JSON from response
	var schema map[string]any
	if err := json.Unmarshal([]byte(response), &schema); err != nil {
		// Try to find JSON in response
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start != -1 && end > start {
			if err := json.Unmarshal([]byte(response[start:end+1]), &schema); err != nil {
				return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no JSON found in LLM response")
		}
	}

	return schema, nil
}

func (t *GetCommandSchemaTool) formatSchema(command, subcommand string, schema map[string]any, helpText string) string {
	var result strings.Builder

	cmdName := command
	if subcommand != "" {
		cmdName = command + " " + subcommand
	}

	result.WriteString(fmt.Sprintf("# %s Schema\n\n", cmdName))

	// Description
	if desc, ok := schema["description"].(string); ok {
		result.WriteString(fmt.Sprintf("**Description:** %s\n\n", desc))
	}

	// Subcommands
	if subs, ok := schema["subcommands"].([]any); ok && len(subs) > 0 {
		result.WriteString("## Subcommands\n")
		for _, sub := range subs {
			if s, ok := sub.(map[string]any); ok {
				name := s["name"]
				desc := s["description"]
				result.WriteString(fmt.Sprintf("- `%s %v`: %v\n", cmdName, name, desc))
			}
		}
		result.WriteString("\n")
	}

	// Flags
	if flags, ok := schema["flags"].([]any); ok && len(flags) > 0 {
		result.WriteString("## Flags\n")
		for _, flag := range flags {
			if f, ok := flag.(map[string]any); ok {
				name := f["name"]
				short := f["short"]
				desc := f["description"]
				if short != nil && short != "" {
					result.WriteString(fmt.Sprintf("- `%v`, `%v`: %v\n", name, short, desc))
				} else {
					result.WriteString(fmt.Sprintf("- `%v`: %v\n", name, desc))
				}
			}
		}
		result.WriteString("\n")
	}

	// Arguments
	if args, ok := schema["arguments"].([]any); ok && len(args) > 0 {
		result.WriteString("## Arguments\n")
		for _, arg := range args {
			if a, ok := arg.(map[string]any); ok {
				name := a["name"]
				desc := a["description"]
				req := ""
				if r, ok := a["required"].(bool); ok && r {
					req = " (required)"
				}
				result.WriteString(fmt.Sprintf("- `%v`%s: %v\n", name, req, desc))
			}
		}
		result.WriteString("\n")
	}

	// Examples
	if examples, ok := schema["examples"].([]any); ok && len(examples) > 0 {
		result.WriteString("## Examples\n```\n")
		for _, ex := range examples {
			result.WriteString(fmt.Sprintf("%v\n", ex))
		}
		result.WriteString("```\n\n")
	}

	result.WriteString("---\n")
	result.WriteString("Use the `shell` tool to execute this command.\n")

	return result.String()
}
