package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/imyousuf/agentic-test-runner/internal/api"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

var (
	computerJSONOutput      bool
	computerEndpoint        string
	computerPort            int
	computerCountdownMode   string
	computerCountdownSecs   int
	computerNoGUI           bool
	computerScreenshotOut   string
	computerScreenshotDisp  int
	computerScreenshotRgn   string
	computerClickButton     string
	computerClickDouble     bool
	computerMoveSmooth      bool
	computerDragFrom        string
	computerDragTo          string
	computerDragButton      string
	computerScrollDX        int
	computerScrollDY        int
	computerTypeDelayMs     int
)

func newComputerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "computer",
		Short: "Control the desktop (mouse, keyboard, screen, windows)",
		Long: `Control your desktop via an HTTP daemon backed by robotgo.

Run 'atr computer start' to launch the daemon, then use the subcommands to
take actions. By default, every action shows a 3-second countdown in the
daemon's terminal so you can intervene with Ctrl+C.

Linux requires X11 (Wayland is not supported in v1).`,
	}
	cmd.PersistentFlags().BoolVar(&computerJSONOutput, "json", false, "Output as JSON")
	cmd.PersistentFlags().StringVar(&computerEndpoint, "endpoint", "", "Server endpoint URL (default: from state file)")

	cmd.AddCommand(newComputerStartCmd())
	cmd.AddCommand(newComputerStopCmd())
	cmd.AddCommand(newComputerStatusCmd())
	cmd.AddCommand(newComputerServeCmd())
	cmd.AddCommand(newComputerScreenshotCmd())
	cmd.AddCommand(newComputerClickCmd())
	cmd.AddCommand(newComputerMoveCmd())
	cmd.AddCommand(newComputerDragCmd())
	cmd.AddCommand(newComputerScrollCmd())
	cmd.AddCommand(newComputerHoverCmd())
	cmd.AddCommand(newComputerTypeCmd())
	cmd.AddCommand(newComputerKeyCmd())
	cmd.AddCommand(newComputerChordCmd())
	cmd.AddCommand(newComputerPositionCmd())
	cmd.AddCommand(newComputerDisplaysCmd())
	cmd.AddCommand(newComputerResetApprovalsCmd())
	cmd.AddCommand(newComputerWindowCmd())
	cmd.AddCommand(newComputerAppCmd())

	return cmd
}

// ---------------- Daemon lifecycle ----------------

func newComputerStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start computer daemon",
		Long:  "Start the desktop-control daemon as a background process.",
		RunE:  runComputerStart,
	}
	cmd.Flags().IntVar(&computerPort, "port", 0, "Server port (default: 9334)")
	cmd.Flags().StringVar(&computerCountdownMode, "countdown-mode", "", "Countdown mode: per-request, per-app, or off")
	cmd.Flags().IntVar(&computerCountdownSecs, "countdown", 0, "Countdown seconds before each action (default: 3)")
	cmd.Flags().BoolVar(&computerNoGUI, "no-gui", false, "Disable the GUI countdown overlay")
	return cmd
}

func runComputerStart(cmd *cobra.Command, args []string) error {
	// Pass flag overrides to daemon via env vars.
	if computerCountdownMode != "" {
		_ = os.Setenv("ATR_COMPUTER_COUNTDOWN_MODE", computerCountdownMode)
	}
	if computerCountdownSecs > 0 {
		_ = os.Setenv("ATR_COMPUTER_COUNTDOWN_SECONDS", strconv.Itoa(computerCountdownSecs))
	}
	if computerNoGUI {
		_ = os.Setenv("ATR_COMPUTER_GUI_ENABLED", "false")
	}

	state, err := api.StartComputerDaemon(computerPort)
	if err != nil {
		return err
	}

	if computerJSONOutput {
		return outputJSON(map[string]any{
			"status":     "started",
			"endpoint":   state.Endpoint,
			"pid":        state.PID,
			"mode":       state.Mode,
			"started_at": state.StartedAt.Format(time.RFC3339),
		})
	}

	fmt.Println("Computer daemon started")
	fmt.Printf("  Endpoint: %s\n", state.Endpoint)
	fmt.Printf("  PID: %d\n", state.PID)
	fmt.Printf("  Mode: %s\n", state.Mode)
	if statePath, err := api.ComputerStateFilePath(); err == nil {
		fmt.Printf("  State: %s\n", statePath)
	}
	if logPath, err := api.ComputerLogFilePath(); err == nil {
		fmt.Printf("  Logs: %s\n", logPath)
	}
	fmt.Println()
	fmt.Println("Note: countdown messages are written to the daemon log file above.")
	fmt.Println("Use 'tail -f' on the log to watch, or run 'atr computer stop' to exit.")
	return nil
}

func newComputerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop computer daemon",
		RunE:  runComputerStop,
	}
}

func runComputerStop(cmd *cobra.Command, args []string) error {
	if err := api.StopComputerDaemon(); err != nil {
		return err
	}
	if computerJSONOutput {
		return outputJSON(map[string]string{"status": "stopped"})
	}
	fmt.Println("Computer daemon stopped")
	return nil
}

func newComputerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check computer daemon status",
		RunE:  runComputerStatus,
	}
}

func runComputerStatus(cmd *cobra.Command, args []string) error {
	state, err := api.GetRunningComputerState()
	if err != nil {
		return err
	}
	if state == nil {
		if computerJSONOutput {
			return outputJSON(map[string]any{"running": false})
		}
		fmt.Println("Computer daemon not running")
		return nil
	}
	if computerJSONOutput {
		return outputJSON(map[string]any{
			"running":    true,
			"endpoint":   state.Endpoint,
			"pid":        state.PID,
			"mode":       state.Mode,
			"started_at": state.StartedAt.Format(time.RFC3339),
		})
	}
	fmt.Println("Computer daemon running")
	fmt.Printf("  Endpoint: %s\n", state.Endpoint)
	fmt.Printf("  PID: %d\n", state.PID)
	fmt.Printf("  Mode: %s\n", state.Mode)
	fmt.Printf("  Started: %s\n", state.StartedAt.Format(time.RFC3339))
	return nil
}

func newComputerServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Run computer server (internal)",
		Hidden: true,
		RunE:   runComputerServe,
	}
	cmd.Flags().IntVar(&computerPort, "port", 9334, "Server port")
	return cmd
}

func runComputerServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	server, err := api.NewComputerServer(api.ComputerServerConfig{
		Port:        computerPort,
		ComputerCfg: cfg.Computer,
	})
	if err != nil {
		return err
	}
	if err := server.Start(context.Background(), computerPort); err != nil {
		return err
	}
	fmt.Printf("Computer server started at %s (mode=%s)\n", server.Endpoint(), cfg.Computer.Countdown.Mode)
	server.Wait()
	return nil
}

// ---------------- Primitives ----------------

func newComputerScreenshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "screenshot",
		Short: "Capture the screen and write to a file",
		RunE:  runComputerScreenshot,
	}
	cmd.Flags().StringVarP(&computerScreenshotOut, "output", "o", "", "Output file path (required)")
	cmd.Flags().IntVarP(&computerScreenshotDisp, "display", "d", -1, "Display index (default: configured default)")
	cmd.Flags().StringVar(&computerScreenshotRgn, "region", "", "Region X,Y,W,H (e.g. 0,0,800,600)")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func runComputerScreenshot(cmd *cobra.Command, args []string) error {
	body := map[string]any{
		"display": computerScreenshotDisp,
	}
	if computerScreenshotRgn != "" {
		x, y, w, h, err := parseRegion(computerScreenshotRgn)
		if err != nil {
			return err
		}
		body["region"] = true
		body["x"] = x
		body["y"] = y
		body["width"] = w
		body["height"] = h
	}
	resp, err := computerAPIRequestRaw("POST", "/computer/screenshot", body)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected response shape")
	}
	b64, _ := data["image_base64"].(string)
	png, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	if err := os.WriteFile(computerScreenshotOut, png, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	if computerJSONOutput {
		return outputJSON(map[string]any{
			"path":       computerScreenshotOut,
			"size_bytes": len(png),
		})
	}
	fmt.Printf("Wrote %d bytes to %s\n", len(png), computerScreenshotOut)
	return nil
}

func newComputerClickCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "click <x> <y>",
		Short: "Click at screen coordinates",
		Args:  cobra.ExactArgs(2),
		RunE:  runComputerClick,
	}
	cmd.Flags().StringVar(&computerClickButton, "button", "left", "Mouse button: left, right, center")
	cmd.Flags().BoolVar(&computerClickDouble, "double", false, "Double click")
	return cmd
}

func runComputerClick(cmd *cobra.Command, args []string) error {
	x, y, err := parseXY(args[0], args[1])
	if err != nil {
		return err
	}
	return computerAPIPost("/computer/click", map[string]any{
		"x":      x,
		"y":      y,
		"button": computerClickButton,
		"double": computerClickDouble,
	})
}

func newComputerMoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <x> <y>",
		Short: "Move mouse to screen coordinates",
		Args:  cobra.ExactArgs(2),
		RunE:  runComputerMove,
	}
	cmd.Flags().BoolVar(&computerMoveSmooth, "smooth", false, "Animate the move")
	return cmd
}

func runComputerMove(cmd *cobra.Command, args []string) error {
	x, y, err := parseXY(args[0], args[1])
	if err != nil {
		return err
	}
	return computerAPIPost("/computer/move", map[string]any{
		"x":      x,
		"y":      y,
		"smooth": computerMoveSmooth,
	})
}

func newComputerDragCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drag --from X,Y --to X,Y",
		Short: "Drag from one point to another",
		RunE:  runComputerDrag,
	}
	cmd.Flags().StringVar(&computerDragFrom, "from", "", "Start point as X,Y")
	cmd.Flags().StringVar(&computerDragTo, "to", "", "End point as X,Y")
	cmd.Flags().StringVar(&computerDragButton, "button", "left", "Mouse button: left, right, center")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func runComputerDrag(cmd *cobra.Command, args []string) error {
	fx, fy, err := parsePair(computerDragFrom)
	if err != nil {
		return fmt.Errorf("--from: %w", err)
	}
	tx, ty, err := parsePair(computerDragTo)
	if err != nil {
		return fmt.Errorf("--to: %w", err)
	}
	return computerAPIPost("/computer/drag", map[string]any{
		"from_x": fx, "from_y": fy,
		"to_x": tx, "to_y": ty,
		"button": computerDragButton,
	})
}

func newComputerScrollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scroll",
		Short: "Scroll at the current mouse position",
		RunE:  runComputerScroll,
	}
	cmd.Flags().IntVar(&computerScrollDX, "dx", 0, "Horizontal scroll amount")
	cmd.Flags().IntVar(&computerScrollDY, "dy", 0, "Vertical scroll amount (positive = up)")
	return cmd
}

func runComputerScroll(cmd *cobra.Command, args []string) error {
	if computerScrollDX == 0 && computerScrollDY == 0 {
		return fmt.Errorf("at least one of --dx or --dy must be non-zero")
	}
	return computerAPIPost("/computer/scroll", map[string]any{
		"dx": computerScrollDX,
		"dy": computerScrollDY,
	})
}

func newComputerHoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hover <x> <y>",
		Short: "Move mouse to coordinates without clicking",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			x, y, err := parseXY(args[0], args[1])
			if err != nil {
				return err
			}
			return computerAPIPost("/computer/hover", map[string]any{"x": x, "y": y})
		},
	}
}

func newComputerTypeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "type <text>",
		Short: "Type text",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/type", map[string]any{
				"text":     args[0],
				"delay_ms": computerTypeDelayMs,
			})
		},
	}
	cmd.Flags().IntVar(&computerTypeDelayMs, "delay-ms", 0, "Delay between key events")
	return cmd
}

func newComputerKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "key <key>",
		Short: "Press a single named key (e.g. enter, esc, f5)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/key", map[string]any{"key": args[0]})
		},
	}
}

func newComputerChordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chord <chord>",
		Short: "Press a key combination (e.g. ctrl+shift+t)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/chord", map[string]any{"chord": args[0]})
		},
	}
}

func newComputerPositionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "position",
		Short: "Print the current mouse position",
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIGet("/computer/position")
		},
	}
}

func newComputerDisplaysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "displays",
		Short: "List attached displays and the primary screen size",
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIGet("/computer/displays")
		},
	}
}

func newComputerResetApprovalsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset-approvals",
		Short: "Clear the per-app approval cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/approvals/clear", nil)
		},
	}
}

// ---------------- Window management ----------------

var (
	computerWindowTitle string
	computerWindowID    uint32
	computerWindowPos   string
	computerWindowSize  string
)

func newComputerWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "window",
		Short: "Manage windows (list, focus, minimize, maximize, restore, close, move, resize)",
	}
	cmd.AddCommand(newComputerWindowListCmd())
	cmd.AddCommand(newComputerWindowActiveCmd())
	cmd.AddCommand(newComputerWindowFocusCmd())
	cmd.AddCommand(newComputerWindowStateCmd("minimize"))
	cmd.AddCommand(newComputerWindowStateCmd("maximize"))
	cmd.AddCommand(newComputerWindowStateCmd("restore"))
	cmd.AddCommand(newComputerWindowStateCmd("close"))
	cmd.AddCommand(newComputerWindowMoveCmd())
	cmd.AddCommand(newComputerWindowResizeCmd())
	return cmd
}

func newComputerWindowListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all top-level windows",
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIGet("/computer/windows")
		},
	}
}

func newComputerWindowActiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "active",
		Short: "Show the currently focused window",
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIGet("/computer/window/active")
		},
	}
}

// resolveWindowID returns the window ID to operate on. If --id is set, use it.
// Otherwise, use --title to find the first matching window.
func resolveWindowID() (uint32, error) {
	if computerWindowID != 0 {
		return computerWindowID, nil
	}
	if computerWindowTitle == "" {
		return 0, fmt.Errorf("either --id or --title is required")
	}
	resp, err := computerAPIRequestRaw("GET", "/computer/windows", nil)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("%s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("unexpected response shape")
	}
	wins, _ := data["windows"].([]any)
	pat := strings.ToLower(computerWindowTitle)
	for _, w := range wins {
		m, _ := w.(map[string]any)
		title, _ := m["title"].(string)
		appName, _ := m["app_name"].(string)
		if strings.Contains(strings.ToLower(title), pat) || strings.Contains(strings.ToLower(appName), pat) {
			if idF, ok := m["id"].(float64); ok {
				return uint32(idF), nil
			}
		}
	}
	return 0, fmt.Errorf("no window matched title or app pattern %q", computerWindowTitle)
}

func addWindowSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().Uint32Var(&computerWindowID, "id", 0, "Window ID")
	cmd.Flags().StringVar(&computerWindowTitle, "title", "", "Substring to match window title (case-insensitive)")
}

func newComputerWindowFocusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Focus a window by ID or title",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolveWindowID()
			if err != nil {
				return err
			}
			return computerAPIPost("/computer/window/focus", map[string]any{"id": id})
		},
	}
	addWindowSelectionFlags(cmd)
	return cmd
}

func newComputerWindowStateCmd(state string) *cobra.Command {
	short := map[string]string{
		"minimize": "Minimize a window by ID or title",
		"maximize": "Maximize a window by ID or title",
		"restore":  "Restore a window from minimize/maximize",
		"close":    "Close a window by ID or title",
	}[state]
	cmd := &cobra.Command{
		Use:   state,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolveWindowID()
			if err != nil {
				return err
			}
			return computerAPIPost("/computer/window/state", map[string]any{"id": id, "state": state})
		},
	}
	addWindowSelectionFlags(cmd)
	return cmd
}

func newComputerWindowMoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move",
		Short: "Move a window to (X,Y)",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolveWindowID()
			if err != nil {
				return err
			}
			x, y, err := parsePair(computerWindowPos)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			return computerAPIPost("/computer/window/move", map[string]any{"id": id, "x": x, "y": y})
		},
	}
	addWindowSelectionFlags(cmd)
	cmd.Flags().StringVar(&computerWindowPos, "to", "", "Target position as X,Y")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newComputerWindowResizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resize",
		Short: "Resize a window to (W,H)",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := resolveWindowID()
			if err != nil {
				return err
			}
			w, h, err := parsePair(computerWindowSize)
			if err != nil {
				return fmt.Errorf("--size: %w", err)
			}
			return computerAPIPost("/computer/window/resize", map[string]any{"id": id, "width": w, "height": h})
		},
	}
	addWindowSelectionFlags(cmd)
	cmd.Flags().StringVar(&computerWindowSize, "size", "", "Target size as W,H")
	_ = cmd.MarkFlagRequired("size")
	return cmd
}

// ---------------- App management ----------------

func newComputerAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Launch or quit applications",
	}
	cmd.AddCommand(newComputerAppLaunchCmd())
	cmd.AddCommand(newComputerAppQuitCmd())
	return cmd
}

func newComputerAppLaunchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "launch <name>",
		Short: "Launch an application by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/app/launch", map[string]any{"name": args[0]})
		},
	}
}

func newComputerAppQuitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quit <name>",
		Short: "Quit an application by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return computerAPIPost("/computer/app/quit", map[string]any{"name": args[0]})
		},
	}
}

// ---------------- Helpers ----------------

func parseXY(xs, ys string) (int, int, error) {
	x, err := strconv.Atoi(xs)
	if err != nil {
		return 0, 0, fmt.Errorf("x must be an integer: %w", err)
	}
	y, err := strconv.Atoi(ys)
	if err != nil {
		return 0, 0, fmt.Errorf("y must be an integer: %w", err)
	}
	return x, y, nil
}

func parsePair(s string) (int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected X,Y, got %q", s)
	}
	return parseXY(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
}

func parseRegion(s string) (x, y, w, h int, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		err = fmt.Errorf("expected X,Y,W,H, got %q", s)
		return
	}
	if x, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
		err = fmt.Errorf("region X: %w", err)
		return
	}
	if y, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
		err = fmt.Errorf("region Y: %w", err)
		return
	}
	if w, err = strconv.Atoi(strings.TrimSpace(parts[2])); err != nil {
		err = fmt.Errorf("region W: %w", err)
		return
	}
	if h, err = strconv.Atoi(strings.TrimSpace(parts[3])); err != nil {
		err = fmt.Errorf("region H: %w", err)
		return
	}
	return
}

func getComputerEndpoint() (string, error) {
	if computerEndpoint != "" {
		return computerEndpoint, nil
	}
	state, err := api.GetRunningComputerState()
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", fmt.Errorf("computer daemon not running. Start with: atr computer start")
	}
	return state.Endpoint, nil
}

func computerAPIGet(path string) error {
	return computerAPIRequest("GET", path, nil)
}

func computerAPIPost(path string, body any) error {
	return computerAPIRequest("POST", path, body)
}

func computerAPIRequestRaw(method, path string, body any) (*api.APIResponse, error) {
	endpoint, err := getComputerEndpoint()
	if err != nil {
		return nil, err
	}
	apiURL := endpoint + "/api/v1" + path
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Long timeout: a per-request countdown alone takes 3s; longer for typing.
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	var result api.APIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}

func computerAPIRequest(method, path string, body any) error {
	result, err := computerAPIRequestRaw(method, path, body)
	if err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("%s", result.Error)
	}
	if computerJSONOutput {
		return outputJSON(result.Data)
	}
	return outputHuman(result.Data)
}
