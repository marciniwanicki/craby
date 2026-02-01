package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig holds logging configuration
type LogConfig struct {
	// MaxSize is the maximum size in megabytes before rotation
	MaxSize int
	// MaxBackups is the maximum number of old log files to retain
	MaxBackups int
	// MaxAge is the maximum number of days to retain old log files
	MaxAge int
	// Compress determines if rotated files should be compressed
	Compress bool
}

// DefaultLogConfig returns default logging configuration
func DefaultLogConfig() LogConfig {
	return LogConfig{
		MaxSize:    10, // 10 MB
		MaxBackups: 5,  // Keep 5 old files
		MaxAge:     14, // 14 days
		Compress:   true,
	}
}

// LogsDir returns the path to ~/.craby/logs/
func LogsDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs"), nil
}

// SetupLogger creates a zerolog logger that writes to both stdout and a rolling log file
func SetupLogger(cfg LogConfig) (zerolog.Logger, io.Closer, error) {
	logsDir, err := LogsDir()
	if err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("failed to get logs directory: %w", err)
	}

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logPath := filepath.Join(logsDir, "craby.log")

	// Delete existing log file to start fresh each daemon session
	_ = os.Remove(logPath)

	// Set up lumberjack for rolling logs
	fileWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	}

	// Create a multi-writer for both stdout and file
	// Console output is human-readable, file output is JSON for parsing
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
	multiWriter := io.MultiWriter(consoleWriter, fileWriter)

	// Create logger with timestamp
	logger := zerolog.New(multiWriter).With().Timestamp().Caller().Logger()

	// Set global log level to debug for detailed logging
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	return logger, fileWriter, nil
}

// SetupFileOnlyLogger creates a logger that only writes to file (no stdout)
func SetupFileOnlyLogger(cfg LogConfig) (zerolog.Logger, io.Closer, error) {
	logsDir, err := LogsDir()
	if err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("failed to get logs directory: %w", err)
	}

	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logPath := filepath.Join(logsDir, "craby.log")

	// Delete existing log file to start fresh each daemon session
	_ = os.Remove(logPath)

	fileWriter := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	}

	logger := zerolog.New(fileWriter).With().Timestamp().Caller().Logger()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	return logger, fileWriter, nil
}

// ClearLLMCallLogs removes all .md files from the logs directory
func ClearLLMCallLogs() error {
	logsDir, err := LogsDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist, nothing to clear
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			_ = os.Remove(filepath.Join(logsDir, entry.Name()))
		}
	}

	return nil
}

// LLMCallLogger logs LLM calls to separate markdown files
type LLMCallLogger struct {
	logsDir string
	index   int
	mu      sync.Mutex
}

// NewLLMCallLogger creates a new LLM call logger
func NewLLMCallLogger() (*LLMCallLogger, error) {
	logsDir, err := LogsDir()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return nil, err
	}

	return &LLMCallLogger{
		logsDir: logsDir,
		index:   0,
	}, nil
}

// LLMCallLog represents a single LLM call to be logged
type LLMCallLog struct {
	Type       string           // "chat", "chat_with_tools", "simple_chat"
	Model      string           // Model name
	Messages   []LLMMessageLog  // Input messages
	Tools      []string         // Tool names if any
	Response   string           // Response content
	ToolCalls  []LLMToolCallLog // Tool calls in response
	Error      string           // Error if any
	DurationMs int64            // Duration in milliseconds
}

// LLMMessageLog represents a message in the LLM call
type LLMMessageLog struct {
	Role    string
	Content string
}

// LLMToolCallLog represents a tool call in the response
type LLMToolCallLog struct {
	Name      string
	Arguments string
}

// Log writes an LLM call to a markdown file
func (l *LLMCallLogger) Log(call LLMCallLog) error {
	l.mu.Lock()
	index := l.index
	l.index++
	l.mu.Unlock()

	filename := fmt.Sprintf("llm_call_%d.md", index)
	filepath := filepath.Join(l.logsDir, filename)

	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("# LLM Call %d\n\n", index))
	sb.WriteString(fmt.Sprintf("**Type:** %s  \n", call.Type))
	sb.WriteString(fmt.Sprintf("**Model:** %s  \n", call.Model))
	sb.WriteString(fmt.Sprintf("**Time:** %s  \n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Duration:** %dms  \n\n", call.DurationMs))

	// Input messages
	sb.WriteString("## Input Messages\n\n")
	for i, msg := range call.Messages {
		sb.WriteString(fmt.Sprintf("### Message %d (%s)\n\n", i, msg.Role))
		sb.WriteString("```\n")
		sb.WriteString(msg.Content)
		sb.WriteString("\n```\n\n")
	}

	// Tools if any
	if len(call.Tools) > 0 {
		sb.WriteString("## Tools Available\n\n")
		for _, tool := range call.Tools {
			sb.WriteString(fmt.Sprintf("- %s\n", tool))
		}
		sb.WriteString("\n")
	}

	// Response
	sb.WriteString("## Response\n\n")
	if call.Error != "" {
		sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", call.Error))
	} else {
		if call.Response != "" {
			sb.WriteString("### Content\n\n")
			sb.WriteString("```\n")
			sb.WriteString(call.Response)
			sb.WriteString("\n```\n\n")
		}

		if len(call.ToolCalls) > 0 {
			sb.WriteString("### Tool Calls\n\n")
			for i, tc := range call.ToolCalls {
				sb.WriteString(fmt.Sprintf("#### Tool Call %d: %s\n\n", i, tc.Name))
				sb.WriteString("```json\n")
				sb.WriteString(tc.Arguments)
				sb.WriteString("\n```\n\n")
			}
		}
	}

	//nolint:gosec // Log files in user's config directory
	return os.WriteFile(filepath, []byte(sb.String()), 0640)
}
