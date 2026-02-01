package config

import (
	"encoding/json"
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

// ClearStepLogs removes all step_*.md files from the logs directory
func ClearStepLogs() error {
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
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "step_") && strings.HasSuffix(entry.Name(), ".md") {
			_ = os.Remove(filepath.Join(logsDir, entry.Name()))
		}
	}

	return nil
}

// Deprecated: use ClearStepLogs instead
func ClearLLMCallLogs() error {
	return ClearStepLogs()
}

// StepType represents the type of pipeline step being logged
type StepType string

const (
	StepTypeLLM       StepType = "llm"       // LLM calls (planning, synthesis, schema discovery)
	StepTypePlan      StepType = "plan"      // Generated plan
	StepTypeExecution StepType = "execution" // Tool execution
)

// StepLogger logs pipeline steps to separate markdown files with sequential numbering
type StepLogger struct {
	logsDir string
	index   int
	mu      sync.Mutex
}

// NewStepLogger creates a new step logger
func NewStepLogger() (*StepLogger, error) {
	logsDir, err := LogsDir()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return nil, err
	}

	return &StepLogger{
		logsDir: logsDir,
		index:   0,
	}, nil
}

// Reset resets the step counter (typically called at start of new request)
func (l *StepLogger) Reset() {
	l.mu.Lock()
	l.index = 0
	l.mu.Unlock()
}

// LLMStepLog represents a single LLM call to be logged
type LLMStepLog struct {
	Phase      string           // "planning", "synthesis", "schema_discovery", etc.
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

// PlanStepLog represents a generated plan to be logged
type PlanStepLog struct {
	Intent        string
	Complexity    string
	NeedsTools    bool
	ReadyToAnswer bool
	Context       []string
	Steps         []PlanStepEntry
	RawXML        string
}

// PlanStepEntry represents a single step in the plan
type PlanStepEntry struct {
	ID        string
	DependsOn string
	Tool      string
	Purpose   string
	Args      map[string]string
}

// ExecutionStepLog represents a tool execution to be logged
type ExecutionStepLog struct {
	StepID     string
	Tool       string
	Purpose    string
	Args       map[string]any
	Output     string
	Success    bool
	Error      string
	DurationMs int64
}

// nextIndex returns the next step index and increments the counter
func (l *StepLogger) nextIndex() int {
	l.mu.Lock()
	index := l.index
	l.index++
	l.mu.Unlock()
	return index
}

// LogLLM logs an LLM call step
func (l *StepLogger) LogLLM(log LLMStepLog) error {
	index := l.nextIndex()
	filename := fmt.Sprintf("step_%03d_llm_%s.md", index, sanitizeFilename(log.Phase))
	fpath := filepath.Join(l.logsDir, filename)

	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("# Step %03d: LLM Call (%s)\n\n", index, log.Phase))
	sb.WriteString(fmt.Sprintf("**Phase:** %s  \n", log.Phase))
	sb.WriteString(fmt.Sprintf("**Model:** %s  \n", log.Model))
	sb.WriteString(fmt.Sprintf("**Time:** %s  \n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Duration:** %dms  \n\n", log.DurationMs))

	// Input messages
	sb.WriteString("## Input Messages\n\n")
	for i, msg := range log.Messages {
		sb.WriteString(fmt.Sprintf("### Message %d (%s)\n\n", i, msg.Role))
		sb.WriteString("```\n")
		sb.WriteString(msg.Content)
		sb.WriteString("\n```\n\n")
	}

	// Tools if any
	if len(log.Tools) > 0 {
		sb.WriteString("## Tools Available\n\n")
		for _, tool := range log.Tools {
			sb.WriteString(fmt.Sprintf("- %s\n", tool))
		}
		sb.WriteString("\n")
	}

	// Response
	sb.WriteString("## Response\n\n")
	if log.Error != "" {
		sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", log.Error))
	} else {
		if log.Response != "" {
			sb.WriteString("### Content\n\n")
			sb.WriteString("```\n")
			sb.WriteString(log.Response)
			sb.WriteString("\n```\n\n")
		}

		if len(log.ToolCalls) > 0 {
			sb.WriteString("### Tool Calls\n\n")
			for i, tc := range log.ToolCalls {
				sb.WriteString(fmt.Sprintf("#### Tool Call %d: %s\n\n", i, tc.Name))
				sb.WriteString("```json\n")
				sb.WriteString(tc.Arguments)
				sb.WriteString("\n```\n\n")
			}
		}
	}

	//nolint:gosec // Log files in user's config directory
	return os.WriteFile(fpath, []byte(sb.String()), 0640)
}

// LogPlan logs a generated plan
func (l *StepLogger) LogPlan(log PlanStepLog) error {
	index := l.nextIndex()
	filename := fmt.Sprintf("step_%03d_plan.md", index)
	fpath := filepath.Join(l.logsDir, filename)

	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("# Step %03d: Plan Generated\n\n", index))
	sb.WriteString(fmt.Sprintf("**Time:** %s  \n\n", time.Now().Format(time.RFC3339)))

	// Plan overview
	sb.WriteString("## Overview\n\n")
	sb.WriteString(fmt.Sprintf("**Intent:** %s  \n", log.Intent))
	sb.WriteString(fmt.Sprintf("**Complexity:** %s  \n", log.Complexity))
	sb.WriteString(fmt.Sprintf("**Needs Tools:** %t  \n", log.NeedsTools))
	sb.WriteString(fmt.Sprintf("**Ready to Answer:** %t  \n\n", log.ReadyToAnswer))

	// Context
	if len(log.Context) > 0 {
		sb.WriteString("## Context\n\n")
		for _, item := range log.Context {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	// Steps
	if len(log.Steps) > 0 {
		sb.WriteString("## Planned Steps\n\n")
		for _, step := range log.Steps {
			sb.WriteString(fmt.Sprintf("### %s: %s\n\n", step.ID, step.Tool))
			if step.DependsOn != "" {
				sb.WriteString(fmt.Sprintf("**Depends On:** %s  \n", step.DependsOn))
			}
			sb.WriteString(fmt.Sprintf("**Purpose:** %s  \n\n", step.Purpose))
			if len(step.Args) > 0 {
				sb.WriteString("**Arguments:**\n```\n")
				for k, v := range step.Args {
					sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
				}
				sb.WriteString("```\n\n")
			}
		}
	}

	// Raw XML
	if log.RawXML != "" {
		sb.WriteString("## Raw Plan XML\n\n")
		sb.WriteString("```xml\n")
		sb.WriteString(log.RawXML)
		sb.WriteString("\n```\n")
	}

	//nolint:gosec // Log files in user's config directory
	return os.WriteFile(fpath, []byte(sb.String()), 0640)
}

// LogExecution logs a tool execution step
func (l *StepLogger) LogExecution(log ExecutionStepLog) error {
	index := l.nextIndex()
	filename := fmt.Sprintf("step_%03d_exec_%s.md", index, sanitizeFilename(log.Tool))
	fpath := filepath.Join(l.logsDir, filename)

	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("# Step %03d: Execute %s\n\n", index, log.Tool))
	sb.WriteString(fmt.Sprintf("**Step ID:** %s  \n", log.StepID))
	sb.WriteString(fmt.Sprintf("**Tool:** %s  \n", log.Tool))
	sb.WriteString(fmt.Sprintf("**Time:** %s  \n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Duration:** %dms  \n", log.DurationMs))
	sb.WriteString(fmt.Sprintf("**Success:** %t  \n\n", log.Success))

	// Purpose
	if log.Purpose != "" {
		sb.WriteString(fmt.Sprintf("**Purpose:** %s\n\n", log.Purpose))
	}

	// Arguments
	if len(log.Args) > 0 {
		sb.WriteString("## Arguments\n\n```json\n")
		argsJSON, _ := json.MarshalIndent(log.Args, "", "  ")
		sb.WriteString(string(argsJSON))
		sb.WriteString("\n```\n\n")
	}

	// Output
	sb.WriteString("## Output\n\n")
	if log.Error != "" {
		sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", log.Error))
	}
	sb.WriteString("```\n")
	sb.WriteString(log.Output)
	sb.WriteString("\n```\n")

	//nolint:gosec // Log files in user's config directory
	return os.WriteFile(fpath, []byte(sb.String()), 0640)
}

// LLMCallLogger is an alias for StepLogger for backward compatibility
// Deprecated: use StepLogger instead
type LLMCallLogger = StepLogger

// NewLLMCallLogger creates a new step logger (backward compatible)
// Deprecated: use NewStepLogger instead
func NewLLMCallLogger() (*StepLogger, error) {
	return NewStepLogger()
}

// LLMCallLog is an alias for LLMStepLog for backward compatibility
// Deprecated: use LLMStepLog instead
type LLMCallLog = LLMStepLog

// Log is a backward-compatible wrapper that logs an LLM call
func (l *StepLogger) Log(call LLMStepLog) error {
	// Convert old "Type" field to "Phase" if needed
	if call.Phase == "" {
		call.Phase = "unknown"
	}
	return l.LogLLM(call)
}
