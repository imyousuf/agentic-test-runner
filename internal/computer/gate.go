package computer

import (
	"context"
	"fmt"
	"time"
)

// ActionDesc describes a desktop action that the gate is about to allow
// through. Description is shown to the user; AppID is used as the cache
// key in per-app mode (typically window class on X11, bundle id on macOS,
// process exe on Windows).
type ActionDesc struct {
	Description string
	AppID       string
}

// Confirm runs the safety gate before an action executes. It returns nil
// when the action is approved (either auto-approved or after the countdown
// completes), or ErrAborted if the user cancels via context cancellation
// (typically Ctrl+C → SIGINT → cancelled context).
//
// The countdown itself is non-blocking on each tick — it watches ctx.Done()
// every 100ms so abort is responsive.
func (c *Computer) Confirm(ctx context.Context, action ActionDesc) error {
	switch c.cfg.CountdownMode {
	case ModeOff:
		return nil
	case ModePerApp:
		if action.AppID != "" && c.isApproved(action.AppID) {
			return nil
		}
	}

	if err := c.runCountdown(ctx, action); err != nil {
		return err
	}

	if c.cfg.CountdownMode == ModePerApp && action.AppID != "" {
		c.markApproved(action.AppID)
	}
	return nil
}

// runCountdown writes the countdown to the configured output and watches
// ctx for cancellation. Returns ErrAborted on cancellation.
func (c *Computer) runCountdown(ctx context.Context, action ActionDesc) error {
	seconds := c.cfg.CountdownSeconds
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}

	fmt.Fprintf(c.cfg.Output, "[atr] About to: %s\n[atr]   Press Ctrl+C to abort. ", desc)

	for remaining := seconds; remaining > 0; remaining-- {
		fmt.Fprintf(c.cfg.Output, "%d... ", remaining)
		// Use a 1-second tick made of 10×100ms slices for responsive cancellation.
		for range 10 {
			select {
			case <-ctx.Done():
				fmt.Fprintln(c.cfg.Output, "\n[atr] Aborted.")
				return ErrAborted
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	fmt.Fprintln(c.cfg.Output, "go.")
	return nil
}

// isApproved reports whether appID has been approved this session.
func (c *Computer) isApproved(appID string) bool {
	c.appsMu.Lock()
	defer c.appsMu.Unlock()
	_, ok := c.approvedApps[appID]
	return ok
}

// markApproved adds appID to the per-app cache.
func (c *Computer) markApproved(appID string) {
	c.appsMu.Lock()
	defer c.appsMu.Unlock()
	c.approvedApps[appID] = time.Now()
}
