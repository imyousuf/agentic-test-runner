package computer

import (
	"context"
	"fmt"
)

// LaunchApp starts the named application. Resolution is platform-specific:
// on Linux, the name is treated as either an executable in PATH or a
// .desktop application via gtk-launch / xdg-open; on macOS, "open -a NAME";
// on Windows, "Start-Process NAME" via PowerShell.
func (c *Computer) LaunchApp(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("app name must not be empty")
	}
	if err := c.Confirm(ctx, ActionDesc{Description: fmt.Sprintf("Launch app %q", name)}); err != nil {
		return err
	}
	return platformLaunchApp(name)
}

// QuitApp terminates running processes whose name matches name. Sends
// SIGTERM on Unix; uses taskkill on Windows.
func (c *Computer) QuitApp(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("app name must not be empty")
	}
	if err := c.Confirm(ctx, ActionDesc{Description: fmt.Sprintf("Quit app %q", name)}); err != nil {
		return err
	}
	return platformQuitApp(name)
}
