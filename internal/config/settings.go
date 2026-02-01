package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marciniwanicki/craby/templates"
)

// Settings represents the application settings
type Settings struct {
	Tools ToolsSettings `json:"tools"`
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

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return &settings, nil
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

// processUserTemplate replaces placeholders in the user template with actual values
func processUserTemplate(content string) string {
	replacements := map[string]string{
		"{{USERNAME}}": os.Getenv("USER"),
		"{{HOME}}":     os.Getenv("HOME"),
		"{{OS}}":       getOS(),
	}

	for placeholder, value := range replacements {
		content = strings.ReplaceAll(content, placeholder, value)
	}

	return content
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

// LoadTemplates loads templates from ~/.craby/identity.md and ~/.craby/user.md
// If files don't exist, creates them from the embedded default templates
func LoadTemplates() (*Templates, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	result := &Templates{}

	// Load or create identity template
	identityPath := filepath.Join(dir, "identity.md")
	if data, err := os.ReadFile(identityPath); err == nil {
		result.Identity = string(data)
	} else if os.IsNotExist(err) {
		result.Identity = DefaultIdentityTemplate()
		if err := os.WriteFile(identityPath, []byte(result.Identity), 0600); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Load or create user template
	userPath := filepath.Join(dir, "user.md")
	if data, err := os.ReadFile(userPath); err == nil {
		result.User = processUserTemplate(string(data))
	} else if os.IsNotExist(err) {
		defaultUser := DefaultUserTemplate()
		// Write the template with placeholders
		if err := os.WriteFile(userPath, []byte(defaultUser), 0600); err != nil {
			return nil, err
		}
		// Process placeholders for actual use
		result.User = processUserTemplate(defaultUser)
	} else {
		return nil, err
	}

	return result, nil
}
