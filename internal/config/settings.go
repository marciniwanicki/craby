package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marciniwanicki/craby/templates"
)

// Settings represents the application settings
type Settings struct {
	Tools     ToolsSettings     `json:"tools"`
	Variables TemplateVariables `json:"variables"`
}

// TemplateVariables contains variables that are substituted in templates
type TemplateVariables struct {
	Username      string `json:"username"`
	HomeDirectory string `json:"home_directory"`
	OSName        string `json:"os_name"`
}

// ToolsSettings contains tool-related settings
type ToolsSettings struct {
	Shell ShellSettings `json:"shell"`
	Write WriteSettings `json:"write"`
}

// WriteSettings contains write tool settings
type WriteSettings struct {
	Enabled      bool     `json:"enabled"`
	AllowedPaths []string `json:"allowed_paths"` // Paths where writing is allowed (supports ~)
	BlockedPaths []string `json:"blocked_paths"` // Paths that are always blocked
	MaxFileSize  int64    `json:"max_file_size"` // Maximum file size in bytes (0 = unlimited)
}

// ShellSettings contains shell tool settings
type ShellSettings struct {
	Enabled   bool     `json:"enabled"`
	Allowlist []string `json:"allowlist"`
}

// DefaultSettings returns the default settings
func DefaultSettings() *Settings {
	return &Settings{
		Tools: ToolsSettings{
			Shell: ShellSettings{
				Enabled: true,
				Allowlist: []string{
					"date",
					"whoami",
					"pwd",
					"ls",
					"cat",
					"head",
					"tail",
					"wc",
					"echo",
					"uname",
					"hostname",
					"uptime",
				},
			},
			Write: WriteSettings{
				Enabled:      true,
				AllowedPaths: []string{"~", "/tmp"},
				BlockedPaths: []string{"~/.ssh", "~/.gnupg", "~/.aws", "~/.craby/settings.json"},
				MaxFileSize:  10 * 1024 * 1024, // 10MB default
			},
		},
		Variables: DefaultTemplateVariables(),
	}
}

// DefaultTemplateVariables returns template variables populated from the environment
func DefaultTemplateVariables() TemplateVariables {
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME") // Windows fallback
	}

	home := os.Getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}

	return TemplateVariables{
		Username:      username,
		HomeDirectory: home,
		OSName:        getOS(),
	}
}

// ConfigDir returns the path to ~/.craby
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".craby"), nil
}

// SettingsPath returns the path to settings.json
func SettingsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// Load loads settings from ~/.craby/settings.json
// If the file doesn't exist, it creates it with default settings
func Load() (*Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, err
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Create default settings
		settings := DefaultSettings()
		if err := settings.Save(); err != nil {
			return nil, err
		}
		return settings, nil
	}

	// Read existing file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Start with defaults
	settings := DefaultSettings()

	// Unmarshal over defaults (preserves defaults for missing fields)
	if err := json.Unmarshal(data, settings); err != nil {
		return nil, err
	}

	// Ensure variables have values (fill in any empty ones with defaults)
	defaults := DefaultTemplateVariables()
	if settings.Variables.Username == "" {
		settings.Variables.Username = defaults.Username
	}
	if settings.Variables.HomeDirectory == "" {
		settings.Variables.HomeDirectory = defaults.HomeDirectory
	}
	if settings.Variables.OSName == "" {
		settings.Variables.OSName = defaults.OSName
	}

	return settings, nil
}

// Save saves settings to ~/.craby/settings.json
func (s *Settings) Save() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	path, err := SettingsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// IsCommandAllowed checks if a command is in the shell allowlist
func (s *Settings) IsCommandAllowed(cmd string) bool {
	if !s.Tools.Shell.Enabled {
		return false
	}

	for _, allowed := range s.Tools.Shell.Allowlist {
		if allowed == cmd {
			return true
		}
	}
	return false
}

// ExpandPath expands ~ to the user's home directory
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	} else if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return path
}

// IsWritePathAllowed checks if a path is allowed for writing
func (s *Settings) IsWritePathAllowed(targetPath string) (bool, string) {
	if !s.Tools.Write.Enabled {
		return false, "write tool is disabled"
	}

	// Clean and resolve the target path
	expandedTarget := ExpandPath(targetPath)
	absTarget, err := filepath.Abs(expandedTarget)
	if err != nil {
		return false, "invalid path"
	}

	// Check blocked paths first (takes precedence)
	for _, blocked := range s.Tools.Write.BlockedPaths {
		expandedBlocked := ExpandPath(blocked)
		absBlocked, err := filepath.Abs(expandedBlocked)
		if err != nil {
			continue
		}
		// Check if target is the blocked path or inside it
		if absTarget == absBlocked || strings.HasPrefix(absTarget, absBlocked+string(filepath.Separator)) {
			return false, "path is blocked: " + blocked
		}
	}

	// Check if path is within allowed paths
	for _, allowed := range s.Tools.Write.AllowedPaths {
		expandedAllowed := ExpandPath(allowed)
		absAllowed, err := filepath.Abs(expandedAllowed)
		if err != nil {
			continue
		}
		// Check if target is the allowed path or inside it
		if absTarget == absAllowed || strings.HasPrefix(absTarget, absAllowed+string(filepath.Separator)) {
			return true, ""
		}
	}

	return false, "path not in allowed paths"
}

// Templates holds the loaded template content
type Templates struct {
	Identity string
	User     string
}

// DefaultIdentityTemplate returns the default identity template from embedded files
func DefaultIdentityTemplate() string {
	content, err := templates.Identity()
	if err != nil {
		return "You are Craby, a helpful personal AI assistant."
	}
	return content
}

// DefaultUserTemplate returns the default user template with placeholders replaced
func DefaultUserTemplate() string {
	content, err := templates.User()
	if err != nil {
		return "# User"
	}
	return content
}

// processTemplate replaces placeholders in a template with values from settings
func processTemplate(content string, vars TemplateVariables) string {
	replacements := map[string]string{
		"{{USERNAME}}":       vars.Username,
		"{{HOME}}":           vars.HomeDirectory,
		"{{HOME_DIRECTORY}}": vars.HomeDirectory,
		"{{OS}}":             vars.OSName,
		"{{OS_NAME}}":        vars.OSName,
	}

	for placeholder, value := range replacements {
		content = strings.ReplaceAll(content, placeholder, value)
	}

	return content
}

// processUserTemplate replaces placeholders in the user template with actual values
// Deprecated: use processTemplate with settings.Variables instead
func processUserTemplate(content string) string {
	return processTemplate(content, DefaultTemplateVariables())
}

func getOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return runtime.GOOS
	}
}

// PipelineTemplates holds templates for the pipeline agent
type PipelineTemplates struct {
	Planning  string
	Synthesis string
	Identity  string
	User      string
}

// LoadPipelineTemplates loads templates for the pipeline agent
// Uses built-in templates by default, with optional overrides from ~/.craby/
func LoadPipelineTemplates() (*PipelineTemplates, error) {
	// Load settings to get variables
	settings, err := Load()
	if err != nil {
		// Use default variables if settings can't be loaded
		settings = DefaultSettings()
	}

	return LoadPipelineTemplatesWithSettings(settings)
}

// LoadPipelineTemplatesWithSettings loads templates using provided settings
func LoadPipelineTemplatesWithSettings(settings *Settings) (*PipelineTemplates, error) {
	// Load base templates first
	baseTemplates, err := LoadTemplatesWithSettings(settings)
	if err != nil {
		return nil, err
	}

	result := &PipelineTemplates{
		Identity: baseTemplates.Identity,
		User:     baseTemplates.User,
	}

	dir, _ := ConfigDir()

	// Load planning template (built-in default, optional override)
	planningContent, err := templates.Planning()
	if err != nil {
		return nil, fmt.Errorf("failed to load planning template: %w", err)
	}
	// Check for user override
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, "planning.md")); err == nil {
			planningContent = string(data)
		}
	}
	result.Planning = processTemplate(planningContent, settings.Variables)

	// Load synthesis template (built-in default, optional override)
	synthesisContent, err := templates.Synthesis()
	if err != nil {
		return nil, fmt.Errorf("failed to load synthesis template: %w", err)
	}
	// Check for user override
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, "synthesis.md")); err == nil {
			synthesisContent = string(data)
		}
	}
	result.Synthesis = processTemplate(synthesisContent, settings.Variables)

	return result, nil
}

// LoadTemplates loads templates using default settings
// Uses built-in templates by default, with optional overrides from ~/.craby/
func LoadTemplates() (*Templates, error) {
	settings, err := Load()
	if err != nil {
		settings = DefaultSettings()
	}
	return LoadTemplatesWithSettings(settings)
}

// LoadTemplatesWithSettings loads templates using provided settings
// Uses built-in templates by default, with optional overrides from ~/.craby/
// Does NOT auto-create files - only reads if they exist
func LoadTemplatesWithSettings(settings *Settings) (*Templates, error) {
	dir, _ := ConfigDir()

	result := &Templates{}

	// Load identity template (built-in default, optional override)
	result.Identity = DefaultIdentityTemplate()
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, "identity.md")); err == nil {
			result.Identity = string(data)
		}
	}
	result.Identity = processTemplate(result.Identity, settings.Variables)

	// Load user template (built-in default, optional override)
	result.User = DefaultUserTemplate()
	if dir != "" {
		if data, err := os.ReadFile(filepath.Join(dir, "user.md")); err == nil {
			result.User = string(data)
		}
	}
	result.User = processTemplate(result.User, settings.Variables)

	return result, nil
}
