package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// ComputerServer hosts the desktop computer-use REST API.
// It mirrors Server but is wired to a computer.Computer instead of a browser.
type ComputerServer struct {
	computer   *computer.Computer
	httpServer *http.Server
	endpoint   string
	mux        *http.ServeMux
	mode       computer.Mode
	appConfig  *config.Config // for LLM-powered features (computer ask)
}

// ComputerServerConfig holds configuration for the computer server.
type ComputerServerConfig struct {
	Port        int
	ComputerCfg config.ComputerConfig
	// AppConfig is the full ATR config used by LLM-powered features such
	// as `atr computer ask`. May be nil; ask endpoints will return an
	// error in that case.
	AppConfig *config.Config
}

// NewComputerServer creates a new computer-control server.
func NewComputerServer(cfg ComputerServerConfig) (*ComputerServer, error) {
	mode := computer.Mode(cfg.ComputerCfg.Countdown.Mode)
	if !mode.IsValid() {
		return nil, fmt.Errorf("invalid countdown mode %q", cfg.ComputerCfg.Countdown.Mode)
	}
	c, err := computer.New(computer.Config{
		CountdownMode:    mode,
		CountdownSeconds: cfg.ComputerCfg.Countdown.Seconds,
		GUIEnabled:       cfg.ComputerCfg.GUI.Enabled,
		DefaultDisplay:   cfg.ComputerCfg.Display,
		Output:           os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create computer: %w", err)
	}
	s := &ComputerServer{
		computer:  c,
		mux:       http.NewServeMux(),
		mode:      mode,
		appConfig: cfg.AppConfig,
	}
	s.registerComputerRoutes()
	return s, nil
}

// Start starts the computer server.
func (s *ComputerServer) Start(ctx context.Context, port int) error {
	if port == 0 {
		port = 9334
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		s.computer.Close()
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	s.endpoint = fmt.Sprintf("http://localhost:%d", actualPort)

	s.httpServer = &http.Server{
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	state := &ComputerState{
		PID:       os.Getpid(),
		Endpoint:  s.endpoint,
		Mode:      string(s.mode),
		StartedAt: time.Now(),
	}
	if err := SaveComputerState(state); err != nil {
		s.computer.Close()
		if closeErr := listener.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close listener: %v\n", closeErr)
		}
		return fmt.Errorf("failed to save computer state: %w", err)
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Computer server error: %v\n", err)
		}
	}()
	return nil
}

// Endpoint returns the server endpoint URL.
func (s *ComputerServer) Endpoint() string { return s.endpoint }

// Wait blocks until a shutdown signal arrives, then shuts down gracefully.
func (s *ComputerServer) Wait() {
	sigCh := make(chan os.Signal, 1)
	registerShutdownSignals(sigCh)
	<-sigCh
	s.Shutdown()
}

// Shutdown gracefully shuts down the server.
func (s *ComputerServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Warning: computer HTTP server shutdown error: %v\n", err)
		}
	}
	if s.computer != nil {
		s.computer.Close()
	}
	RemoveComputerState()
}

// Computer returns the underlying computer instance.
func (s *ComputerServer) Computer() *computer.Computer { return s.computer }

// StartComputerDaemon starts the computer server as a background daemon process.
func StartComputerDaemon(port int) (*ComputerState, error) {
	existing, err := GetRunningComputerState()
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("computer server already running (PID: %d, endpoint: %s)", existing.PID, existing.Endpoint)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	args := []string{"computer", "serve"}
	if port != 0 {
		args = append(args, "--port", fmt.Sprintf("%d", port))
	}
	logPath, err := ComputerLogFilePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get log file path: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	setSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start computer daemon: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		state, err := GetRunningComputerState()
		if err != nil {
			return nil, err
		}
		if state != nil {
			return state, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("computer daemon started but state file not created (timeout). Check logs at: %s", logPath)
}

// StopComputerDaemon stops the running computer daemon.
func StopComputerDaemon() error {
	state, err := GetRunningComputerState()
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("no computer server running")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(state.Endpoint+"/api/v1/computer/shutdown", "application/json", nil)
	if err != nil {
		process, ferr := os.FindProcess(state.PID)
		if ferr != nil {
			return fmt.Errorf("failed to find process: %w", ferr)
		}
		if terr := terminateProcess(process); terr != nil {
			return fmt.Errorf("failed to terminate process: %w", terr)
		}
		RemoveComputerState()
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown request failed: %s", resp.Status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(state.PID) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("computer process did not exit after shutdown")
}
