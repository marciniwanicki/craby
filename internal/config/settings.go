package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings represents the application settings
type Settings struct {
	Tools ToolsSettings `json:"tools"`
}

// ToolsSettings contains tool-related settings
type ToolsSettings struct {
	Shell ShellSettings `json:"shell"`
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
		},
	}
}

// ConfigDir returns the path to ~/.crabby
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".crabby"), nil
}

// SettingsPath returns the path to settings.json
func SettingsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// Load loads settings from ~/.crabby/settings.json
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

// Save saves settings to ~/.crabby/settings.json
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
