package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	if !settings.Tools.Shell.Enabled {
		t.Error("expected shell to be enabled by default")
	}

	if len(settings.Tools.Shell.Allowlist) == 0 {
		t.Error("expected default allowlist to have commands")
	}

	// Check some expected default commands
	expectedCmds := []string{"date", "whoami", "pwd", "ls", "echo"}
	for _, cmd := range expectedCmds {
		if !settings.IsCommandAllowed(cmd) {
			t.Errorf("expected %q to be in default allowlist", cmd)
		}
	}
}

func TestIsCommandAllowed(t *testing.T) {
	settings := &Settings{
		Tools: ToolsSettings{
			Shell: ShellSettings{
				Enabled:   true,
				Allowlist: []string{"date", "echo", "ls"},
			},
		},
	}

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"date", true},
		{"echo", true},
		{"ls", true},
		{"rm", false},
		{"curl", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := settings.IsCommandAllowed(tt.cmd); got != tt.allowed {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.allowed)
			}
		})
	}
}

func TestIsCommandAllowed_ShellDisabled(t *testing.T) {
	settings := &Settings{
		Tools: ToolsSettings{
			Shell: ShellSettings{
				Enabled:   false,
				Allowlist: []string{"date", "echo"},
			},
		},
	}

	if settings.IsCommandAllowed("date") {
		t.Error("expected no commands allowed when shell is disabled")
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Override the config dir for testing
	t.Setenv("HOME", tmpDir)

	// Create custom settings
	settings := &Settings{
		Tools: ToolsSettings{
			Shell: ShellSettings{
				Enabled:   true,
				Allowlist: []string{"custom-cmd", "another-cmd"},
			},
		},
	}

	// Save settings
	if err := settings.Save(); err != nil {
		t.Fatalf("failed to save settings: %v", err)
	}

	// Verify file was created
	expectedPath := filepath.Join(tmpDir, ".crabby", "settings.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("settings file was not created at %s", expectedPath)
	}

	// Load settings
	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}

	// Verify loaded settings match
	if !loaded.Tools.Shell.Enabled {
		t.Error("loaded settings: shell should be enabled")
	}

	if len(loaded.Tools.Shell.Allowlist) != 2 {
		t.Errorf("loaded settings: expected 2 commands in allowlist, got %d", len(loaded.Tools.Shell.Allowlist))
	}

	if !loaded.IsCommandAllowed("custom-cmd") {
		t.Error("loaded settings: custom-cmd should be allowed")
	}
}

func TestLoad_CreatesDefaultIfNotExists(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	// Override the config dir for testing
	t.Setenv("HOME", tmpDir)

	// Load should create default settings
	settings, err := Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}

	// Should have default settings
	if !settings.Tools.Shell.Enabled {
		t.Error("expected shell to be enabled in default settings")
	}

	// File should exist now
	expectedPath := filepath.Join(tmpDir, ".crabby", "settings.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("settings file should have been created")
	}
}

func TestConfigDir(t *testing.T) {
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}

	if !filepath.IsAbs(dir) {
		t.Error("ConfigDir() should return absolute path")
	}

	if !strings.HasSuffix(dir, ".crabby") {
		t.Errorf("ConfigDir() = %q, should end with .crabby", dir)
	}
}

func TestSettingsPath(t *testing.T) {
	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath() error: %v", err)
	}

	if !filepath.IsAbs(path) {
		t.Error("SettingsPath() should return absolute path")
	}

	if filepath.Base(path) != "settings.json" {
		t.Errorf("SettingsPath() = %q, should end with settings.json", path)
	}
}
