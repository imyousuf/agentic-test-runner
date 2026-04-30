package computer

import (
	"context"
	"fmt"

	"github.com/go-vgo/robotgo"
)

// MouseButton identifies which mouse button to use.
type MouseButton string

const (
	ButtonLeft   MouseButton = "left"
	ButtonRight  MouseButton = "right"
	ButtonMiddle MouseButton = "center"
)

// IsValid reports whether b is a known button name.
func (b MouseButton) IsValid() bool {
	switch b {
	case ButtonLeft, ButtonRight, ButtonMiddle:
		return true
	}
	return false
}

// NoDisplay marks coordinates as already being in root coords. Used as
// the displayIndex argument to mouse/keyboard methods when the caller
// has not specified a display.
const NoDisplay = -1

// resolveDisplay translates display-local (x, y) on display N to root
// coords. When displayIndex is NoDisplay, (x, y) is returned unchanged
// (already root coords).
func (c *Computer) resolveDisplay(x, y, displayIndex int) (int, int, error) {
	if displayIndex < 0 {
		return x, y, nil
	}
	displays := c.Displays()
	if displayIndex >= len(displays) {
		return 0, 0, fmt.Errorf("display %d out of range (have %d)", displayIndex, len(displays))
	}
	b := displays[displayIndex].Bounds
	if x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("display-local coords must be non-negative (got %d, %d)", x, y)
	}
	if x >= b.Dx() || y >= b.Dy() {
		return 0, 0, fmt.Errorf("display-local (%d, %d) out of display %d bounds (%dx%d)", x, y, displayIndex, b.Dx(), b.Dy())
	}
	return b.Min.X + x, b.Min.Y + y, nil
}

// Click moves the mouse to (x, y) and clicks the given button.
// If double is true, performs a double click. When displayIndex is
// >= 0, (x, y) is interpreted as display-local pixels relative to that
// display's top-left; otherwise (x, y) are root coords.
func (c *Computer) Click(ctx context.Context, x, y int, button MouseButton, double bool, displayIndex int) error {
	if !button.IsValid() {
		return fmt.Errorf("invalid button %q", button)
	}
	rx, ry, err := c.resolveDisplay(x, y, displayIndex)
	if err != nil {
		return err
	}
	desc := fmt.Sprintf("Click (%s%s) at (%d, %d)", button, doubleSuffix(double), rx, ry)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Move(rx, ry)
	robotgo.Click(string(button), double)
	return nil
}

// MoveTo moves the mouse cursor to (x, y). When smooth is true, the move
// is animated rather than instantaneous. displayIndex semantics match Click.
func (c *Computer) MoveTo(ctx context.Context, x, y int, smooth bool, displayIndex int) error {
	rx, ry, err := c.resolveDisplay(x, y, displayIndex)
	if err != nil {
		return err
	}
	desc := fmt.Sprintf("Move mouse to (%d, %d)", rx, ry)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	if smooth {
		robotgo.MoveSmooth(rx, ry)
	} else {
		robotgo.Move(rx, ry)
	}
	return nil
}

// Drag presses the given button at (fromX, fromY), drags to (toX, toY),
// and releases. displayIndex applies to BOTH endpoints — call twice with
// NoDisplay if the start and end are on different displays in root coords.
func (c *Computer) Drag(ctx context.Context, fromX, fromY, toX, toY int, button MouseButton, displayIndex int) error {
	if !button.IsValid() {
		return fmt.Errorf("invalid button %q", button)
	}
	rfx, rfy, err := c.resolveDisplay(fromX, fromY, displayIndex)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	rtx, rty, err := c.resolveDisplay(toX, toY, displayIndex)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	desc := fmt.Sprintf("Drag from (%d, %d) to (%d, %d) with %s button", rfx, rfy, rtx, rty, button)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Move(rfx, rfy)
	robotgo.Toggle(string(button), "down")
	robotgo.MoveSmooth(rtx, rty)
	robotgo.Toggle(string(button), "up")
	return nil
}

// Scroll scrolls the wheel by (dx, dy) at the current mouse position.
// Positive dy scrolls up; negative scrolls down. Most callers want dx=0.
// Scroll has no display parameter — it acts at the cursor's current
// position regardless of which monitor that is on.
func (c *Computer) Scroll(ctx context.Context, dx, dy int) error {
	desc := fmt.Sprintf("Scroll (%d, %d)", dx, dy)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Scroll(dx, dy)
	return nil
}

// Hover moves the mouse cursor to (x, y) without clicking. displayIndex
// semantics match Click.
func (c *Computer) Hover(ctx context.Context, x, y, displayIndex int) error {
	return c.MoveTo(ctx, x, y, true, displayIndex)
}

// Position returns the current (x, y) of the mouse cursor in root coords.
// This is a passive read and does not run the safety gate.
func (c *Computer) Position() (int, int) {
	return robotgo.Location()
}

func doubleSuffix(double bool) string {
	if double {
		return ", double"
	}
	return ""
}
