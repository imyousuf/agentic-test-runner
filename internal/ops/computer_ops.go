package ops

// Computer primitive Request/Result types and Execute functions live in this
// file. Each primitive follows the pattern:
//
//   type XRequest struct { ... }
//   type XResult  struct { ... }
//   func X(ctx context.Context, c *computer.Computer, req XRequest) (XResult, error)
//
// REST handlers in internal/api/computer_handlers.go and MCP dispatchers in
// internal/mcp/computer_dispatch.go both call into these functions.
//
// The REST layer maps computer.ErrAborted → HTTP 499 via abortStatus(); ops
// itself returns the raw error.

import (
	"context"
	"fmt"

	"github.com/imyousuf/agentic-test-runner/internal/computer"
)

// --- Screenshot -------------------------------------------------------------

// ComputerScreenshotRequest captures the desktop or a region of it.
//
// When Region is true, X/Y/Width/Height define the region in display-local
// coordinates of the chosen Display. When Region is false, the entire display
// is captured. Display is *int so callers can distinguish "no display field"
// (use the daemon's configured default) from "display 0" (display-local
// coords on the primary monitor).
type ComputerScreenshotRequest struct {
	Display *int   `json:"display,omitempty" jsonschema_description:"Display index to capture; omit to use the daemon's configured default"`
	Region  bool   `json:"region"            jsonschema_description:"Capture a sub-rectangle instead of the full display"`
	X       int    `json:"x"                 jsonschema_description:"Region X (display-local pixels) when region=true"`
	Y       int    `json:"y"                 jsonschema_description:"Region Y (display-local pixels) when region=true"`
	Width   int    `json:"width"             jsonschema_description:"Region width in pixels when region=true"`
	Height  int    `json:"height"            jsonschema_description:"Region height in pixels when region=true"`
	Output  string `json:"output"            jsonschema_description:"Optional file path to save the PNG; if empty the daemon returns base64."`
}

// ComputerScreenshotResult carries the captured PNG bytes.
type ComputerScreenshotResult struct {
	PNG    []byte `json:"-"`
	Format string `json:"format"`
}

// ComputerScreenshot captures the current display (or a region) and returns
// PNG bytes. Surfaces decide whether to base64-encode (REST) or save to disk
// (MCP).
func ComputerScreenshot(_ context.Context, c *computer.Computer, req ComputerScreenshotRequest) (ComputerScreenshotResult, error) {
	display := -1
	if req.Display != nil {
		display = *req.Display
	}
	var (
		png []byte
		err error
	)
	if req.Region {
		if req.Width <= 0 || req.Height <= 0 {
			return ComputerScreenshotResult{}, fmt.Errorf("region requires positive width and height")
		}
		png, err = c.ScreenshotRegion(display, req.X, req.Y, req.Width, req.Height)
	} else {
		png, err = c.Screenshot(display)
	}
	if err != nil {
		return ComputerScreenshotResult{}, err
	}
	return ComputerScreenshotResult{PNG: png, Format: "png"}, nil
}

// --- Click / mouse ----------------------------------------------------------

// ComputerClickRequest clicks at (X, Y). When Display is non-nil, the click
// uses display-local coordinates relative to that display; when nil, (X, Y)
// are absolute root coordinates.
type ComputerClickRequest struct {
	X           int    `json:"x"            jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y           int    `json:"y"            jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Button      string `json:"button"                                jsonschema_description:"Mouse button: left, right, or center (default: left)"`
	DoubleClick bool   `json:"double_click"                          jsonschema_description:"Issue a double-click instead of a single click"`
	RightClick  bool   `json:"right_click"                           jsonschema_description:"Convenience: equivalent to button=\"right\""`
	Display     *int   `json:"display,omitempty"                     jsonschema_description:"Optional display index. When set, x and y are display-local pixels; otherwise root coordinates."`
}

// ComputerClickResult reports the click coordinates and resolved display.
// Display is *int so the internal NoDisplay sentinel (-1) doesn't leak into
// JSON responses — when the caller didn't pass one, the field is omitted.
type ComputerClickResult struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Display *int `json:"display,omitempty"`
}

// ComputerClick clicks (or double-clicks / right-clicks) at the given coords.
func ComputerClick(ctx context.Context, c *computer.Computer, req ComputerClickRequest) (ComputerClickResult, error) {
	button := computer.MouseButton(req.Button)
	if req.RightClick {
		button = computer.ButtonRight
	}
	if button == "" {
		button = computer.ButtonLeft
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	if err := c.Click(ctx, req.X, req.Y, button, req.DoubleClick, display); err != nil {
		return ComputerClickResult{}, err
	}
	return ComputerClickResult{X: req.X, Y: req.Y, Display: req.Display}, nil
}

// ComputerMoveRequest moves the mouse to (X, Y).
type ComputerMoveRequest struct {
	X       int  `json:"x"                  jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y       int  `json:"y"                  jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Smooth  bool `json:"smooth"                                  jsonschema_description:"Animate the move"`
	Display *int `json:"display,omitempty"                       jsonschema_description:"Optional display index"`
}

// ComputerMoveResult mirrors ComputerClickResult.
type ComputerMoveResult struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Display *int `json:"display,omitempty"`
}

// ComputerMove moves the mouse cursor.
func ComputerMove(ctx context.Context, c *computer.Computer, req ComputerMoveRequest) (ComputerMoveResult, error) {
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	if err := c.MoveTo(ctx, req.X, req.Y, req.Smooth, display); err != nil {
		return ComputerMoveResult{}, err
	}
	return ComputerMoveResult{X: req.X, Y: req.Y, Display: req.Display}, nil
}

// ComputerDragRequest drags from one point to another.
type ComputerDragRequest struct {
	FromX   int    `json:"from_x"             jsonschema:"required" jsonschema_description:"Start X coordinate"`
	FromY   int    `json:"from_y"             jsonschema:"required" jsonschema_description:"Start Y coordinate"`
	ToX     int    `json:"to_x"               jsonschema:"required" jsonschema_description:"End X coordinate"`
	ToY     int    `json:"to_y"               jsonschema:"required" jsonschema_description:"End Y coordinate"`
	Button  string `json:"button"                                  jsonschema_description:"Mouse button (default: left)"`
	Display *int   `json:"display,omitempty"                       jsonschema_description:"Optional display index applied to both endpoints"`
}

// ComputerDragResult reports the drag endpoints.
type ComputerDragResult struct {
	From    [2]int `json:"from"`
	To      [2]int `json:"to"`
	Display *int   `json:"display,omitempty"`
}

// ComputerDrag drags from one point to another.
func ComputerDrag(ctx context.Context, c *computer.Computer, req ComputerDragRequest) (ComputerDragResult, error) {
	button := computer.MouseButton(req.Button)
	if button == "" {
		button = computer.ButtonLeft
	}
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	if err := c.Drag(ctx, req.FromX, req.FromY, req.ToX, req.ToY, button, display); err != nil {
		return ComputerDragResult{}, err
	}
	return ComputerDragResult{
		From:    [2]int{req.FromX, req.FromY},
		To:      [2]int{req.ToX, req.ToY},
		Display: req.Display,
	}, nil
}

// ComputerScrollRequest scrolls the wheel at the cursor's position.
type ComputerScrollRequest struct {
	DX int `json:"dx" jsonschema_description:"Horizontal scroll amount"`
	DY int `json:"dy" jsonschema_description:"Vertical scroll amount (positive = up)"`
}

// ComputerScrollResult reports the requested scroll deltas.
type ComputerScrollResult struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// ComputerScroll scrolls the wheel at the cursor's current position.
func ComputerScroll(ctx context.Context, c *computer.Computer, req ComputerScrollRequest) (ComputerScrollResult, error) {
	if err := c.Scroll(ctx, req.DX, req.DY); err != nil {
		return ComputerScrollResult{}, err
	}
	return ComputerScrollResult{DX: req.DX, DY: req.DY}, nil
}

// ComputerHoverRequest moves the mouse without clicking.
type ComputerHoverRequest struct {
	X       int  `json:"x"                  jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y       int  `json:"y"                  jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Display *int `json:"display,omitempty"                       jsonschema_description:"Optional display index"`
}

// ComputerHoverResult reports the hover coordinates.
type ComputerHoverResult struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Display *int `json:"display,omitempty"`
}

// ComputerHover moves the mouse cursor without clicking.
func ComputerHover(ctx context.Context, c *computer.Computer, req ComputerHoverRequest) (ComputerHoverResult, error) {
	display := computer.NoDisplay
	if req.Display != nil {
		display = *req.Display
	}
	if err := c.Hover(ctx, req.X, req.Y, display); err != nil {
		return ComputerHoverResult{}, err
	}
	return ComputerHoverResult{X: req.X, Y: req.Y, Display: req.Display}, nil
}

// --- Keyboard ---------------------------------------------------------------

// ComputerTypeRequest types text by simulating keystrokes.
type ComputerTypeRequest struct {
	Text    string `json:"text"     jsonschema:"required" jsonschema_description:"Text to type"`
	DelayMs int    `json:"delay_ms"                       jsonschema_description:"Delay between key events in milliseconds"`
}

// ComputerTypeResult reports the number of characters typed.
type ComputerTypeResult struct {
	Chars int `json:"chars"`
}

// ComputerType types text.
func ComputerType(ctx context.Context, c *computer.Computer, req ComputerTypeRequest) (ComputerTypeResult, error) {
	if err := c.Type(ctx, req.Text, req.DelayMs); err != nil {
		return ComputerTypeResult{}, err
	}
	return ComputerTypeResult{Chars: len(req.Text)}, nil
}

// ComputerPressKeyRequest presses a single named key.
type ComputerPressKeyRequest struct {
	Key string `json:"key" jsonschema:"required" jsonschema_description:"Key name (e.g. enter, esc, f5)"`
}

// ComputerPressKeyResult reports the pressed key.
type ComputerPressKeyResult struct {
	Key string `json:"key"`
}

// ComputerPressKey simulates pressing a single key.
func ComputerPressKey(ctx context.Context, c *computer.Computer, req ComputerPressKeyRequest) (ComputerPressKeyResult, error) {
	if req.Key == "" {
		return ComputerPressKeyResult{}, fmt.Errorf("key is required")
	}
	if err := c.PressKey(ctx, req.Key); err != nil {
		return ComputerPressKeyResult{}, err
	}
	return ComputerPressKeyResult{Key: req.Key}, nil
}

// ComputerKeyChordRequest presses a key combination such as ctrl+shift+t.
type ComputerKeyChordRequest struct {
	Chord string `json:"chord" jsonschema:"required" jsonschema_description:"Key combination, e.g. ctrl+shift+t"`
}

// ComputerKeyChordResult reports the pressed chord.
type ComputerKeyChordResult struct {
	Chord string `json:"chord"`
}

// ComputerKeyChord presses a chord.
func ComputerKeyChord(ctx context.Context, c *computer.Computer, req ComputerKeyChordRequest) (ComputerKeyChordResult, error) {
	if req.Chord == "" {
		return ComputerKeyChordResult{}, fmt.Errorf("chord is required")
	}
	if err := c.KeyChord(ctx, req.Chord); err != nil {
		return ComputerKeyChordResult{}, err
	}
	return ComputerKeyChordResult{Chord: req.Chord}, nil
}

// --- Passive reads ----------------------------------------------------------

// ComputerPositionResult carries the current cursor position.
type ComputerPositionResult struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ComputerPosition returns the current cursor position.
func ComputerPosition(_ context.Context, c *computer.Computer) (ComputerPositionResult, error) {
	x, y := c.Position()
	return ComputerPositionResult{X: x, Y: y}, nil
}

// ComputerDisplaysResult lists the attached displays.
type ComputerDisplaysResult struct {
	Primary  PrimarySize        `json:"primary"`
	Displays []computer.Display `json:"displays"`
}

// PrimarySize is the size of the primary screen in pixels.
type PrimarySize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ComputerDisplays returns the primary screen size and all displays.
func ComputerDisplays(_ context.Context, c *computer.Computer) (ComputerDisplaysResult, error) {
	w, h := c.ScreenSize()
	return ComputerDisplaysResult{
		Primary:  PrimarySize{Width: w, Height: h},
		Displays: c.Displays(),
	}, nil
}

// ComputerApprovalsClearResult reports that the per-app approval cache was cleared.
type ComputerApprovalsClearResult struct {
	Cleared bool `json:"cleared"`
}

// ComputerApprovalsClear clears the per-app approval cache.
func ComputerApprovalsClear(_ context.Context, c *computer.Computer) (ComputerApprovalsClearResult, error) {
	c.ResetApprovals()
	return ComputerApprovalsClearResult{Cleared: true}, nil
}

// --- Window management ------------------------------------------------------

// ComputerListWindowsResult lists all top-level windows.
type ComputerListWindowsResult struct {
	Windows []computer.Window `json:"windows"`
	Count   int               `json:"count"`
}

// ComputerListWindows enumerates top-level windows.
func ComputerListWindows(_ context.Context, c *computer.Computer) (ComputerListWindowsResult, error) {
	wins, err := c.ListWindows()
	if err != nil {
		return ComputerListWindowsResult{}, err
	}
	return ComputerListWindowsResult{Windows: wins, Count: len(wins)}, nil
}

// ComputerActiveWindow returns the currently focused window.
func ComputerActiveWindow(_ context.Context, c *computer.Computer) (computer.Window, error) {
	return c.ActiveWindow()
}

// ComputerFocusWindowRequest focuses a window by ID.
type ComputerFocusWindowRequest struct {
	ID uint32 `json:"id" jsonschema:"required" jsonschema_description:"Platform window ID"`
}

// ComputerFocusWindowResult reports the focused window's ID.
type ComputerFocusWindowResult struct {
	ID uint32 `json:"id"`
}

// ComputerFocusWindow focuses a window.
func ComputerFocusWindow(ctx context.Context, c *computer.Computer, req ComputerFocusWindowRequest) (ComputerFocusWindowResult, error) {
	if req.ID == 0 {
		return ComputerFocusWindowResult{}, fmt.Errorf("id is required")
	}
	if err := c.FocusWindow(ctx, req.ID); err != nil {
		return ComputerFocusWindowResult{}, err
	}
	return ComputerFocusWindowResult{ID: req.ID}, nil
}

// ComputerWindowStateRequest applies a window-state operation.
type ComputerWindowStateRequest struct {
	ID    uint32 `json:"id"    jsonschema:"required" jsonschema_description:"Platform window ID"`
	State string `json:"state" jsonschema:"required" jsonschema_description:"State to apply: minimize, maximize, restore, close"`
}

// ComputerWindowStateResult mirrors the request.
type ComputerWindowStateResult struct {
	ID    uint32 `json:"id"`
	State string `json:"state"`
}

// ComputerWindowState changes a window's state.
func ComputerWindowState(ctx context.Context, c *computer.Computer, req ComputerWindowStateRequest) (ComputerWindowStateResult, error) {
	if req.ID == 0 || req.State == "" {
		return ComputerWindowStateResult{}, fmt.Errorf("id and state are required")
	}
	if err := c.SetWindowState(ctx, req.ID, computer.WindowState(req.State)); err != nil {
		return ComputerWindowStateResult{}, err
	}
	return ComputerWindowStateResult{ID: req.ID, State: req.State}, nil
}

// ComputerMoveWindowRequest moves a window.
type ComputerMoveWindowRequest struct {
	ID uint32 `json:"id" jsonschema:"required" jsonschema_description:"Platform window ID"`
	X  int    `json:"x"  jsonschema:"required" jsonschema_description:"Target X (root coordinates)"`
	Y  int    `json:"y"  jsonschema:"required" jsonschema_description:"Target Y (root coordinates)"`
}

// ComputerMoveWindowResult mirrors the request.
type ComputerMoveWindowResult struct {
	ID uint32 `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
}

// ComputerMoveWindow moves a window.
func ComputerMoveWindow(ctx context.Context, c *computer.Computer, req ComputerMoveWindowRequest) (ComputerMoveWindowResult, error) {
	if req.ID == 0 {
		return ComputerMoveWindowResult{}, fmt.Errorf("id is required")
	}
	if err := c.MoveWindow(ctx, req.ID, req.X, req.Y); err != nil {
		return ComputerMoveWindowResult{}, err
	}
	return ComputerMoveWindowResult{ID: req.ID, X: req.X, Y: req.Y}, nil
}

// ComputerResizeWindowRequest resizes a window.
type ComputerResizeWindowRequest struct {
	ID     uint32 `json:"id"     jsonschema:"required" jsonschema_description:"Platform window ID"`
	Width  int    `json:"width"  jsonschema:"required" jsonschema_description:"Target width in pixels"`
	Height int    `json:"height" jsonschema:"required" jsonschema_description:"Target height in pixels"`
}

// ComputerResizeWindowResult mirrors the request.
type ComputerResizeWindowResult struct {
	ID     uint32 `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ComputerResizeWindow resizes a window.
func ComputerResizeWindow(ctx context.Context, c *computer.Computer, req ComputerResizeWindowRequest) (ComputerResizeWindowResult, error) {
	if req.ID == 0 || req.Width <= 0 || req.Height <= 0 {
		return ComputerResizeWindowResult{}, fmt.Errorf("id, width, and height are required")
	}
	if err := c.ResizeWindow(ctx, req.ID, req.Width, req.Height); err != nil {
		return ComputerResizeWindowResult{}, err
	}
	return ComputerResizeWindowResult{ID: req.ID, Width: req.Width, Height: req.Height}, nil
}

// --- App management ---------------------------------------------------------

// ComputerLaunchAppRequest launches an application by name.
type ComputerLaunchAppRequest struct {
	Name string `json:"name" jsonschema:"required" jsonschema_description:"Application name to launch"`
}

// ComputerLaunchAppResult reports the launched application.
type ComputerLaunchAppResult struct {
	Launched string `json:"launched"`
}

// ComputerLaunchApp launches an application.
func ComputerLaunchApp(ctx context.Context, c *computer.Computer, req ComputerLaunchAppRequest) (ComputerLaunchAppResult, error) {
	if req.Name == "" {
		return ComputerLaunchAppResult{}, fmt.Errorf("name is required")
	}
	if err := c.LaunchApp(ctx, req.Name); err != nil {
		return ComputerLaunchAppResult{}, err
	}
	return ComputerLaunchAppResult{Launched: req.Name}, nil
}

// ComputerQuitAppRequest quits an application by name.
type ComputerQuitAppRequest struct {
	Name string `json:"name" jsonschema:"required" jsonschema_description:"Application name to quit"`
}

// ComputerQuitAppResult reports the quit application.
type ComputerQuitAppResult struct {
	Quit string `json:"quit"`
}

// ComputerQuitApp quits an application.
func ComputerQuitApp(ctx context.Context, c *computer.Computer, req ComputerQuitAppRequest) (ComputerQuitAppResult, error) {
	if req.Name == "" {
		return ComputerQuitAppResult{}, fmt.Errorf("name is required")
	}
	if err := c.QuitApp(ctx, req.Name); err != nil {
		return ComputerQuitAppResult{}, err
	}
	return ComputerQuitAppResult{Quit: req.Name}, nil
}

// --- Ask --------------------------------------------------------------------

// ComputerAskRequest is a natural-language instruction for the computer-use
// agent loop. As with browser Ask, the ops layer accepts the request but
// requires the surface to inject an AskRunner with an LLM client.
type ComputerAskRequest struct {
	Instruction    string `json:"instruction"     jsonschema:"required" jsonschema_description:"Natural-language task for the agent to accomplish"`
	MaxSteps       int    `json:"max_steps"                                jsonschema_description:"Maximum agent iterations (default 20)"`
	TimeoutSeconds int    `json:"timeout_seconds"                          jsonschema_description:"Wall-clock timeout in seconds (default 300)"`
}

// ComputerAskResult carries the agent's final answer.
type ComputerAskResult struct {
	Answer string `json:"answer"`
}

// ComputerAsk runs the supplied AskRunner with the requested instruction.
// Surfaces wire up the LLM client and the ComputerAskAgent themselves.
func ComputerAsk(ctx context.Context, run AskRunner, req ComputerAskRequest) (ComputerAskResult, error) {
	if req.Instruction == "" {
		return ComputerAskResult{}, fmt.Errorf("instruction is required")
	}
	if run == nil {
		return ComputerAskResult{}, fmt.Errorf("ask runner not configured")
	}
	answer, err := run(ctx, req.Instruction)
	if err != nil {
		return ComputerAskResult{}, fmt.Errorf("computer ask failed: %w", err)
	}
	return ComputerAskResult{Answer: answer}, nil
}
