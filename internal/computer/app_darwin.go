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
	script := fmt.Sprintf(`tell application %q to quit`, name)
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript quit: %v (%s)", err, string(out))
	}
	return nil
}
