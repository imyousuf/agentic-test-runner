package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/internal/ops"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// registerComputerRoutes wires the computer endpoints into s.mux.
func (s *ComputerServer) registerComputerRoutes() {
	s.mux.HandleFunc("/api/v1/computer/health", s.handleComputerHealth)
	s.mux.HandleFunc("/api/v1/computer/shutdown", s.handleComputerShutdown)
	s.mux.HandleFunc("/api/v1/computer/screenshot", s.handleComputerScreenshot)
	s.mux.HandleFunc("/api/v1/computer/click", s.handleComputerClick)
	s.mux.HandleFunc("/api/v1/computer/move", s.handleComputerMove)
	s.mux.HandleFunc("/api/v1/computer/drag", s.handleComputerDrag)
	s.mux.HandleFunc("/api/v1/computer/scroll", s.handleComputerScroll)
	s.mux.HandleFunc("/api/v1/computer/hover", s.handleComputerHover)
	s.mux.HandleFunc("/api/v1/computer/type", s.handleComputerType)
	s.mux.HandleFunc("/api/v1/computer/key", s.handleComputerKey)
	s.mux.HandleFunc("/api/v1/computer/chord", s.handleComputerChord)
	s.mux.HandleFunc("/api/v1/computer/position", s.handleComputerPosition)
	s.mux.HandleFunc("/api/v1/computer/displays", s.handleComputerDisplays)
	s.mux.HandleFunc("/api/v1/computer/approvals/clear", s.handleComputerResetApprovals)
	s.mux.HandleFunc("/api/v1/computer/windows", s.handleComputerListWindows)
	s.mux.HandleFunc("/api/v1/computer/window/active", s.handleComputerActiveWindow)
	s.mux.HandleFunc("/api/v1/computer/window/focus", s.handleComputerFocusWindow)
	s.mux.HandleFunc("/api/v1/computer/window/state", s.handleComputerWindowState)
	s.mux.HandleFunc("/api/v1/computer/window/move", s.handleComputerMoveWindow)
	s.mux.HandleFunc("/api/v1/computer/window/resize", s.handleComputerResizeWindow)
	s.mux.HandleFunc("/api/v1/computer/app/launch", s.handleComputerLaunchApp)
	s.mux.HandleFunc("/api/v1/computer/app/quit", s.handleComputerQuitApp)
	s.mux.HandleFunc("/api/v1/computer/ask", s.handleComputerAsk)
}

// requireMethod returns true if the request method matches; otherwise it
// writes a 405 and returns false.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return false
	}
	return true
}

// gatedContext returns a context that cancels when SIGINT/SIGTERM arrives,
// so a Ctrl+C in the daemon's terminal aborts the in-flight gate countdown.
func (s *ComputerServer) gatedContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

// abortStatus maps an action error to an HTTP status: aborted -> 499,
// other errors -> 500. Uses errors.Is so wrapped ErrAborted (e.g. via
// fmt.Errorf("%w")) still maps to 499.
func abortStatus(err error) int {
	if errors.Is(err, computer.ErrAborted) {
		return 499
	}
	return http.StatusInternalServerError
}

func (s *ComputerServer) handleComputerHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeSuccess(w, map[string]any{
		"running":             true,
		"endpoint":            s.endpoint,
		"mode":                string(s.mode),
		"countdown_seconds":   s.computer.CountdownSeconds(),
		"approved_apps_count": s.computer.ApprovedAppCount(),
	})
}

func (s *ComputerServer) handleComputerShutdown(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeSuccess(w, map[string]any{"shutting_down": true})
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Shutdown()
		os.Exit(0)
	}()
}

func (s *ComputerServer) handleComputerScreenshot(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerScreenshotRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// req.Display is *int: nil means "use the daemon's configured default
	// display"; explicit display=0 stays as display 0 (display-local coords
	// on the primary monitor). The ops layer handles the nil → -1 mapping.
	res, err := ops.ComputerScreenshot(r.Context(), s.computer, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, map[string]any{
		"image_base64": base64.StdEncoding.EncodeToString(res.PNG),
		"size_bytes":   len(res.PNG),
		"format":       res.Format,
	})
}

func (s *ComputerServer) handleComputerClick(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerClickRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerClick(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerMove(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerMoveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerMove(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerDrag(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerDragRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerDrag(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerScroll(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerScrollRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerScroll(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerHover(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerHoverRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerHover(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerType(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerType(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerKey(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerPressKeyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerPressKey(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerChord(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerKeyChordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerKeyChord(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerPosition(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	res, err := ops.ComputerPosition(r.Context(), s.computer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerDisplays(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	res, err := ops.ComputerDisplays(r.Context(), s.computer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerResetApprovals(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	res, err := ops.ComputerApprovalsClear(r.Context(), s.computer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerListWindows(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	res, err := ops.ComputerListWindows(r.Context(), s.computer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerActiveWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	res, err := ops.ComputerActiveWindow(r.Context(), s.computer)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerFocusWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerFocusWindowRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerFocusWindow(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerWindowState(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerWindowStateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == 0 || req.State == "" {
		writeError(w, http.StatusBadRequest, "id and state are required")
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerWindowState(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerMoveWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerMoveWindowRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == 0 {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerMoveWindow(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerResizeWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerResizeWindowRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == 0 || req.Width <= 0 || req.Height <= 0 {
		writeError(w, http.StatusBadRequest, "id, width, and height are required")
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerResizeWindow(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerLaunchApp(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerLaunchAppRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerLaunchApp(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

func (s *ComputerServer) handleComputerQuitApp(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerQuitAppRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	res, err := ops.ComputerQuitApp(ctx, s.computer, req)
	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, res)
}

// handleComputerAsk runs an in-process agent loop that drives the desktop
// to accomplish the user's natural-language instruction. Mirrors handleAsk
// (browser) at internal/api/handlers.go.
func (s *ComputerServer) handleComputerAsk(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ops.ComputerAskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Instruction == "" {
		writeError(w, http.StatusBadRequest, "instruction is required")
		return
	}
	if s.appConfig == nil {
		writeError(w, http.StatusInternalServerError, "LLM not configured: app config not provided to server")
		return
	}
	if err := s.appConfig.ValidateForLLM(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("LLM configuration error: %v", err))
		return
	}

	// Build the gated context FIRST so the LLM client lifetime is bounded
	// by the same context as the agent loop — Ctrl+C in the daemon's
	// terminal cancels everything in one shot.
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()

	llmCfg := s.appConfig.GetLLMConfig()
	llmClient, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create LLM client: %v", err))
		return
	}
	defer llmClient.Close()

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	askAgent := agent.NewComputerAskAgent(agent.ComputerAskConfig{
		LLMClient:     llmClient,
		Computer:      s.computer,
		MaxIterations: req.MaxSteps,
		Timeout:       timeout,
		Verbose:       true,
	})

	runner := func(ctx context.Context, instruction string) (string, error) {
		return askAgent.ComputerAsk(ctx, instruction)
	}

	start := time.Now()
	res, err := ops.ComputerAsk(ctx, runner, req)
	duration := time.Since(start)

	if err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}

	writeSuccess(w, map[string]any{
		"answer":      res.Answer,
		"duration_ms": duration.Milliseconds(),
		"backend":     string(llmClient.Provider()),
		"model":       llmClient.Model(),
	})
}

// decodeJSON decodes the request body into v. Empty body is OK.
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
