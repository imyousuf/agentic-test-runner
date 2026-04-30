// Package computer provides cross-platform desktop control primitives:
// mouse, keyboard, screen capture, and window/app management.
//
// It is the desktop counterpart to internal/browser, exposed via the same
// daemon + REST + CLI + MCP pattern. A configurable countdown gate runs
// before every action so a human can intervene with Ctrl+C.
//
// Linux support is X11-only for v1; Wayland is tracked as future work.
package computer

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// Mode controls when the safety gate prompts before an action.
type Mode string

const (
	// ModePerRequest prompts on every action (default).
	ModePerRequest Mode = "per-request"
	// ModePerApp prompts on the first action against an app, then auto-approves
	// subsequent actions on that app for the daemon's lifetime.
	ModePerApp Mode = "per-app"
	// ModeOff disables prompts. Explicit opt-in for trusted batch runs.
	ModeOff Mode = "off"
)

// IsValid reports whether m is a known mode value.
func (m Mode) IsValid() bool {
	switch m {
	case ModePerRequest, ModePerApp, ModeOff:
		return true
	}
	return false
}

// Config holds runtime configuration for the Computer.
type Config struct {
	// CountdownMode selects when the gate prompts (per-request, per-app, off).
	CountdownMode Mode
	// CountdownSeconds is the number of seconds to count down before each
	// gated action. Must be >= 1 when the gate is active.
	CountdownSeconds int
	// GUIEnabled toggles the optional webview overlay (M5).
	GUIEnabled bool
	// DefaultDisplay is the monitor index used when callers don't specify one.
	DefaultDisplay int
	// Output is where the gate writes countdown messages. Defaults to os.Stderr.
	Output io.Writer
}

// DefaultConfig returns sensible defaults: per-request mode, 3-second countdown.
func DefaultConfig() Config {
	return Config{
		CountdownMode:    ModePerRequest,
		CountdownSeconds: 3,
		GUIEnabled:       true,
		DefaultDisplay:   0,
		Output:           os.Stderr,
	}
}

// Computer is the long-lived desktop controller. One per daemon process.
type Computer struct {
	cfg    Config
	logger *log.Logger
	gui    *gui

	appsMu       sync.Mutex
	approvedApps map[string]time.Time // app-id -> approval time
}

// New constructs a Computer from cfg.
func New(cfg Config) (*Computer, error) {
	if !cfg.CountdownMode.IsValid() {
		return nil, fmt.Errorf("invalid countdown mode %q", cfg.CountdownMode)
	}
	if cfg.CountdownMode != ModeOff && cfg.CountdownSeconds < 1 {
		return nil, fmt.Errorf("countdown seconds must be >= 1, got %d", cfg.CountdownSeconds)
	}
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}
	logger := log.New(cfg.Output, "[atr-computer] ", log.LstdFlags)
	return &Computer{
		cfg:          cfg,
		logger:       logger,
		gui:          newGUI(cfg.GUIEnabled, logger),
		approvedApps: make(map[string]time.Time),
	}, nil
}

// Close releases resources. Safe to call multiple times.
func (c *Computer) Close() error {
	return nil
}

// Mode returns the currently configured countdown mode.
func (c *Computer) Mode() Mode { return c.cfg.CountdownMode }

// CountdownSeconds returns the configured countdown duration.
func (c *Computer) CountdownSeconds() int { return c.cfg.CountdownSeconds }

// ApprovedAppCount returns the number of apps in the per-app approval cache.
func (c *Computer) ApprovedAppCount() int {
	c.appsMu.Lock()
	defer c.appsMu.Unlock()
	return len(c.approvedApps)
}

// ResetApprovals clears the per-app approval cache.
func (c *Computer) ResetApprovals() {
	c.appsMu.Lock()
	defer c.appsMu.Unlock()
	c.approvedApps = make(map[string]time.Time)
}
