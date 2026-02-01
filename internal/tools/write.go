package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marciniwanicki/craby/internal/config"
)

// WriteTool writes content to files
type WriteTool struct {
	settings *config.Settings
}

// NewWriteTool creates a new write tool
func NewWriteTool(settings *config.Settings) *WriteTool {
	return &WriteTool{
		settings: settings,
	}
}

func (t *WriteTool) Name() string {
	return "write"
}

func (t *WriteTool) Description() string {
	return "Write content to a file. Can create new files or overwrite/append to existing ones. " +
		"Allowed paths: " + strings.Join(t.settings.Tools.Write.AllowedPaths, ", ")
}

func (t *WriteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to write to (supports ~ for home directory)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
			"append": map[string]any{
				"type":        "boolean",
				"description": "If true, append to the file instead of overwriting (default: false)",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteTool) Execute(args map[string]any) (string, error) {
	// Extract path parameter
	pathRaw, ok := args["path"]
	if !ok {
		return "", fmt.Errorf("missing required parameter: path")
	}
	path, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("path must be a string")
	}

	// Extract content parameter
	contentRaw, ok := args["content"]
	if !ok {
		return "", fmt.Errorf("missing required parameter: content")
	}
	content, ok := contentRaw.(string)
	if !ok {
		return "", fmt.Errorf("content must be a string")
	}

	// Extract append parameter (optional, defaults to false)
	appendMode := false
	if appendRaw, ok := args["append"]; ok {
		if appendBool, ok := appendRaw.(bool); ok {
			appendMode = appendBool
		}
	}

	// Validate path
	allowed, reason := t.settings.IsWritePathAllowed(path)
	if !allowed {
		return "", fmt.Errorf("write not allowed: %s", reason)
	}

	// Check file size limit
	if t.settings.Tools.Write.MaxFileSize > 0 {
		if int64(len(content)) > t.settings.Tools.Write.MaxFileSize {
			return "", fmt.Errorf("content exceeds maximum file size (%d bytes)", t.settings.Tools.Write.MaxFileSize)
		}
	}

	// Expand and resolve path
	expandedPath := config.ExpandPath(path)
	absPath, err := filepath.Abs(expandedPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Create parent directories if needed
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Determine file flags
	flags := os.O_WRONLY | os.O_CREATE
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	// Open/create file with secure permissions
	file, err := os.OpenFile(absPath, flags, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Write content
	n, err := file.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("failed to write content: %w", err)
	}

	action := "wrote"
	if appendMode {
		action = "appended"
	}

	return fmt.Sprintf("Successfully %s %d bytes to %s", action, n, path), nil
}
