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
// ctx for cancellation. Returns ErrAborted on cancellation. When the GUI
// overlay is enabled and a backend is available, it also surfaces the
// countdown there in parallel and merges its abort signal with ctx.
func (c *Computer) runCountdown(ctx context.Context, action ActionDesc) error {
	seconds := c.cfg.CountdownSeconds
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}

	// Wrap ctx so a GUI cancel button (or any other source) can trigger abort.
	gateCtx, cancelGate := context.WithCancel(ctx)
	defer cancelGate()

	// Launch the GUI overlay in parallel. ShowCountdown is a no-op when GUI
	// is disabled or no backend exists. If the user clicks the GUI's cancel
	// button we cancel gateCtx so the terminal countdown loop below sees
	// abort on its next 100ms tick.
	guiDone := make(chan error, 1)
	if c.gui != nil {
		go func() {
			guiDone <- c.gui.ShowCountdown(gateCtx, action, seconds)
		}()
	} else {
		close(guiDone)
	}

	fmt.Fprintf(c.cfg.Output, "[atr] About to: %s\n[atr]   Press Ctrl+C to abort. ", desc)

	guiAborted := false
loop:
	for remaining := seconds; remaining > 0; remaining-- {
		fmt.Fprintf(c.cfg.Output, "%d... ", remaining)
		// Use a 1-second tick made of 10×100ms slices for responsive cancellation.
		for range 10 {
			select {
			case <-ctx.Done():
				fmt.Fprintln(c.cfg.Output, "\n[atr] Aborted.")
				cancelGate()
				if guiDone != nil {
					select {
					case <-guiDone:
					case <-time.After(500 * time.Millisecond):
					}
				}
				return ErrAborted
			case err, ok := <-guiDone:
				if ok && err == ErrAborted {
					guiAborted = true
					fmt.Fprintln(c.cfg.Output, "\n[atr] Aborted (GUI).")
					break loop
				}
				// GUI channel closed or returned nil → keep counting; nil out
				// so we don't keep selecting on a closed channel.
				guiDone = nil
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if guiAborted {
		cancelGate()
		// guiAborted implies we already drained guiDone via the GUI case,
		// so no further wait is needed.
		return ErrAborted
	}
	// Tell any in-flight GUI process to tear down, then wait briefly for it
	// so we don't leak goroutines. zenity exits within milliseconds when its
	// stdin closes (or its parent context cancels).
	cancelGate()
	if guiDone != nil {
		select {
		case <-guiDone:
		case <-time.After(500 * time.Millisecond):
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
