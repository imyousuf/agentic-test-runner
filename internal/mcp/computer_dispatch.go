package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
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
		return computerScreenshot(c, args)

	case "computer_click":
		x, y := getInt(args, "x"), getInt(args, "y")
		button := computer.MouseButton(getStringOrDefault(args, "button", "left"))
		double := getBool(args, "double")
		if err := c.Click(ctx, x, y, button, double, getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Clicked (%s%s) at (%d, %d)", button, doubleSuffix(double), x, y), nil

	case "computer_double_click":
		x, y := getInt(args, "x"), getInt(args, "y")
		if err := c.Click(ctx, x, y, computer.ButtonLeft, true, getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Double-clicked at (%d, %d)", x, y), nil

	case "computer_right_click":
		x, y := getInt(args, "x"), getInt(args, "y")
		if err := c.Click(ctx, x, y, computer.ButtonRight, false, getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Right-clicked at (%d, %d)", x, y), nil

	case "computer_move":
		x, y := getInt(args, "x"), getInt(args, "y")
		if err := c.MoveTo(ctx, x, y, getBool(args, "smooth"), getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Moved to (%d, %d)", x, y), nil

	case "computer_drag":
		fx, fy := getInt(args, "from_x"), getInt(args, "from_y")
		tx, ty := getInt(args, "to_x"), getInt(args, "to_y")
		btn := computer.MouseButton(getStringOrDefault(args, "button", "left"))
		if err := c.Drag(ctx, fx, fy, tx, ty, btn, getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Dragged from (%d, %d) to (%d, %d)", fx, fy, tx, ty), nil

	case "computer_scroll":
		dx, dy := getInt(args, "dx"), getInt(args, "dy")
		if err := c.Scroll(ctx, dx, dy); err != nil {
			return "", err
		}
		return fmt.Sprintf("Scrolled (%d, %d)", dx, dy), nil

	case "computer_hover":
		x, y := getInt(args, "x"), getInt(args, "y")
		if err := c.Hover(ctx, x, y, getDisplay(args)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Hovered at (%d, %d)", x, y), nil

	case "computer_type":
		text := getString(args, "text")
		if err := c.Type(ctx, text, getInt(args, "delay_ms")); err != nil {
			return "", err
		}
		return fmt.Sprintf("Typed %d characters", len(text)), nil

	case "computer_press_key":
		key := getString(args, "key")
		if err := c.PressKey(ctx, key); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %q", key), nil

	case "computer_key_chord":
		chord := getString(args, "chord")
		if err := c.KeyChord(ctx, chord); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed %q", chord), nil

	case "computer_position":
		x, y := c.Position()
		return fmt.Sprintf("Mouse position: (%d, %d)", x, y), nil

	case "computer_displays":
		w, h := c.ScreenSize()
		displays := c.Displays()
		return fmt.Sprintf("Primary: %dx%d. Displays: %s", w, h, jsonOrEmpty(displays)), nil

	case "computer_list_windows":
		wins, err := c.ListWindows()
		if err != nil {
			return "", err
		}
		return jsonOrEmpty(wins), nil

	case "computer_active_window":
		win, err := c.ActiveWindow()
		if err != nil {
			return "", err
		}
		return jsonOrEmpty(win), nil

	case "computer_focus_window":
		id := uint32(getInt(args, "id"))
		if err := c.FocusWindow(ctx, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("Focused window %d", id), nil

	case "computer_window_state":
		id := uint32(getInt(args, "id"))
		state := computer.WindowState(getString(args, "state"))
		if err := c.SetWindowState(ctx, id, state); err != nil {
			return "", err
		}
		return fmt.Sprintf("Window %d state -> %s", id, state), nil

	case "computer_move_window":
		id := uint32(getInt(args, "id"))
		x, y := getInt(args, "x"), getInt(args, "y")
		if err := c.MoveWindow(ctx, id, x, y); err != nil {
			return "", err
		}
		return fmt.Sprintf("Moved window %d to (%d, %d)", id, x, y), nil

	case "computer_resize_window":
		id := uint32(getInt(args, "id"))
		w, h := getInt(args, "width"), getInt(args, "height")
		if err := c.ResizeWindow(ctx, id, w, h); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resized window %d to %dx%d", id, w, h), nil

	case "computer_launch_app":
		appName := getString(args, "name")
		if err := c.LaunchApp(ctx, appName); err != nil {
			return "", err
		}
		return fmt.Sprintf("Launched %q", appName), nil

	case "computer_quit_app":
		appName := getString(args, "name")
		if err := c.QuitApp(ctx, appName); err != nil {
			return "", err
		}
		return fmt.Sprintf("Quit %q", appName), nil

	default:
		return "", fmt.Errorf("unknown computer tool: %s", name)
	}
}

func computerScreenshot(c *computer.Computer, args map[string]any) (string, error) {
	display := -1
	if v, ok := args["display"]; ok {
		if n, err := toInt(v); err == nil {
			display = n
		}
	}
	out := getString(args, "output")
	if out == "" {
		out = filepath.Join(os.TempDir(), fmt.Sprintf("atr-screenshot-%d.png", time.Now().UnixNano()))
	}
	png, err := c.Screenshot(display)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, png, 0o644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	return fmt.Sprintf("Screenshot saved to %s (%d bytes)", out, len(png)), nil
}

// ----- helpers -----

// getDisplay returns the display index from args, or computer.NoDisplay if
// absent or null. Distinguishes "missing" from "0" (display 0 is valid).
func getDisplay(args map[string]any) int {
	v, ok := args["display"]
	if !ok || v == nil {
		return computer.NoDisplay
	}
	if n, err := toInt(v); err == nil {
		return n
	}
	return computer.NoDisplay
}

func getInt(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	n, _ := toInt(v)
	return n
}

func getBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func getString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func getStringOrDefault(args map[string]any, key, def string) string {
	if v := getString(args, key); v != "" {
		return v
	}
	return def
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		var x int
		_, err := fmt.Sscanf(n, "%d", &x)
		return x, err
	}
	return 0, fmt.Errorf("not an integer: %v", v)
}

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
