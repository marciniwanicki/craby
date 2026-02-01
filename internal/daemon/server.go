package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/marciniwanicki/crabby/internal/agent"
	"github.com/marciniwanicki/crabby/internal/api"
	"github.com/marciniwanicki/crabby/internal/config"
	"github.com/marciniwanicki/crabby/internal/tools"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

const Version = "0.1.0"

// Server represents the daemon server
type Server struct {
	port     int
	ollama   *OllamaClient
	handler  *Handler
	settings *config.Settings
	logger   zerolog.Logger
	upgrader websocket.Upgrader
	quit     chan os.Signal
}

// NewServer creates a new daemon server
func NewServer(port int, ollamaURL, model string) *Server {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

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
	ollama := NewOllamaClient(ollamaURL, model)

	// Load external tools
	externalTools, toolStatuses, err := config.LoadAndCheckTools()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load external tools")
	} else {
		for name, status := range toolStatuses {
			if status.Available {
				logger.Info().Str("tool", name).Msg("external tool available")
			} else {
				logger.Warn().Str("tool", name).Str("reason", status.Message).Msg("external tool not available")
			}
		}
	}

	// Create tool registry
	registry := tools.NewRegistry()

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

	// Create handler
	handler := NewHandler(agnt, logger)

	return &Server{
		port:     port,
		ollama:   ollama,
		handler:  handler,
		settings: settings,
		logger:   logger,
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
