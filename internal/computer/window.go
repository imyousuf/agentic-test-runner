package computer

import "context"

// Window describes a top-level application window.
type Window struct {
	// ID is the platform window identifier (X11 window ID on Linux,
	// CGWindowID on macOS, HWND on Windows).
	ID uint32 `json:"id"`
	// PID is the process owning the window. May be 0 if not detectable.
	PID uint32 `json:"pid"`
	// Title is the visible window title.
	Title string `json:"title"`
	// AppName is the owning process / bundle name when known.
	AppName string `json:"app_name,omitempty"`
	// Bounds is the window's screen rectangle: x, y, width, height.
	Bounds [4]int `json:"bounds"`
	// Minimized indicates the window is currently iconified.
	Minimized bool `json:"minimized"`
	// Maximized indicates the window is currently maximized.
	Maximized bool `json:"maximized"`
}

// WindowState selects a window-state operation.
type WindowState string

const (
	WindowMinimize WindowState = "minimize"
	WindowMaximize WindowState = "maximize"
	WindowRestore  WindowState = "restore"
	WindowClose    WindowState = "close"
)

// ListWindows enumerates all top-level windows. Read-only; no safety gate.
func (c *Computer) ListWindows() ([]Window, error) {
	return platformListWindows()
}

// ActiveWindow returns the currently focused window.
func (c *Computer) ActiveWindow() (Window, error) {
	return platformActiveWindow()
}

// FocusWindow brings the window with the given platform ID to the front.
func (c *Computer) FocusWindow(ctx context.Context, id uint32) error {
	if err := c.Confirm(ctx, ActionDesc{Description: describeWindow("Focus", id), AppID: c.activeAppID()}); err != nil {
		return err
	}
	return platformFocusWindow(id)
}

// SetWindowState applies a state operation (minimize/maximize/restore/close).
func (c *Computer) SetWindowState(ctx context.Context, id uint32, state WindowState) error {
	if err := c.Confirm(ctx, ActionDesc{Description: describeWindow(string(state), id), AppID: c.activeAppID()}); err != nil {
		return err
	}
	return platformSetWindowState(id, state)
}

// MoveWindow moves the window with the given ID to (x, y).
func (c *Computer) MoveWindow(ctx context.Context, id uint32, x, y int) error {
	if err := c.Confirm(ctx, ActionDesc{Description: describeWindow("Move", id), AppID: c.activeAppID()}); err != nil {
		return err
	}
	return platformMoveWindow(id, x, y)
}

// ResizeWindow resizes the window with the given ID to (w, h).
func (c *Computer) ResizeWindow(ctx context.Context, id uint32, w, h int) error {
	if err := c.Confirm(ctx, ActionDesc{Description: describeWindow("Resize", id), AppID: c.activeAppID()}); err != nil {
		return err
	}
	return platformResizeWindow(id, w, h)
}

func describeWindow(verb string, id uint32) string {
	return verb + " window " + uintToStr(id)
}

func uintToStr(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
