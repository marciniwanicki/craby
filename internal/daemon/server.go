package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/marciniwanicki/craby/internal/agent"
	"github.com/marciniwanicki/craby/internal/api"
	"github.com/marciniwanicki/craby/internal/config"
	"github.com/marciniwanicki/craby/internal/tools"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

const Version = "0.1.0"

// Server represents the daemon server
type Server struct {
	port      int
	ollama    *OllamaClient
	handler   *Handler
	registry  *tools.Registry
	settings  *config.Settings
	logger    zerolog.Logger
	logCloser io.Closer
	upgrader  websocket.Upgrader
	quit      chan os.Signal
}

// NewServer creates a new daemon server
func NewServer(port int, ollamaURL, model string) *Server {
	// Set up rolling file logger
	logCfg := config.DefaultLogConfig()
	logger, logCloser, err := config.SetupLogger(logCfg)
	if err != nil {
		// Fall back to stdout-only logging
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger.Warn().Err(err).Msg("failed to set up file logging, using stdout only")
		logCloser = nil
	}

	// Clear old LLM call logs
	if err := config.ClearLLMCallLogs(); err != nil {
		logger.Warn().Err(err).Msg("failed to clear LLM call logs")
	}

	// Set up LLM call logger
	llmCallLogger, err := config.NewLLMCallLogger()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to set up LLM call logger")
	}

	// Load settings
	settings, err := config.Load()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load settings, using defaults")
		settings = config.DefaultSettings()
	}

	// Log loaded settings
	logger.Info().
		Bool("shell_enabled", settings.Tools.Shell.Enabled).
		Strs("shell_allowlist", settings.Tools.Shell.Allowlist).
		Msg("loaded settings")

	// Load templates
	templates, err := config.LoadTemplates()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load templates, using defaults")
		templates = &config.Templates{
			Identity: config.DefaultIdentityTemplate(),
			User:     config.DefaultUserTemplate(),
		}
	}
	logger.Info().Msg("loaded templates")

	// Build system prompt from templates
	systemPrompt := templates.Identity + "\n\n" + templates.User

	// Create Ollama client
	ollama := NewOllamaClient(ollamaURL, model, llmCallLogger)

	// Load external tools
	externalTools, toolStatuses, err := config.LoadAndCheckTools()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load external tools")
	} else {
		for name, status := range toolStatuses {
			if status.Available {
				logger.Info().Str("tool", name).Msg("external tool available")
			} else {
				logEvent := logger.Warn().
					Str("tool", name).
					Str("reason", status.Message).
					Int("exit_code", status.ExitCode)
				if status.Stdout != "" {
					logEvent = logEvent.Str("stdout", status.Stdout)
				}
				if status.Stderr != "" {
					logEvent = logEvent.Str("stderr", status.Stderr)
				}
				logEvent.Msg("external tool not available")
			}
		}
	}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create schema cache for dynamic tool discovery
	schemaCache, err := config.NewSchemaCache()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to create schema cache")
	}

	// Register discovery tools (always available)
	listCmdTool := tools.NewListCommandsTool(settings, externalTools, schemaCache)
	registry.Register(listCmdTool)
	logger.Info().Msg("registered list_available_commands tool")

	getSchemaTool := tools.NewGetCommandSchemaTool(settings, schemaCache, ollama)
	registry.Register(getSchemaTool)
	logger.Info().Msg("registered get_command_schema tool")

	// Register shell tool if enabled
	var shellTool *tools.ShellTool
	if settings.Tools.Shell.Enabled {
		if len(externalTools) > 0 {
			shellTool = tools.NewShellToolWithExternalTools(settings, externalTools)
		} else {
			shellTool = tools.NewShellTool(settings)
		}
		registry.Register(shellTool)
		logger.Info().Msg("registered shell tool")
	}

	// Register write tool if enabled
	if settings.Tools.Write.Enabled {
		writeTool := tools.NewWriteTool(settings)
		registry.Register(writeTool)
		logger.Info().Msg("registered write tool")
	}

	// Add external tools info to system prompt
	if shellTool != nil {
		externalToolsPrompt := shellTool.GetExternalToolsPrompt()
		if externalToolsPrompt != "" {
			systemPrompt += "\n" + externalToolsPrompt
		}
	}

	// Create agent with system prompt from templates
	agnt := agent.NewAgent(ollama, registry, logger, systemPrompt)

	// Create handler with shell tool for smart discovery
	handler := NewHandler(agnt, shellTool, logger)

	return &Server{
		port:      port,
		ollama:    ollama,
		handler:   handler,
		registry:  registry,
		settings:  settings,
		logger:    logger,
		logCloser: logCloser,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow local connections
			},
		},
	}
}

// Run starts the server and blocks until shutdown
func (s *Server) Run() error {
	mux := http.NewServeMux()

	// HTTP endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/shutdown", s.handleShutdown)
	mux.HandleFunc("/history", s.handleHistory)
	mux.HandleFunc("/context", s.handleContext)
	mux.HandleFunc("/tool/run", s.handleToolRun)
	mux.HandleFunc("/tool/list", s.handleToolList)

	// WebSocket endpoints
	mux.HandleFunc("/ws/chat", s.handleWSChat)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	done := make(chan bool)
	s.quit = make(chan os.Signal, 1)
	signal.Notify(s.quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-s.quit
		s.logger.Info().Msg("shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			s.logger.Error().Err(err).Msg("server shutdown error")
		}
		close(done)
	}()

	s.logger.Info().
		Int("port", s.port).
		Str("model", s.ollama.Model()).
		Msg("starting daemon server")

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	<-done
	s.logger.Info().Msg("server stopped")

	// Close log file
	if s.logCloser != nil {
		_ = s.logCloser.Close()
	}

	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	healthy, _ := s.ollama.Health(ctx)

	resp := &api.StatusResponse{
		Healthy: healthy,
		Model:   s.ollama.Model(),
		Version: Version,
	}

	data, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(data)
}

func (s *Server) handleWSChat(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to upgrade connection")
		return
	}

	s.logger.Info().Str("remote", r.RemoteAddr).Msg("new chat connection")
	s.handler.HandleChat(conn)
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.logger.Info().Msg("shutdown requested via API")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("shutting down"))

	// Trigger shutdown in background to allow response to be sent
	go func() {
		s.quit <- syscall.SIGTERM
	}()
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		resp := &api.ContextResponse{
			Context: s.handler.FullContext(),
		}
		data, err := proto.Marshal(resp)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(data)

	case http.MethodPost:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		var req api.ContextRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		s.handler.SetContext(req.Context)
		s.logger.Info().Str("context", req.Context).Msg("context updated")
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	history := s.handler.History()

	resp := &api.HistoryResponse{
		Messages: make([]*api.HistoryMessage, 0, len(history)),
	}

	for _, msg := range history {
		var role api.Role
		switch msg.Role {
		case "user":
			role = api.Role_USER
		case "assistant":
			role = api.Role_ASSISTANT
		default:
			continue // Skip system and tool messages
		}
		resp.Messages = append(resp.Messages, &api.HistoryMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	data, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(data)
}

func (s *Server) handleToolRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req api.ToolRunRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.logger.Info().Str("tool", req.Name).Str("args", req.Arguments).Msg("executing tool directly")

	// Parse arguments from JSON
	var args map[string]any
	if req.Arguments != "" {
		if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
			resp := &api.ToolRunResponse{
				Success: false,
				Error:   fmt.Sprintf("invalid arguments JSON: %v", err),
			}
			s.sendToolResponse(w, resp)
			return
		}
	} else {
		args = make(map[string]any)
	}

	// Execute the tool
	output, err := s.registry.Execute(req.Name, args)

	resp := &api.ToolRunResponse{
		Output:  output,
		Success: err == nil,
	}
	if err != nil {
		resp.Error = err.Error()
	}

	s.sendToolResponse(w, resp)
}

func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	toolList := s.registry.List()

	resp := &api.ToolListResponse{
		Tools: make([]*api.ToolInfo, 0, len(toolList)),
	}

	for _, t := range toolList {
		resp.Tools = append(resp.Tools, &api.ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
		})
	}

	respData, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(respData)
}

func (s *Server) sendToolResponse(w http.ResponseWriter, resp *api.ToolRunResponse) {
	data, err := proto.Marshal(resp)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(data)
}
