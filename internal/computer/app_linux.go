//go:build linux

package computer

import (
	"fmt"
	"os/exec"
)

// platformLaunchApp launches name on Linux.
//
// Strategy:
//  1. If name is on PATH, exec it directly (covers terminal apps, browsers).
//  2. Otherwise try gtk-launch (.desktop file lookup).
//  3. Fall back to xdg-open (handles URLs and files cleanly; sometimes apps).
func platformLaunchApp(name string) error {
	if path, err := exec.LookPath(name); err == nil {
		cmd := exec.Command(path)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start %s: %w", path, err)
		}
		_ = cmd.Process.Release()
		return nil
	}
	if _, err := exec.LookPath("gtk-launch"); err == nil {
		if err := exec.Command("gtk-launch", name).Start(); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", name).Start()
	}
	return fmt.Errorf("no launcher available for %q (tried PATH, gtk-launch, xdg-open)", name)
}

// platformQuitApp sends SIGTERM to processes matching name via pkill.
func platformQuitApp(name string) error {
	if _, err := exec.LookPath("pkill"); err != nil {
		return fmt.Errorf("pkill not found in PATH: %w", err)
	}
	out, err := exec.Command("pkill", "-x", name).CombinedOutput()
	if err != nil {
		// pkill returns 1 when no matches; treat as informative error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return fmt.Errorf("no running process named %q", name)
		}
		return fmt.Errorf("pkill %s: %v (%s)", name, err, string(out))
	}
	return nil
}
