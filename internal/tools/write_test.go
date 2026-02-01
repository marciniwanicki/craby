package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marciniwanicki/craby/internal/config"
)

func writeTestSettings(allowedPaths, blockedPaths []string) *config.Settings {
	return &config.Settings{
		Tools: config.ToolsSettings{
			Write: config.WriteSettings{
				Enabled:      true,
				AllowedPaths: allowedPaths,
				BlockedPaths: blockedPaths,
				MaxFileSize:  1024, // 1KB for tests
			},
		},
	}
}

func TestWriteTool_Name(t *testing.T) {
	tool := NewWriteTool(writeTestSettings([]string{"/tmp"}, nil))
	if tool.Name() != "write" {
		t.Errorf("expected name 'write', got %q", tool.Name())
	}
}

func TestWriteTool_Description(t *testing.T) {
	tool := NewWriteTool(writeTestSettings([]string{"/tmp", "~"}, nil))
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestWriteTool_Parameters(t *testing.T) {
	tool := NewWriteTool(writeTestSettings([]string{"/tmp"}, nil))
	params := tool.Parameters()

	if params["type"] != "object" {
		t.Error("expected type to be 'object'")
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	if _, ok := props["path"]; !ok {
		t.Error("expected 'path' property")
	}
	if _, ok := props["content"]; !ok {
		t.Error("expected 'content' property")
	}
	if _, ok := props["append"]; !ok {
		t.Error("expected 'append' property")
	}
}

func TestWriteTool_Execute_CreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, nil))

	filePath := filepath.Join(tmpDir, "test.txt")
	result, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "Hello, World!",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Verify file contents
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", string(content))
	}
}

func TestWriteTool_Execute_OverwriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, nil))

	filePath := filepath.Join(tmpDir, "test.txt")

	// Write initial content
	_, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "Initial content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Overwrite
	_, err = tool.Execute(map[string]any{
		"path":    filePath,
		"content": "New content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := os.ReadFile(filePath)
	if string(content) != "New content" {
		t.Errorf("expected 'New content', got %q", string(content))
	}
}

func TestWriteTool_Execute_AppendFile(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, nil))

	filePath := filepath.Join(tmpDir, "test.txt")

	// Write initial content
	_, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "Line 1\n",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Append
	_, err = tool.Execute(map[string]any{
		"path":    filePath,
		"content": "Line 2\n",
		"append":  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := os.ReadFile(filePath)
	if string(content) != "Line 1\nLine 2\n" {
		t.Errorf("expected 'Line 1\\nLine 2\\n', got %q", string(content))
	}
}

func TestWriteTool_Execute_CreateParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, nil))

	filePath := filepath.Join(tmpDir, "subdir", "nested", "test.txt")
	_, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "nested content",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(content) != "nested content" {
		t.Errorf("expected 'nested content', got %q", string(content))
	}
}

func TestWriteTool_Execute_BlockedPath(t *testing.T) {
	tmpDir := t.TempDir()
	blockedDir := filepath.Join(tmpDir, "blocked")
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, []string{blockedDir}))

	filePath := filepath.Join(blockedDir, "test.txt")
	_, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "should fail",
	})

	if err == nil {
		t.Error("expected error for blocked path")
	}
}

func TestWriteTool_Execute_PathNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewWriteTool(writeTestSettings([]string{tmpDir}, nil))

	// Try to write outside allowed path
	_, err := tool.Execute(map[string]any{
		"path":    "/etc/passwd",
		"content": "should fail",
	})

	if err == nil {
		t.Error("expected error for path not in allowed list")
	}
}

func TestWriteTool_Execute_MaxFileSizeExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	settings := writeTestSettings([]string{tmpDir}, nil)
	settings.Tools.Write.MaxFileSize = 10 // 10 bytes
	tool := NewWriteTool(settings)

	filePath := filepath.Join(tmpDir, "test.txt")
	_, err := tool.Execute(map[string]any{
		"path":    filePath,
		"content": "This content is longer than 10 bytes",
	})

	if err == nil {
		t.Error("expected error for exceeding max file size")
	}
}

func TestWriteTool_Execute_MissingPath(t *testing.T) {
	tool := NewWriteTool(writeTestSettings([]string{"/tmp"}, nil))

	_, err := tool.Execute(map[string]any{
		"content": "test",
	})

	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestWriteTool_Execute_MissingContent(t *testing.T) {
	tool := NewWriteTool(writeTestSettings([]string{"/tmp"}, nil))

	_, err := tool.Execute(map[string]any{
		"path": "/tmp/test.txt",
	})

	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestWriteTool_Execute_Disabled(t *testing.T) {
	settings := writeTestSettings([]string{"/tmp"}, nil)
	settings.Tools.Write.Enabled = false
	tool := NewWriteTool(settings)

	_, err := tool.Execute(map[string]any{
		"path":    "/tmp/test.txt",
		"content": "test",
	})

	if err == nil {
		t.Error("expected error when tool is disabled")
	}
}
