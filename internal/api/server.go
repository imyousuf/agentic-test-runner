package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// Server is the HTTP server for browser control.
type Server struct {
	browser    *browser.Browser
	httpServer *http.Server
	endpoint   string
	mux        *http.ServeMux
}

// ServerConfig holds configuration for the server.
type ServerConfig struct {
	Port       int
	BrowserCfg config.BrowserConfig
}

// NewServer creates a new browser control server.
func NewServer(cfg ServerConfig) (*Server, error) {
	b, err := browser.New(cfg.BrowserCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create browser: %w", err)
	}

	s := &Server{
		browser: b,
		mux:     http.NewServeMux(),
	}

	s.registerRoutes()

	return s, nil
}

// Start starts the server and browser.
func (s *Server) Start(ctx context.Context, port int) error {
	// Launch browser
	if err := s.browser.Launch(ctx); err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	// Find available port if not specified
	if port == 0 {
		port = 9333 // Default port
	}

	// Create listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.browser.Close()
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	s.endpoint = fmt.Sprintf("http://localhost:%d", actualPort)

	s.httpServer = &http.Server{
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Save state
	state := &BrowserState{
		PID:       os.Getpid(),
		Endpoint:  s.endpoint,
		StartedAt: time.Now(),
	}
	if err := SaveState(state); err != nil {
		s.browser.Close()
		if closeErr := listener.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close listener: %v\n", closeErr)
		}
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Start server
	go func() {
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	return nil
}

// Endpoint returns the server endpoint URL.
func (s *Server) Endpoint() string {
	return s.endpoint
}

// Wait waits for shutdown signal and gracefully shuts down.
func (s *Server) Wait() {
	sigCh := make(chan os.Signal, 1)
	registerShutdownSignals(sigCh)
	<-sigCh

	s.Shutdown()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Warning: HTTP server shutdown error: %v\n", err)
		}
	}

	if s.browser != nil {
		s.browser.Close()
	}

	RemoveState()
}

// Browser returns the browser instance.
func (s *Server) Browser() *browser.Browser {
	return s.browser
}

// StartDaemon starts the server as a background daemon process.
func StartDaemon(port int) (*BrowserState, error) {
	// Check if already running
	existing, err := GetRunningState()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("browser server already running (PID: %d, endpoint: %s)", existing.PID, existing.Endpoint)
	}

	// Get current executable
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build command arguments
	args := []string{"browser", "serve"}
	if port != 0 {
		args = append(args, "--port", fmt.Sprintf("%d", port))
	}

	// Open log file for daemon output (helps debug startup issues)
	logPath, err := LogFilePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get log file path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}
	// Note: logFile is intentionally not closed here - daemon inherits and uses it

	// Start daemon process
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent process (platform-specific)
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for state file to appear (with timeout)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		state, err := GetRunningState()
		if err != nil {
			return nil, err
		}
		if state != nil {
			return state, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("daemon started but state file not created (timeout). Check logs at: %s", logPath)
}

// StopDaemon stops the running daemon.
func StopDaemon() error {
	state, err := GetRunningState()
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("no browser server running")
	}

	// Send shutdown request
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(state.Endpoint+"/api/v1/shutdown", "application/json", nil)
	if err != nil {
		// If request fails, try to kill the process directly
		process, err := os.FindProcess(state.PID)
		if err != nil {
			return fmt.Errorf("failed to find process: %w", err)
		}
		if err := terminateProcess(process); err != nil {
			return fmt.Errorf("failed to terminate process: %w", err)
		}
		RemoveState()
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown request failed: %s", resp.Status)
	}

	// Wait for process to exit
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(state.PID) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process did not exit after shutdown")
}

// APIResponse is a standard API response.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeSuccess writes a success response.
func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: data})
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, APIResponse{Success: false, Error: message})
}
