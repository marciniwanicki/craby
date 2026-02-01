package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ExternalTool represents a tool defined in ~/.crabby/tools/
type ExternalTool struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	WhenToUse   string            `yaml:"when_to_use"`
	Access      ToolAccess        `yaml:"access"`
	Check       ToolCheck         `yaml:"check"`
	Subcommands []ToolSubcommand  `yaml:"subcommands,omitempty"`
	Examples    []string          `yaml:"examples,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty"`
}

// ToolAccess defines how to access/invoke the tool
type ToolAccess struct {
	Type    string `yaml:"type"`    // "shell", "api", "mcp" (future)
	Command string `yaml:"command"` // base command for shell type
	WorkDir string `yaml:"workdir,omitempty"`
}

// ToolCheck defines how to verify the tool is available
type ToolCheck struct {
	Command  string `yaml:"command"`            // command to run
	Expected string `yaml:"expected,omitempty"` // expected substring in output
}

// ToolSubcommand describes a subcommand of the tool
type ToolSubcommand struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Args        []string `yaml:"args,omitempty"`
	Example     string   `yaml:"example,omitempty"`
}

// ToolsDir returns the path to ~/.crabby/tools/
func ToolsDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tools"), nil
}

// LoadExternalTools loads all tool definitions from ~/.crabby/tools/
func LoadExternalTools() ([]*ExternalTool, error) {
	toolsDir, err := ToolsDir()
	if err != nil {
		return nil, err
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(toolsDir, 0750); err != nil {
		return nil, err
	}

	// Read tool directories
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil, err
	}

	var tools []*ExternalTool

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		toolName := entry.Name()
		toolDir := filepath.Join(toolsDir, toolName)

		// Look for <toolname>.yaml or tool.yaml
		yamlPaths := []string{
			filepath.Join(toolDir, toolName+".yaml"),
			filepath.Join(toolDir, toolName+".yml"),
			filepath.Join(toolDir, "tool.yaml"),
			filepath.Join(toolDir, "tool.yml"),
		}

		var tool *ExternalTool
		for _, yamlPath := range yamlPaths {
			if t, err := loadToolFromYAML(yamlPath); err == nil {
				tool = t
				break
			}
		}

		if tool != nil {
			// Ensure name matches directory if not set
			if tool.Name == "" {
				tool.Name = toolName
			}
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// loadToolFromYAML loads a single tool definition from a YAML file
func loadToolFromYAML(path string) (*ExternalTool, error) {
	// Path is constructed from trusted config directory (~/.crabby/tools/)
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is from user's config dir
	if err != nil {
		return nil, err
	}

	var tool ExternalTool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	return &tool, nil
}

// Validate checks if the tool definition is valid
func (t *ExternalTool) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if t.Description == "" {
		return fmt.Errorf("tool description is required")
	}
	if t.Access.Type == "" {
		return fmt.Errorf("access type is required")
	}
	if t.Access.Type == "shell" && t.Access.Command == "" {
		return fmt.Errorf("access command is required for shell tools")
	}
	return nil
}

// GenerateSystemPrompt generates a description of the tool for the LLM
func (t *ExternalTool) GenerateSystemPrompt() string {
	prompt := fmt.Sprintf("## Tool: %s\n\n", t.Name)
	prompt += fmt.Sprintf("**Description:** %s\n\n", t.Description)

	if t.WhenToUse != "" {
		prompt += fmt.Sprintf("**When to use:** %s\n\n", t.WhenToUse)
	}

	if len(t.Subcommands) > 0 {
		prompt += "**Available subcommands:**\n"
		for _, sub := range t.Subcommands {
			prompt += fmt.Sprintf("- `%s %s`: %s", t.Access.Command, sub.Name, sub.Description)
			if sub.Example != "" {
				prompt += fmt.Sprintf(" (example: `%s`)", sub.Example)
			}
			prompt += "\n"
		}
		prompt += "\n"
	}

	if len(t.Examples) > 0 {
		prompt += "**Examples:**\n"
		for _, ex := range t.Examples {
			prompt += fmt.Sprintf("- `%s`\n", ex)
		}
		prompt += "\n"
	}

	return prompt
}
