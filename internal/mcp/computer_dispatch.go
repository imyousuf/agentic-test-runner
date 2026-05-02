package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/agent"
	"github.com/imyousuf/agentic-test-runner/internal/computer"
	"github.com/imyousuf/agentic-test-runner/internal/ops"
	"github.com/imyousuf/agentic-test-runner/pkg/llm"
)

// ensureComputer lazy-initializes s.computer using the configured countdown
// mode (or "off" if not set, since MCP runs as a Claude Code subprocess where
// the user controls invocation).
func (s *Server) ensureComputer() (*computer.Computer, error) {
	if s.computer != nil {
		return s.computer, nil
	}
	mode := computer.ModeOff
	seconds := 0
	if s.appConfig != nil && s.appConfig.Computer.Countdown.Mode != "" {
		mode = computer.Mode(s.appConfig.Computer.Countdown.Mode)
		seconds = s.appConfig.Computer.Countdown.Seconds
	}
	if mode != computer.ModeOff && seconds < 1 {
		seconds = 3
	}
	c, err := computer.New(computer.Config{
		CountdownMode:    mode,
		CountdownSeconds: seconds,
		Output:           os.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create computer: %w", err)
	}
	s.computer = c
	return c, nil
}

// isComputerTool returns true if name is a known computer_* MCP tool.
func isComputerTool(name string) bool {
	return strings.HasPrefix(name, "computer_")
}

// executeComputerTool dispatches one of the computer_* tools.
func (s *Server) executeComputerTool(ctx context.Context, name string, args map[string]any) (string, error) {
	c, err := s.ensureComputer()
	if err != nil {
		return "", err
	}

	switch name {
	case "computer_screenshot":
		return computerScreenshotMCP(ctx, c, args)

	case "computer_click":
		var req ops.ComputerClickRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		button := req.Button
		if button == "" {
			button = "left"
		}
		res, err := ops.ComputerClick(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Clicked (%s%s) at (%d, %d)", button, doubleSuffix(req.DoubleClick), res.X, res.Y), nil

	case "computer_double_click":
		var req ops.ComputerClickRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		req.DoubleClick = true
		res, err := ops.ComputerClick(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Double-clicked at (%d, %d)", res.X, res.Y), nil

	case "computer_right_click":
		var req ops.ComputerClickRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		req.RightClick = true
		req.DoubleClick = false
		res, err := ops.ComputerClick(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Right-clicked at (%d, %d)", res.X, res.Y), nil

	case "computer_move":
		var req ops.ComputerMoveRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerMove(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Moved to (%d, %d)", res.X, res.Y), nil

	case "computer_drag":
		var req ops.ComputerDragRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerDrag(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Dragged from (%d, %d) to (%d, %d)", res.From[0], res.From[1], res.To[0], res.To[1]), nil

	case "computer_scroll":
		var req ops.ComputerScrollRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerScroll(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Scrolled (%d, %d)", res.DX, res.DY), nil

	case "computer_hover":
		var req ops.ComputerHoverRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerHover(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Hovered at (%d, %d)", res.X, res.Y), nil

	case "computer_type":
		var req ops.ComputerTypeRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerType(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Typed %d characters", res.Chars), nil

	case "computer_press_key":
		var req ops.ComputerPressKeyRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerPressKey(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %q", res.Key), nil

	case "computer_key_chord":
		var req ops.ComputerKeyChordRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerKeyChord(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %q", res.Chord), nil

	case "computer_position":
		res, err := ops.ComputerPosition(ctx, c)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Mouse position: (%d, %d)", res.X, res.Y), nil

	case "computer_displays":
		res, err := ops.ComputerDisplays(ctx, c)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Primary: %dx%d. Displays: %s", res.Primary.Width, res.Primary.Height, jsonOrEmpty(res.Displays)), nil

	case "computer_approvals_clear":
		if _, err := ops.ComputerApprovalsClear(ctx, c); err != nil {
			return "", err
		}
		return "Cleared per-app approval cache", nil

	case "computer_list_windows":
		res, err := ops.ComputerListWindows(ctx, c)
		if err != nil {
			return "", err
		}
		return jsonOrEmpty(res.Windows), nil

	case "computer_active_window":
		win, err := ops.ComputerActiveWindow(ctx, c)
		if err != nil {
			return "", err
		}
		return jsonOrEmpty(win), nil

	case "computer_focus_window":
		var req ops.ComputerFocusWindowRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerFocusWindow(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Focused window %d", res.ID), nil

	case "computer_window_state":
		var req ops.ComputerWindowStateRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerWindowState(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Window %d state -> %s", res.ID, res.State), nil

	case "computer_move_window":
		var req ops.ComputerMoveWindowRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerMoveWindow(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Moved window %d to (%d, %d)", res.ID, res.X, res.Y), nil

	case "computer_resize_window":
		var req ops.ComputerResizeWindowRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerResizeWindow(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Resized window %d to %dx%d", res.ID, res.Width, res.Height), nil

	case "computer_launch_app":
		var req ops.ComputerLaunchAppRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerLaunchApp(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Launched %q", res.Launched), nil

	case "computer_quit_app":
		var req ops.ComputerQuitAppRequest
		if err := ops.MapToStruct(args, &req); err != nil {
			return "", err
		}
		res, err := ops.ComputerQuitApp(ctx, c, req)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Quit %q", res.Quit), nil

	case "computer_ask":
		return s.executeComputerAsk(ctx, args)

	default:
		return "", fmt.Errorf("unknown computer tool: %s", name)
	}
}

// executeComputerAsk runs an in-process agent loop using the daemon's
// configured LLM. Mirrors handleComputerAsk in internal/api but reachable
// from MCP clients (e.g., Claude Code).
func (s *Server) executeComputerAsk(ctx context.Context, args map[string]any) (string, error) {
	var req ops.ComputerAskRequest
	if err := ops.MapToStruct(args, &req); err != nil {
		return "", err
	}
	if req.Instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}
	if s.appConfig == nil {
		return "", fmt.Errorf("LLM not configured: app config not provided to MCP server")
	}
	if err := s.appConfig.ValidateForLLM(); err != nil {
		return "", fmt.Errorf("LLM configuration error: %w", err)
	}

	c, err := s.ensureComputer()
	if err != nil {
		return "", err
	}

	llmCfg := s.appConfig.GetLLMConfig()
	llmClient, err := llm.NewClient(ctx, llmCfg)
	if err != nil {
		return "", fmt.Errorf("failed to create LLM client: %w", err)
	}
	defer llmClient.Close()

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	askAgent := agent.NewComputerAskAgent(agent.ComputerAskConfig{
		LLMClient:     llmClient,
		Computer:      c,
		MaxIterations: req.MaxSteps,
		Timeout:       timeout,
		Verbose:       true,
	})

	runner := func(ctx context.Context, instruction string) (string, error) {
		return askAgent.ComputerAsk(ctx, instruction)
	}
	res, err := ops.ComputerAsk(ctx, runner, req)
	if err != nil {
		return "", err
	}
	return res.Answer, nil
}

// computerScreenshotMCP wraps ops.ComputerScreenshot for the MCP surface,
// which writes the PNG to disk and returns the file path. The MCP-only
// "output" arg controls the destination.
func computerScreenshotMCP(ctx context.Context, c *computer.Computer, args map[string]any) (string, error) {
	var req ops.ComputerScreenshotRequest
	if err := ops.MapToStruct(args, &req); err != nil {
		return "", err
	}
	// MCP convention: omitting display means "default display" (matching the
	// pre-migration helper that returned -1 when args["display"] was missing).
	if _, present := args["display"]; !present {
		req.UseDefaultDisplay = true
	}
	res, err := ops.ComputerScreenshot(ctx, c, req)
	if err != nil {
		return "", err
	}
	out := req.Output
	if out == "" {
		out = filepath.Join(os.TempDir(), fmt.Sprintf("atr-screenshot-%d.png", time.Now().UnixNano()))
	}
	if err := os.WriteFile(out, res.PNG, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	return fmt.Sprintf("Screenshot saved to %s (%d bytes)", out, len(res.PNG)), nil
}

// ----- helpers -----

func doubleSuffix(double bool) string {
	if double {
		return ", double"
	}
	return ""
}

func jsonOrEmpty(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
