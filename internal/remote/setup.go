package remote

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// SetupOptions describes the service to install.
type SetupOptions struct {
	Port int
	Bind string
	FPS  int
}

// SetupResult reports what the setup did, so the caller can print it.
type SetupResult struct {
	Platform    string
	ServicePath string
	URL         string
	Installed   bool
	Notes       []string
}

const serviceName = "atr-remote"

func atrDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find the home directory: %w", err)
	}
	dir := filepath.Join(home, ".atr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return dir, nil
}

// ServicePath returns where the service file belongs on this platform.
func ServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find the home directory: %w", err)
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", serviceName+".service"), nil
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "com.atr.remote.plist"), nil
	default:
		return "", fmt.Errorf("no service support for %s; run \"atr remote\" yourself", runtime.GOOS)
	}
}

// Setup installs a service that keeps the live view running.
//
// It does not look for a browser. The browser belongs to ATR itself, and the
// live view simply attaches to whichever one ATR is driving.
func Setup(opts SetupOptions) (*SetupResult, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to find the running binary: %w", err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", binary, err)
	}

	servicePath, err := ServicePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", filepath.Dir(servicePath), err)
	}

	result := &SetupResult{
		Platform:    runtime.GOOS,
		ServicePath: servicePath,
		URL:         fmt.Sprintf("http://%s:%d/", opts.Bind, opts.Port),
	}

	switch runtime.GOOS {
	case "linux":
		err = setupSystemd(servicePath, binary, opts, result)
	case "darwin":
		err = setupLaunchd(servicePath, binary, opts, result)
	}
	if err != nil {
		return nil, err
	}
	result.Installed = true
	return result, nil
}

func setupSystemd(servicePath, binary string, opts SetupOptions, result *SetupResult) error {
	unit := fmt.Sprintf(`[Unit]
Description=ATR browser live view
After=network-online.target

[Service]
Type=simple
ExecStart="%s" remote --port %d --bind %s --fps %d
Restart=always
# "atr remote setup" installs no browser by design, so "no browser running" is the
# normal state right after install and the unit will exit until one appears.
# Keep the retry cheap rather than respawning every few seconds.
RestartSec=15

[Install]
WantedBy=default.target
`, binary, opts.Port, opts.Bind, opts.FPS)

	if err := os.WriteFile(servicePath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", servicePath, err)
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		result.Notes = append(result.Notes,
			"systemctl was not found. Start the live view yourself with \"atr remote\".")
		return nil
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", serviceName + ".service"},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
	}

	// A user service stops at logout unless lingering is on, and that needs
	// root. Report the command instead of running it.
	if !lingerEnabled() {
		result.Notes = append(result.Notes,
			"The service stops when you log out. To keep it running after a reboot, run:\n"+
				"    sudo loginctl enable-linger "+currentUser())
	}
	return nil
}

// currentUser prefers os/user over $USER, which is empty under cron, in
// containers, and in ssh non-login invocations.
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func lingerEnabled() bool {
	name := currentUser()
	if name == "" {
		return false
	}
	out, err := exec.Command("loginctl", "show-user", name, "--property=Linger").Output()
	return err == nil && strings.Contains(string(out), "Linger=yes")
}

// xmlEscape makes a value safe to interpolate into the plist. A home directory
// containing "&" would otherwise produce XML that launchctl cannot parse.
func xmlEscape(v string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(v)); err != nil {
		return v
	}
	return buf.String()
}

func setupLaunchd(servicePath, binary string, opts SetupOptions, result *SetupResult) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.atr.remote</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>remote</string>
    <string>--port</string>
    <string>%d</string>
    <string>--bind</string>
    <string>%s</string>
    <string>--fps</string>
    <string>%d</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
</dict>
</plist>
`, xmlEscape(binary), opts.Port, xmlEscape(opts.Bind), opts.FPS)

	// No secret in it any more, so ordinary permissions.
	if err := os.WriteFile(servicePath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", servicePath, err)
	}

	target := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", target+"/com.atr.remote").Run()
	if out, err := exec.Command("launchctl", "bootstrap", target, servicePath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall removes the service. It keeps the token, so a later setup gives
// the same URL.
func Uninstall() (string, error) {
	servicePath, err := ServicePath()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "--now", serviceName+".service").Run()
	case "darwin":
		_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/com.atr.remote").Run()
	}

	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to remove %s: %w", servicePath, err)
	}
	if runtime.GOOS == "linux" {
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return servicePath, nil
}

// Status reports whether the service file exists and whether it is running.
func Status() (installed bool, running bool, path string) {
	path, err := ServicePath()
	if err != nil {
		return false, false, ""
	}
	if _, err := os.Stat(path); err != nil {
		return false, false, path
	}

	switch runtime.GOOS {
	case "linux":
		out, _ := exec.Command("systemctl", "--user", "is-active", serviceName+".service").Output()
		return true, strings.TrimSpace(string(out)) == "active", path
	case "darwin":
		err := exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/com.atr.remote").Run()
		return true, err == nil, path
	}
	return true, false, path
}
