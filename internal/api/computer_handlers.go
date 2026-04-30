package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/computer"
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
// other errors -> 500.
func abortStatus(err error) int {
	if err == computer.ErrAborted {
		return 499
	}
	return http.StatusInternalServerError
}

func (s *ComputerServer) handleComputerHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeSuccess(w, map[string]any{
		"running":              true,
		"endpoint":             s.endpoint,
		"mode":                 string(s.mode),
		"countdown_seconds":    s.computer.CountdownSeconds(),
		"approved_apps_count":  s.computer.ApprovedAppCount(),
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
	var req struct {
		Display int  `json:"display"`
		X       int  `json:"x"`
		Y       int  `json:"y"`
		Width   int  `json:"width"`
		Height  int  `json:"height"`
		Region  bool `json:"region"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	display := req.Display
	if display == 0 && r.URL.Query().Get("display") == "" {
		display = -1 // use default
	}
	var (
		png []byte
		err error
	)
	if req.Region {
		if req.Width <= 0 || req.Height <= 0 {
			writeError(w, http.StatusBadRequest, "region requires positive width and height")
			return
		}
		png, err = s.computer.ScreenshotRegion(display, req.X, req.Y, req.Width, req.Height)
	} else {
		png, err = s.computer.Screenshot(display)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, map[string]any{
		"image_base64": base64.StdEncoding.EncodeToString(png),
		"size_bytes":   len(png),
		"format":       "png",
	})
}

func (s *ComputerServer) handleComputerClick(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		X       int    `json:"x"`
		Y       int    `json:"y"`
		Button  string `json:"button"`
		Double  bool   `json:"double"`
		Display *int   `json:"display,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Button == "" {
		req.Button = "left"
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.Click(ctx, req.X, req.Y, computer.MouseButton(req.Button), req.Double, display); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"x": req.X, "y": req.Y, "display": display})
}

func (s *ComputerServer) handleComputerMove(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		X       int  `json:"x"`
		Y       int  `json:"y"`
		Smooth  bool `json:"smooth"`
		Display *int `json:"display,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.MoveTo(ctx, req.X, req.Y, req.Smooth, display); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"x": req.X, "y": req.Y, "display": display})
}

func (s *ComputerServer) handleComputerDrag(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		FromX   int    `json:"from_x"`
		FromY   int    `json:"from_y"`
		ToX     int    `json:"to_x"`
		ToY     int    `json:"to_y"`
		Button  string `json:"button"`
		Display *int   `json:"display,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Button == "" {
		req.Button = "left"
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.Drag(ctx, req.FromX, req.FromY, req.ToX, req.ToY, computer.MouseButton(req.Button), display); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"from": [2]int{req.FromX, req.FromY}, "to": [2]int{req.ToX, req.ToY}, "display": display})
}

func (s *ComputerServer) handleComputerScroll(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		DX int `json:"dx"`
		DY int `json:"dy"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.Scroll(ctx, req.DX, req.DY); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"dx": req.DX, "dy": req.DY})
}

func (s *ComputerServer) handleComputerHover(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		X       int  `json:"x"`
		Y       int  `json:"y"`
		Display *int `json:"display,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.Hover(ctx, req.X, req.Y, display); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"x": req.X, "y": req.Y, "display": display})
}

func (s *ComputerServer) handleComputerType(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Text    string `json:"text"`
		DelayMs int    `json:"delay_ms"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.Type(ctx, req.Text, req.DelayMs); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"chars": len(req.Text)})
}

func (s *ComputerServer) handleComputerKey(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.PressKey(ctx, req.Key); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"key": req.Key})
}

func (s *ComputerServer) handleComputerChord(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Chord string `json:"chord"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.KeyChord(ctx, req.Chord); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"chord": req.Chord})
}

func (s *ComputerServer) handleComputerPosition(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	x, y := s.computer.Position()
	writeSuccess(w, map[string]any{"x": x, "y": y})
}

func (s *ComputerServer) handleComputerDisplays(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	w2, h := s.computer.ScreenSize()
	writeSuccess(w, map[string]any{
		"primary":  map[string]int{"width": w2, "height": h},
		"displays": s.computer.Displays(),
	})
}

func (s *ComputerServer) handleComputerResetApprovals(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	s.computer.ResetApprovals()
	writeSuccess(w, map[string]any{"cleared": true})
}

func (s *ComputerServer) handleComputerListWindows(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	wins, err := s.computer.ListWindows()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, map[string]any{"windows": wins, "count": len(wins)})
}

func (s *ComputerServer) handleComputerActiveWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	win, err := s.computer.ActiveWindow()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, win)
}

func (s *ComputerServer) handleComputerFocusWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID uint32 `json:"id"`
	}
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
	if err := s.computer.FocusWindow(ctx, req.ID); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"id": req.ID})
}

func (s *ComputerServer) handleComputerWindowState(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID    uint32 `json:"id"`
		State string `json:"state"`
	}
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
	if err := s.computer.SetWindowState(ctx, req.ID, computer.WindowState(req.State)); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"id": req.ID, "state": req.State})
}

func (s *ComputerServer) handleComputerMoveWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID uint32 `json:"id"`
		X  int    `json:"x"`
		Y  int    `json:"y"`
	}
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
	if err := s.computer.MoveWindow(ctx, req.ID, req.X, req.Y); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"id": req.ID, "x": req.X, "y": req.Y})
}

func (s *ComputerServer) handleComputerResizeWindow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID     uint32 `json:"id"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
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
	if err := s.computer.ResizeWindow(ctx, req.ID, req.Width, req.Height); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"id": req.ID, "width": req.Width, "height": req.Height})
}

func (s *ComputerServer) handleComputerLaunchApp(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.LaunchApp(ctx, req.Name); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"launched": req.Name})
}

func (s *ComputerServer) handleComputerQuitApp(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()
	if err := s.computer.QuitApp(ctx, req.Name); err != nil {
		writeError(w, abortStatus(err), err.Error())
		return
	}
	writeSuccess(w, map[string]any{"quit": req.Name})
}

// handleComputerAsk runs an in-process agent loop that drives the desktop
// to accomplish the user's natural-language instruction. Mirrors handleAsk
// (browser) at internal/api/handlers.go.
func (s *ComputerServer) handleComputerAsk(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Instruction    string `json:"instruction"`
		MaxSteps       int    `json:"max_steps,omitempty"`
		TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	}
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

	llmCfg := s.appConfig.GetLLMConfig()
	llmClient, err := llm.NewClient(r.Context(), llmCfg)
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

	ctx, cancel := s.gatedContext(r.Context())
	defer cancel()

	start := time.Now()
	answer, err := askAgent.ComputerAsk(ctx, req.Instruction)
	duration := time.Since(start)

	if err != nil {
		writeError(w, abortStatus(err), fmt.Sprintf("computer ask failed: %v", err))
		return
	}

	writeSuccess(w, map[string]any{
		"answer":      answer,
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
