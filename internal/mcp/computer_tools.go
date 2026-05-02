package mcp

import "github.com/imyousuf/agentic-test-runner/internal/ops"

// GetComputerTools returns the list of desktop computer-use tools for MCP.
// These wrap the internal/computer package: mouse, keyboard, screen, and
// window/app management.
//
// Input schemas are reflected from the canonical Request structs in
// internal/ops. A few tools (computer_click variants) use small alias
// structs declared below to expose only the relevant subset of fields.
func GetComputerTools() []Tool {
	return []Tool{
		{
			Name:        "computer_screenshot",
			Description: "Capture the desktop and write to a file. Returns the file path.",
			InputSchema: schemaFor(&ops.ComputerScreenshotRequest{}),
		},
		{
			Name:        "computer_click",
			Description: "Click at screen coordinates (x, y). Use button=left|right|center; double_click=true for double-click.",
			InputSchema: schemaFor(&computerClickSchemaArgs{}),
		},
		{
			Name:        "computer_double_click",
			Description: "Double-click at screen coordinates.",
			InputSchema: schemaFor(&computerDoubleClickSchemaArgs{}),
		},
		{
			Name:        "computer_right_click",
			Description: "Right-click at screen coordinates.",
			InputSchema: schemaFor(&computerRightClickSchemaArgs{}),
		},
		{
			Name:        "computer_move",
			Description: "Move mouse to (x, y). smooth=true animates the move.",
			InputSchema: schemaFor(&ops.ComputerMoveRequest{}),
		},
		{
			Name:        "computer_drag",
			Description: "Drag from (from_x, from_y) to (to_x, to_y) with the given button.",
			InputSchema: schemaFor(&ops.ComputerDragRequest{}),
		},
		{
			Name:        "computer_scroll",
			Description: "Scroll the wheel by (dx, dy). Positive dy scrolls up.",
			InputSchema: schemaFor(&ops.ComputerScrollRequest{}),
		},
		{
			Name:        "computer_hover",
			Description: "Move mouse to (x, y) without clicking.",
			InputSchema: schemaFor(&ops.ComputerHoverRequest{}),
		},
		{
			Name:        "computer_type",
			Description: "Type text at the current focus.",
			InputSchema: schemaFor(&ops.ComputerTypeRequest{}),
		},
		{
			Name:        "computer_press_key",
			Description: "Press a single named key (e.g. enter, esc, f5, tab).",
			InputSchema: schemaFor(&ops.ComputerPressKeyRequest{}),
		},
		{
			Name:        "computer_key_chord",
			Description: "Press a key combination (e.g. ctrl+shift+t, cmd+c).",
			InputSchema: schemaFor(&ops.ComputerKeyChordRequest{}),
		},
		{
			Name:        "computer_position",
			Description: "Return the current mouse position.",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "computer_displays",
			Description: "Return the list of attached displays and primary screen size.",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "computer_approvals_clear",
			Description: "Clear the per-app gating approvals so subsequent gated actions re-prompt.",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "computer_list_windows",
			Description: "List all top-level windows with title, app name, PID, and bounds.",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "computer_active_window",
			Description: "Return the currently focused window.",
			InputSchema: schemaFor(&struct{}{}),
		},
		{
			Name:        "computer_focus_window",
			Description: "Focus a window by ID (use computer_list_windows to find IDs).",
			InputSchema: schemaFor(&ops.ComputerFocusWindowRequest{}),
		},
		{
			Name:        "computer_window_state",
			Description: "Set window state by ID. state: minimize, maximize, restore, close.",
			InputSchema: schemaFor(&ops.ComputerWindowStateRequest{}),
		},
		{
			Name:        "computer_move_window",
			Description: "Move a window to (x, y).",
			InputSchema: schemaFor(&ops.ComputerMoveWindowRequest{}),
		},
		{
			Name:        "computer_resize_window",
			Description: "Resize a window to (width, height).",
			InputSchema: schemaFor(&ops.ComputerResizeWindowRequest{}),
		},
		{
			Name:        "computer_launch_app",
			Description: "Launch an application by name.",
			InputSchema: schemaFor(&ops.ComputerLaunchAppRequest{}),
		},
		{
			Name:        "computer_quit_app",
			Description: "Quit an application by name.",
			InputSchema: schemaFor(&ops.ComputerQuitAppRequest{}),
		},
		{
			Name:        "computer_ask",
			Description: "Run an in-process agent loop to accomplish a desktop task described in natural language. The agent screenshots the desktop, calls the LLM with the goal, and iterates clicks/type/key/launch until the goal is achieved or max-steps is hit. Use this when you want to delegate a multi-step UI task; use the lower-level computer_* tools when you want to drive each step yourself.",
			InputSchema: schemaFor(&ops.ComputerAskRequest{}),
		},
	}
}

// --- MCP-specific schema alias structs for click variants ---
//
// computer_click, computer_double_click, and computer_right_click all
// dispatch into ops.ComputerClick, but each tool exposes a different subset
// of its fields so the LLM only sees the knobs that make sense for that
// variant. The dispatcher sets the hidden flags itself.

// computerClickSchemaArgs is the schema for computer_click. It exposes
// double_click (so callers can opt in) but hides right_click — use the
// dedicated computer_right_click tool instead.
type computerClickSchemaArgs struct {
	X           int    `json:"x"            jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y           int    `json:"y"            jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Button      string `json:"button"                                jsonschema_description:"Mouse button: left, right, or center (default: left)"`
	DoubleClick bool   `json:"double_click"                          jsonschema_description:"Issue a double-click instead of a single click"`
	Display     *int   `json:"display,omitempty"                     jsonschema_description:"Optional display index. When set, x and y are display-local pixels; otherwise root coordinates."`
}

// computerDoubleClickSchemaArgs is the schema for computer_double_click.
// Both DoubleClick and RightClick are set by the dispatcher and therefore
// hidden from the schema.
type computerDoubleClickSchemaArgs struct {
	X       int  `json:"x"                  jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y       int  `json:"y"                  jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Display *int `json:"display,omitempty"                       jsonschema_description:"Optional display index. When set, x and y are display-local pixels; otherwise root coordinates."`
}

// computerRightClickSchemaArgs is the schema for computer_right_click.
// RightClick is set by the dispatcher; DoubleClick is intentionally hidden.
type computerRightClickSchemaArgs struct {
	X       int  `json:"x"                  jsonschema:"required" jsonschema_description:"X pixel coordinate"`
	Y       int  `json:"y"                  jsonschema:"required" jsonschema_description:"Y pixel coordinate"`
	Display *int `json:"display,omitempty"                       jsonschema_description:"Optional display index. When set, x and y are display-local pixels; otherwise root coordinates."`
}
