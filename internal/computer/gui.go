package computer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// guiBackend describes a discovered GUI overlay implementation.
type guiBackend int

const (
	guiBackendNone   guiBackend = iota // no GUI; terminal-only
	guiBackendZenity                   // Linux: zenity --progress (abortable)
	guiBackendNotify                   // Linux: notify-send (visual only, not abortable)
	guiBackendOsa                      // macOS: osascript display notification (visual only)
	guiBackendPwsh                     // Windows: PowerShell toast (visual only)
)

// gui shows the countdown overlay and is responsible for:
//   - rendering a visible interrupt window if a backend is available
//   - returning an "abort requested" signal when the user clicks the GUI's
//     cancel button (only the zenity backend supports this today)
//
// The terminal countdown in gate.go runs independently — gui is purely
// additive. If no backend can initialize, ShowCountdown is a no-op and the
// terminal countdown remains the sole source of feedback.
type gui struct {
	enabled bool
	backend guiBackend
	logger  interface{ Printf(format string, v ...any) }

	initOnce sync.Once
	initErr  error
}

// newGUI constructs a gui instance. Backend probing happens lazily on first
// ShowCountdown so daemon startup stays cheap and never fails because of
// missing tools.
func newGUI(enabled bool, logger interface{ Printf(format string, v ...any) }) *gui {
	return &gui{enabled: enabled, logger: logger}
}

// probe selects the best available backend for the current OS. Idempotent.
func (g *gui) probe() {
	g.initOnce.Do(func() {
		if !g.enabled {
			g.backend = guiBackendNone
			return
		}
		switch runtime.GOOS {
		case "linux":
			if hasCommand("zenity") {
				g.backend = guiBackendZenity
				return
			}
			if hasCommand("notify-send") {
				g.backend = guiBackendNotify
				return
			}
		case "darwin":
			if hasCommand("osascript") {
				g.backend = guiBackendOsa
				return
			}
		case "windows":
			if hasCommand("powershell") || hasCommand("powershell.exe") {
				g.backend = guiBackendPwsh
				return
			}
		}
		g.backend = guiBackendNone
		g.initErr = errors.New("no GUI overlay backend available; falling back to terminal-only")
		if g.logger != nil {
			g.logger.Printf("GUI overlay disabled: %v", g.initErr)
		}
	})
}

// ShowCountdown opens the GUI overlay for the given action. It blocks until
// either:
//   - seconds elapse (returns nil)
//   - ctx is cancelled (returns ctx.Err())
//   - the user clicks the GUI's cancel/abort button (returns ErrAborted)
//
// When no GUI backend is configured or available it returns immediately with
// nil — the gate's terminal countdown is the sole source of feedback in that
// case. ShowCountdown does NOT itself enforce the countdown duration when no
// backend is available; the caller (gate.runCountdown) is authoritative for
// timing.
func (g *gui) ShowCountdown(ctx context.Context, action ActionDesc, seconds int) error {
	if !g.enabled {
		return nil
	}
	g.probe()
	switch g.backend {
	case guiBackendNone:
		return nil
	case guiBackendZenity:
		return g.runZenity(ctx, action, seconds)
	case guiBackendNotify:
		g.runNotifySend(action, seconds)
		return nil
	case guiBackendOsa:
		g.runOsascript(action, seconds)
		return nil
	case guiBackendPwsh:
		g.runPowerShellToast(action, seconds)
		return nil
	default:
		return nil
	}
}

// runZenity launches `zenity --progress` and feeds it percentages over stdin.
// Returns ErrAborted if the user clicks Cancel; nil if the countdown completes
// or ctx is cancelled (the caller's gate handles ctx separately).
func (g *gui) runZenity(ctx context.Context, action ActionDesc, seconds int) error {
	if seconds < 1 {
		return nil
	}
	title := "ATR Computer Use"
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}
	text := fmt.Sprintf("About to: %s\n\nCancel to abort.", desc)

	// Run zenity in a separate context so we can kill it cleanly.
	zCtx, zCancel := context.WithCancel(ctx)
	defer zCancel()

	// #nosec G204 — args are constants/internal-only.
	cmd := exec.CommandContext(zCtx, "zenity",
		"--progress",
		"--title="+title,
		"--text="+text,
		"--percentage=0",
		"--auto-close",
		"--width=360",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		if g.logger != nil {
			g.logger.Printf("zenity stdin: %v", err)
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		if g.logger != nil {
			g.logger.Printf("zenity start: %v", err)
		}
		return nil
	}

	// Feed percentages on a 100ms cadence so the bar animates smoothly.
	totalTicks := seconds * 10
	tickCh := time.NewTicker(100 * time.Millisecond)
	defer tickCh.Stop()

	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		defer func() { _ = stdin.Close() }()
		for i := 1; i <= totalTicks; i++ {
			select {
			case <-zCtx.Done():
				return
			case <-tickCh.C:
				pct := (i * 100) / totalTicks
				if _, werr := io.WriteString(stdin, fmt.Sprintf("%d\n", pct)); werr != nil {
					return
				}
			}
		}
	}()

	// Wait for zenity to exit. Non-zero exit code (1 specifically) means user
	// clicked Cancel.
	waitErr := cmd.Wait()
	<-feedDone

	if ctx.Err() != nil {
		// Outer context cancellation — gate already handles this; report nil
		// so we don't double-report ErrAborted.
		return nil
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			return ErrAborted
		}
		// Other exit errors (e.g. zenity missing on PATH at runtime, killed
		// by signal) → log and treat as no-op so terminal countdown still
		// gates the action.
		if g.logger != nil {
			g.logger.Printf("zenity exited unexpectedly: %v", waitErr)
		}
		return nil
	}
	return nil
}

// runNotifySend sends a one-shot desktop notification. Visual only — not
// abortable from the notification itself.
func (g *gui) runNotifySend(action ActionDesc, seconds int) {
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}
	body := fmt.Sprintf("%s\nStarts in %ds. Press Ctrl+C in the daemon terminal to abort.", desc, seconds)
	// #nosec G204
	cmd := exec.Command("notify-send", "--urgency=normal", "--expire-time", fmt.Sprintf("%d", seconds*1000),
		"ATR: about to act", body)
	if err := cmd.Start(); err != nil {
		if g.logger != nil {
			g.logger.Printf("notify-send start: %v", err)
		}
		return
	}
	// Detach — don't block the gate on the notification process.
	go func() { _ = cmd.Wait() }()
}

// runOsascript shows a macOS notification via osascript. Visual only.
func (g *gui) runOsascript(action ActionDesc, seconds int) {
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}
	body := fmt.Sprintf("%s\nStarts in %ds. Ctrl+C in daemon terminal to abort.", desc, seconds)
	script := fmt.Sprintf(`display notification %q with title "ATR Computer Use"`, body)
	// #nosec G204
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Start(); err != nil {
		if g.logger != nil {
			g.logger.Printf("osascript start: %v", err)
		}
		return
	}
	go func() { _ = cmd.Wait() }()
}

// runPowerShellToast shows a Windows toast via PowerShell. Visual only.
func (g *gui) runPowerShellToast(action ActionDesc, seconds int) {
	desc := action.Description
	if action.AppID != "" {
		desc = fmt.Sprintf("%s on %s", desc, action.AppID)
	}
	body := fmt.Sprintf("%s\\nStarts in %ds. Ctrl+C in daemon terminal to abort.", desc, seconds)
	// Use Windows Forms balloon notification — most universal across Windows
	// versions, no external modules required.
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms;",
		"$n = New-Object System.Windows.Forms.NotifyIcon;",
		"$n.Icon = [System.Drawing.SystemIcons]::Information;",
		"$n.BalloonTipTitle = 'ATR Computer Use';",
		fmt.Sprintf("$n.BalloonTipText = '%s';", body),
		"$n.Visible = $true;",
		fmt.Sprintf("$n.ShowBalloonTip(%d);", seconds*1000),
		fmt.Sprintf("Start-Sleep -Milliseconds %d;", seconds*1000),
		"$n.Dispose();",
	}, " ")
	// #nosec G204
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err := cmd.Start(); err != nil {
		if g.logger != nil {
			g.logger.Printf("powershell start: %v", err)
		}
		return
	}
	go func() { _ = cmd.Wait() }()
}

// hasCommand reports whether name is found on PATH.
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// BackendName returns a human-readable label for the active GUI backend.
// Used by tests and status reporting.
func (g *gui) BackendName() string {
	g.probe()
	switch g.backend {
	case guiBackendZenity:
		return "zenity"
	case guiBackendNotify:
		return "notify-send"
	case guiBackendOsa:
		return "osascript"
	case guiBackendPwsh:
		return "powershell-toast"
	default:
		return "none"
	}
}
