//go:build darwin

package computer

import (
	"fmt"
	"os/exec"
)

func platformLaunchApp(name string) error {
	if err := exec.Command("open", "-a", name).Start(); err != nil {
		return fmt.Errorf("open -a %s: %w", name, err)
	}
	return nil
}

func platformQuitApp(name string) error {
	// Pass `name` as an osascript run-arg rather than interpolating it into
	// a script string. Go's %q produces Go-style quoting which differs from
	// AppleScript's; a name containing `"` would otherwise break out of the
	// string literal and execute arbitrary code.
	script := `on run argv
		tell application (item 1 of argv) to quit
	end run`
	out, err := exec.Command("osascript", "-e", script, "--", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript quit: %v (%s)", err, string(out))
	}
	return nil
}
