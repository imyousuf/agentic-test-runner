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

// Click moves the mouse to (x, y) and clicks the given button.
// If double is true, performs a double click.
func (c *Computer) Click(ctx context.Context, x, y int, button MouseButton, double bool) error {
	if !button.IsValid() {
		return fmt.Errorf("invalid button %q", button)
	}
	desc := fmt.Sprintf("Click (%s%s) at (%d, %d)", button, doubleSuffix(double), x, y)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Move(x, y)
	robotgo.Click(string(button), double)
	return nil
}

// MoveTo moves the mouse cursor to (x, y). When smooth is true, the move
// is animated rather than instantaneous.
func (c *Computer) MoveTo(ctx context.Context, x, y int, smooth bool) error {
	desc := fmt.Sprintf("Move mouse to (%d, %d)", x, y)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	if smooth {
		robotgo.MoveSmooth(x, y)
	} else {
		robotgo.Move(x, y)
	}
	return nil
}

// Drag presses the given button at (fromX, fromY), drags to (toX, toY),
// and releases.
func (c *Computer) Drag(ctx context.Context, fromX, fromY, toX, toY int, button MouseButton) error {
	if !button.IsValid() {
		return fmt.Errorf("invalid button %q", button)
	}
	desc := fmt.Sprintf("Drag from (%d, %d) to (%d, %d) with %s button", fromX, fromY, toX, toY, button)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Move(fromX, fromY)
	robotgo.Toggle(string(button), "down")
	robotgo.MoveSmooth(toX, toY)
	robotgo.Toggle(string(button), "up")
	return nil
}

// Scroll scrolls the wheel by (dx, dy) at the current mouse position.
// Positive dy scrolls up; negative scrolls down. Most callers want dx=0.
func (c *Computer) Scroll(ctx context.Context, dx, dy int) error {
	desc := fmt.Sprintf("Scroll (%d, %d)", dx, dy)
	if err := c.Confirm(ctx, ActionDesc{Description: desc, AppID: c.activeAppID()}); err != nil {
		return err
	}
	robotgo.Scroll(dx, dy)
	return nil
}

// Hover moves the mouse cursor to (x, y) without clicking.
func (c *Computer) Hover(ctx context.Context, x, y int) error {
	return c.MoveTo(ctx, x, y, true)
}

// Position returns the current (x, y) of the mouse cursor.
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
