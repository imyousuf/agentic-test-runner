package mcp

// GetComputerTools returns the list of desktop computer-use tools for MCP.
// These wrap the internal/computer package: mouse, keyboard, screen, and
// window/app management.
func GetComputerTools() []Tool {
	intProp := func(desc string) map[string]any {
		return map[string]any{"type": "integer", "description": desc}
	}
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	boolProp := func(desc string) map[string]any {
		return map[string]any{"type": "boolean", "description": desc}
	}

	return []Tool{
		{
			Name:        "computer_screenshot",
			Description: "Capture the desktop and write to a file. Returns the file path.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"output":  strProp("Output file path (default: temp file)"),
					"display": intProp("Display index (default: 0)"),
				},
			},
		},
		{
			Name:        "computer_click",
			Description: "Click at screen coordinates (x, y). Use button=left|right|center; double=true for double-click.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":       intProp("X pixel coordinate"),
					"y":       intProp("Y pixel coordinate"),
					"button":  strProp("Mouse button (left, right, center)"),
					"double":  boolProp("Double-click"),
					"display": intProp("Optional display index. When set, x and y are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "computer_double_click",
			Description: "Double-click at screen coordinates.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": intProp("X pixel coordinate"),
					"y":       intProp("Y pixel coordinate"),
					"display": intProp("Optional display index. When set, x and y are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "computer_right_click",
			Description: "Right-click at screen coordinates.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": intProp("X pixel coordinate"),
					"y":       intProp("Y pixel coordinate"),
					"display": intProp("Optional display index. When set, x and y are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "computer_move",
			Description: "Move mouse to (x, y). smooth=true animates the move.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x":       intProp("X pixel coordinate"),
					"y":       intProp("Y pixel coordinate"),
					"smooth":  boolProp("Animate the move"),
					"display": intProp("Optional display index. When set, x and y are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "computer_drag",
			Description: "Drag from (from_x, from_y) to (to_x, to_y) with the given button.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from_x":  intProp("Start X"),
					"from_y":  intProp("Start Y"),
					"to_x":    intProp("End X"),
					"to_y":    intProp("End Y"),
					"button":  strProp("Mouse button (default: left)"),
					"display": intProp("Optional display index. When set, all coordinates are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"from_x", "from_y", "to_x", "to_y"},
			},
		},
		{
			Name:        "computer_scroll",
			Description: "Scroll the wheel by (dx, dy). Positive dy scrolls up.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"dx": intProp("Horizontal scroll amount"),
					"dy": intProp("Vertical scroll amount (positive = up)"),
				},
			},
		},
		{
			Name:        "computer_hover",
			Description: "Move mouse to (x, y) without clicking.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": intProp("X pixel coordinate"),
					"y":       intProp("Y pixel coordinate"),
					"display": intProp("Optional display index. When set, x and y are pixels relative to that display's top-left; otherwise they are absolute root coordinates."),
				},
				"required": []string{"x", "y"},
			},
		},
		{
			Name:        "computer_type",
			Description: "Type text at the current focus.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":     strProp("Text to type"),
					"delay_ms": intProp("Delay between key events"),
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "computer_press_key",
			Description: "Press a single named key (e.g. enter, esc, f5, tab).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": strProp("Key name"),
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "computer_key_chord",
			Description: "Press a key combination (e.g. ctrl+shift+t, cmd+c).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"chord": strProp("Chord string with + as separator"),
				},
				"required": []string{"chord"},
			},
		},
		{
			Name:        "computer_position",
			Description: "Return the current mouse position.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "computer_displays",
			Description: "Return the list of attached displays and primary screen size.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "computer_list_windows",
			Description: "List all top-level windows with title, app name, PID, and bounds.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "computer_active_window",
			Description: "Return the currently focused window.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "computer_focus_window",
			Description: "Focus a window by ID (use computer_list_windows to find IDs).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": intProp("Window ID"),
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "computer_window_state",
			Description: "Set window state by ID. state: minimize, maximize, restore, close.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    intProp("Window ID"),
					"state": strProp("minimize, maximize, restore, or close"),
				},
				"required": []string{"id", "state"},
			},
		},
		{
			Name:        "computer_move_window",
			Description: "Move a window to (x, y).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": intProp("Window ID"),
					"x":  intProp("Target X"),
					"y":  intProp("Target Y"),
				},
				"required": []string{"id", "x", "y"},
			},
		},
		{
			Name:        "computer_resize_window",
			Description: "Resize a window to (width, height).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     intProp("Window ID"),
					"width":  intProp("Target width"),
					"height": intProp("Target height"),
				},
				"required": []string{"id", "width", "height"},
			},
		},
		{
			Name:        "computer_launch_app",
			Description: "Launch an application by name.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": strProp("Application name"),
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "computer_quit_app",
			Description: "Quit an application by name.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": strProp("Application name"),
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "computer_ask",
			Description: "Run an in-process agent loop to accomplish a desktop task described in natural language. The agent screenshots the desktop, calls the LLM with the goal, and iterates clicks/type/key/launch until the goal is achieved or max-steps is hit. Use this when you want to delegate a multi-step UI task; use the lower-level computer_* tools when you want to drive each step yourself.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"instruction": strProp("Plain-language description of what to do (e.g. 'open xclock and tell me what time it shows')"),
					"max_steps":   intProp("Max agent iterations (default 20)"),
				},
				"required": []string{"instruction"},
			},
		},
	}
}
