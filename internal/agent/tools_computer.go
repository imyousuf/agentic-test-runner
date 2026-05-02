package agent

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

// computerTool is a thin Tool implementation parameterized by name,
// description, parameters, and an execute closure. Used to register the
// 20+ desktop tools without one struct per tool.
type computerTool struct {
	name        string
	description string
	parameters  map[string]any
	exec        func(ctx context.Context, args map[string]any) (string, bool)
}

func (t *computerTool) Name() string               { return t.name }
func (t *computerTool) Description() string        { return t.description }
func (t *computerTool) Parameters() map[string]any { return t.parameters }
func (t *computerTool) Execute(ctx context.Context, args map[string]any) (string, bool) {
	return t.exec(ctx, args)
}

// NewComputerTools returns the agent-tool wrappers for desktop control.
// Pass a Computer instance configured with the desired countdown mode.
func NewComputerTools(c *computer.Computer) []Tool {
	intProp := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	strProp := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	boolProp := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	obj := func(req []string, props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": req}
	}

	getInt := func(args map[string]any, k string) int {
		v := args[k]
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
		return 0
	}
	// getDisplay returns the display index from args, or computer.NoDisplay
	// if absent. Distinguishes "missing" from "0" (display 0 is a valid value).
	getDisplay := func(args map[string]any) int {
		v, ok := args["display"]
		if !ok || v == nil {
			return computer.NoDisplay
		}
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
		return computer.NoDisplay
	}
	displayProp := intProp("Optional display index. When set, x/y are pixels relative to that display's top-left; otherwise they are absolute root coordinates.")
	getStr := func(args map[string]any, k string) string { s, _ := args[k].(string); return s }
	getStrDef := func(args map[string]any, k, def string) string {
		if s, ok := args[k].(string); ok && s != "" {
			return s
		}
		return def
	}
	getBool := func(args map[string]any, k string) bool { b, _ := args[k].(bool); return b }

	wrap := func(err error, ok string) (string, bool) {
		if err != nil {
			return err.Error(), true
		}
		return ok, false
	}

	jsonOf := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("(error marshaling: %v)", err)
		}
		return string(b)
	}

	tools := []Tool{
		&computerTool{
			name:        "computer_screenshot",
			description: "Capture the desktop and write to a file. Returns the file path.",
			parameters: obj(nil, map[string]any{
				"output":  strProp("Output file path (default: temp file)"),
				"display": intProp("Display index (default: 0)"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				display := -1
				if v, ok := args["display"]; ok {
					if n, ok := v.(float64); ok {
						display = int(n)
					}
				}
				out := getStr(args, "output")
				if out == "" {
					out = filepath.Join(os.TempDir(), fmt.Sprintf("atr-screenshot-%d.png", time.Now().UnixNano()))
				}
				png, err := c.Screenshot(display)
				if err != nil {
					return err.Error(), true
				}
				if err := os.WriteFile(out, png, 0o644); err != nil {
					return err.Error(), true
				}
				return fmt.Sprintf("Screenshot saved to %s (%d bytes)", out, len(png)), false
			},
		},
		&computerTool{
			name:        "computer_click",
			description: "Click at screen coordinates. button=left|right|center; double=true for double click.",
			parameters: obj([]string{"x", "y"}, map[string]any{
				"x":       intProp("X pixel coordinate"),
				"y":       intProp("Y pixel coordinate"),
				"button":  strProp("Mouse button"),
				"double":  boolProp("Double click"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				x, y := getInt(args, "x"), getInt(args, "y")
				btn := computer.MouseButton(getStrDef(args, "button", "left"))
				err := c.Click(ctx, x, y, btn, getBool(args, "double"), getDisplay(args))
				return wrap(err, fmt.Sprintf("Clicked at (%d, %d)", x, y))
			},
		},
		&computerTool{
			name:        "computer_double_click",
			description: "Double-click at screen coordinates.",
			parameters: obj([]string{"x", "y"}, map[string]any{
				"x":       intProp("X"),
				"y":       intProp("Y"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				x, y := getInt(args, "x"), getInt(args, "y")
				return wrap(c.Click(ctx, x, y, computer.ButtonLeft, true, getDisplay(args)), fmt.Sprintf("Double-clicked at (%d, %d)", x, y))
			},
		},
		&computerTool{
			name:        "computer_right_click",
			description: "Right-click at screen coordinates.",
			parameters: obj([]string{"x", "y"}, map[string]any{
				"x":       intProp("X"),
				"y":       intProp("Y"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				x, y := getInt(args, "x"), getInt(args, "y")
				return wrap(c.Click(ctx, x, y, computer.ButtonRight, false, getDisplay(args)), fmt.Sprintf("Right-clicked at (%d, %d)", x, y))
			},
		},
		&computerTool{
			name:        "computer_move",
			description: "Move mouse to (x, y). smooth=true animates.",
			parameters: obj([]string{"x", "y"}, map[string]any{
				"x":       intProp("X"),
				"y":       intProp("Y"),
				"smooth":  boolProp("Animate"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				x, y := getInt(args, "x"), getInt(args, "y")
				return wrap(c.MoveTo(ctx, x, y, getBool(args, "smooth"), getDisplay(args)), fmt.Sprintf("Moved to (%d, %d)", x, y))
			},
		},
		&computerTool{
			name:        "computer_drag",
			description: "Drag from (from_x, from_y) to (to_x, to_y).",
			parameters: obj([]string{"from_x", "from_y", "to_x", "to_y"}, map[string]any{
				"from_x":  intProp("Start X"),
				"from_y":  intProp("Start Y"),
				"to_x":    intProp("End X"),
				"to_y":    intProp("End Y"),
				"button":  strProp("Mouse button"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				fx, fy := getInt(args, "from_x"), getInt(args, "from_y")
				tx, ty := getInt(args, "to_x"), getInt(args, "to_y")
				btn := computer.MouseButton(getStrDef(args, "button", "left"))
				return wrap(c.Drag(ctx, fx, fy, tx, ty, btn, getDisplay(args)), fmt.Sprintf("Dragged (%d,%d) -> (%d,%d)", fx, fy, tx, ty))
			},
		},
		&computerTool{
			name:        "computer_scroll",
			description: "Scroll the wheel by (dx, dy). Positive dy scrolls up.",
			parameters: obj(nil, map[string]any{
				"dx": intProp("Horizontal"),
				"dy": intProp("Vertical"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				dx, dy := getInt(args, "dx"), getInt(args, "dy")
				return wrap(c.Scroll(ctx, dx, dy), fmt.Sprintf("Scrolled (%d, %d)", dx, dy))
			},
		},
		&computerTool{
			name:        "computer_hover",
			description: "Move mouse to (x, y) without clicking.",
			parameters: obj([]string{"x", "y"}, map[string]any{
				"x":       intProp("X"),
				"y":       intProp("Y"),
				"display": displayProp,
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				x, y := getInt(args, "x"), getInt(args, "y")
				return wrap(c.Hover(ctx, x, y, getDisplay(args)), fmt.Sprintf("Hovered at (%d, %d)", x, y))
			},
		},
		&computerTool{
			name:        "computer_type",
			description: "Type text at the current focus.",
			parameters: obj([]string{"text"}, map[string]any{
				"text":     strProp("Text to type"),
				"delay_ms": intProp("Delay between keys"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				text := getStr(args, "text")
				return wrap(c.Type(ctx, text, getInt(args, "delay_ms")), fmt.Sprintf("Typed %d chars", len(text)))
			},
		},
		&computerTool{
			name:        "computer_press_key",
			description: "Press a single named key (e.g. enter, esc, f5).",
			parameters: obj([]string{"key"}, map[string]any{
				"key": strProp("Key name"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				key := getStr(args, "key")
				return wrap(c.PressKey(ctx, key), fmt.Sprintf("Pressed %q", key))
			},
		},
		&computerTool{
			name:        "computer_key_chord",
			description: "Press a key chord (e.g. ctrl+shift+t).",
			parameters: obj([]string{"chord"}, map[string]any{
				"chord": strProp("Chord with + separator"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				chord := getStr(args, "chord")
				return wrap(c.KeyChord(ctx, chord), fmt.Sprintf("Pressed %q", chord))
			},
		},
		&computerTool{
			name:        "computer_position",
			description: "Return the current mouse position.",
			parameters:  obj(nil, map[string]any{}),
			exec: func(ctx context.Context, _ map[string]any) (string, bool) {
				x, y := c.Position()
				return fmt.Sprintf("Mouse: (%d, %d)", x, y), false
			},
		},
		&computerTool{
			name:        "computer_displays",
			description: "List attached displays and primary screen size.",
			parameters:  obj(nil, map[string]any{}),
			exec: func(ctx context.Context, _ map[string]any) (string, bool) {
				w, h := c.ScreenSize()
				return fmt.Sprintf("Primary: %dx%d. Displays: %s", w, h, jsonOf(c.Displays())), false
			},
		},
		&computerTool{
			name:        "computer_list_windows",
			description: "List all top-level windows with title, app, PID, bounds.",
			parameters:  obj(nil, map[string]any{}),
			exec: func(ctx context.Context, _ map[string]any) (string, bool) {
				wins, err := c.ListWindows()
				if err != nil {
					return err.Error(), true
				}
				return jsonOf(wins), false
			},
		},
		&computerTool{
			name:        "computer_active_window",
			description: "Return the currently focused window.",
			parameters:  obj(nil, map[string]any{}),
			exec: func(ctx context.Context, _ map[string]any) (string, bool) {
				win, err := c.ActiveWindow()
				if err != nil {
					return err.Error(), true
				}
				return jsonOf(win), false
			},
		},
		&computerTool{
			name:        "computer_focus_window",
			description: "Focus a window by ID.",
			parameters: obj([]string{"id"}, map[string]any{
				"id": intProp("Window ID"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				id := uint32(getInt(args, "id"))
				return wrap(c.FocusWindow(ctx, id), fmt.Sprintf("Focused window %d", id))
			},
		},
		&computerTool{
			name:        "computer_window_state",
			description: "Set window state by ID. state: minimize, maximize, restore, close.",
			parameters: obj([]string{"id", "state"}, map[string]any{
				"id":    intProp("Window ID"),
				"state": strProp("State name"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				id := uint32(getInt(args, "id"))
				state := computer.WindowState(strings.ToLower(getStr(args, "state")))
				return wrap(c.SetWindowState(ctx, id, state), fmt.Sprintf("Window %d -> %s", id, state))
			},
		},
		&computerTool{
			name:        "computer_move_window",
			description: "Move a window to (x, y).",
			parameters: obj([]string{"id", "x", "y"}, map[string]any{
				"id": intProp("Window ID"),
				"x":  intProp("X"),
				"y":  intProp("Y"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				id := uint32(getInt(args, "id"))
				x, y := getInt(args, "x"), getInt(args, "y")
				return wrap(c.MoveWindow(ctx, id, x, y), fmt.Sprintf("Moved window %d to (%d, %d)", id, x, y))
			},
		},
		&computerTool{
			name:        "computer_resize_window",
			description: "Resize a window to (width, height).",
			parameters: obj([]string{"id", "width", "height"}, map[string]any{
				"id":     intProp("Window ID"),
				"width":  intProp("Width"),
				"height": intProp("Height"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				id := uint32(getInt(args, "id"))
				w, h := getInt(args, "width"), getInt(args, "height")
				return wrap(c.ResizeWindow(ctx, id, w, h), fmt.Sprintf("Resized window %d to %dx%d", id, w, h))
			},
		},
		&computerTool{
			name:        "computer_launch_app",
			description: "Launch an application by name.",
			parameters: obj([]string{"name"}, map[string]any{
				"name": strProp("Application name"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				name := getStr(args, "name")
				return wrap(c.LaunchApp(ctx, name), fmt.Sprintf("Launched %q", name))
			},
		},
		&computerTool{
			name:        "computer_quit_app",
			description: "Quit an application by name.",
			parameters: obj([]string{"name"}, map[string]any{
				"name": strProp("Application name"),
			}),
			exec: func(ctx context.Context, args map[string]any) (string, bool) {
				name := getStr(args, "name")
				return wrap(c.QuitApp(ctx, name), fmt.Sprintf("Quit %q", name))
			},
		},
	}
	return tools
}
