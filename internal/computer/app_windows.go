//go:build windows

package computer

import (
	"fmt"
	"os/exec"
)

func platformLaunchApp(name string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Start-Process -FilePath %q", name))
	return cmd.Start()
}

func platformQuitApp(name string) error {
	out, err := exec.Command("taskkill", "/IM", name, "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("taskkill %s: %v (%s)", name, err, string(out))
	}
	return nil
}
