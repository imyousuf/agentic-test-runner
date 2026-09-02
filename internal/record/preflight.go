package record

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Fix is one command that installs a missing dependency on one platform.
type Fix struct {
	Platform string // "debian", "fedora", "arch", "macos", "windows"
	Command  string
}

// MissingError says that a dependency is not available, why this run needs it,
// and what to type to fix it.
//
// A bare "exec: ffmpeg: not found" makes the person go and search. The error
// already knows which flag asked for ffmpeg and which platform it is running
// on, so it should say both.
type MissingError struct {
	Dependency  string
	Why         string
	Fix         []Fix
	Alternative string // empty when there is no way around it
	Docs        string
}

func (e *MissingError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "atr record needs %s, and it is not available.\n\n", e.Dependency)
	if e.Why != "" {
		fmt.Fprintf(&b, "  Why   %s\n", e.Why)
	}
	for i, f := range orderFixes(e.Fix) {
		label := "  Fix   "
		if i > 0 {
			label = "        "
		}
		fmt.Fprintf(&b, "%s%-38s(%s)\n", label, f.Command, platformName(f.Platform))
	}
	if e.Alternative != "" {
		fmt.Fprintf(&b, "  Or    %s\n", e.Alternative)
	}
	if e.Docs != "" {
		fmt.Fprintf(&b, "  Docs  %s\n", e.Docs)
	}
	return strings.TrimRight(b.String(), "\n")
}

// orderFixes puts the host's own platform first, so the command a person needs
// is the first one they read.
func orderFixes(fixes []Fix) []Fix {
	want := hostPlatform()
	out := make([]Fix, 0, len(fixes))
	for _, f := range fixes {
		if f.Platform == want {
			out = append(out, f)
		}
	}
	for _, f := range fixes {
		if f.Platform != want {
			out = append(out, f)
		}
	}
	return out
}

func hostPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return linuxFamily()
	}
}

// linuxFamily guesses the package manager from what is on PATH. A guess is
// enough here, because every command is printed anyway.
func linuxFamily() string {
	for _, c := range []struct{ bin, family string }{
		{"apt", "debian"}, {"dnf", "fedora"}, {"pacman", "arch"},
	} {
		if _, err := exec.LookPath(c.bin); err == nil {
			return c.family
		}
	}
	return "debian"
}

func platformName(p string) string {
	switch p {
	case "debian":
		return "Debian, Ubuntu"
	case "fedora":
		return "Fedora, RHEL"
	case "arch":
		return "Arch"
	case "macos":
		return "macOS"
	case "windows":
		return "Windows"
	}
	return p
}

var ffmpegFixes = []Fix{
	{Platform: "debian", Command: "sudo apt install ffmpeg"},
	{Platform: "fedora", Command: "sudo dnf install ffmpeg"},
	{Platform: "arch", Command: "sudo pacman -S ffmpeg"},
	{Platform: "macos", Command: "brew install ffmpeg"},
	{Platform: "windows", Command: "winget install Gyan.FFmpeg"},
}

const docsMP4 = "docs/session-recording.md#11-mp4-export"

// Check is one preflight result.
type Check struct {
	Name   string
	OK     bool
	Detail string
	Warn   bool // a failed warning does not stop the recording
	Err    error
}

// Preflight describes what to verify before capture begins.
type Preflight struct {
	Root   string // the recordings directory
	Encode bool   // an MP4 is wanted when the recording stops
	CDPURL string // set when the endpoint is already known
	Pages  int    // how many pages the browser has; -1 when not checked
}

// MinFreeBytes is the free space a recording needs before it will start.
//
// A recording of a busy page measured about 7 MB per minute in the spike, so
// this is roughly an hour and a half of headroom.
const MinFreeBytes int64 = 512 << 20

// Run performs the checks and returns them in the order they were made.
//
// Preflight runs before capture, never after. An error that arrives after
// twenty minutes of recording has already wasted the twenty minutes. The one
// exception is a dependency lost during a recording: that never destroys the
// frames already on the disk.
func (p Preflight) Run() []Check {
	var out []Check

	out = append(out, checkDir(p.Root))
	out = append(out, checkFree(p.Root))

	if p.Pages == 0 {
		out = append(out, Check{
			Name: "browser page", OK: false,
			Detail: "the browser has no page to record",
			Err: fmt.Errorf(
				"the browser has no open page. Open one, or run \"atr browser navigate <url>\" first"),
		})
	} else if p.Pages > 0 {
		out = append(out, Check{
			Name: "browser page", OK: true,
			Detail: fmt.Sprintf("%d page(s)", p.Pages),
		})
	}

	if p.Encode {
		out = append(out, checkFFmpeg(), checkEncoder())
	}
	return out
}

// FirstError returns the error of the first failed check that is not a
// warning.
func FirstError(checks []Check) error {
	for _, c := range checks {
		if !c.OK && !c.Warn {
			if c.Err != nil {
				return c.Err
			}
			return fmt.Errorf("%s: %s", c.Name, c.Detail)
		}
	}
	return nil
}

func checkDir(root string) Check {
	c := Check{Name: "recordings directory", Detail: root}
	if err := os.MkdirAll(root, 0o755); err != nil {
		c.Err = fmt.Errorf(
			"cannot create the recordings directory %s: %w.\n"+
				"  Fix   choose another directory with --output <dir>", root, err)
		return c
	}
	probe := filepath.Join(root, ".atr-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		c.Err = fmt.Errorf(
			"the recordings directory %s is not writable: %w.\n"+
				"  Fix   choose another directory with --output <dir>", root, err)
		return c
	}
	_ = os.Remove(probe)
	c.OK = true
	return c
}

func checkFFmpeg() Check {
	c := Check{Name: "ffmpeg"}
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		c.Detail = "not on PATH"
		c.Err = &MissingError{
			Dependency: "ffmpeg",
			Why:        "--encode=mp4 is set, so the recording is encoded when it stops",
			Fix:        ffmpegFixes,
			Alternative: "drop --encode. The recording still plays in \"atr remote\", " +
				"which needs no ffmpeg.\n        Encode it later with \"atr record encode <id>\".",
			Docs: docsMP4,
		}
		return c
	}
	c.OK = true
	c.Detail = path
	return c
}

func checkEncoder() Check {
	c := Check{Name: "libx264"}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		// checkFFmpeg already reported this. Do not say it twice.
		c.OK = true
		return c
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		c.Warn = true
		c.Detail = "could not ask ffmpeg which encoders it has"
		return c
	}
	if !strings.Contains(string(out), "libx264") {
		c.Detail = "this ffmpeg has no libx264"
		c.Err = &MissingError{
			Dependency: "an ffmpeg with libx264",
			Why:        "the MP4 export encodes H.264, which browsers can play",
			Fix:        ffmpegFixes,
			Alternative: "drop --encode and play the recording in \"atr remote\", " +
				"which needs no encoder.",
			Docs: docsMP4,
		}
		return c
	}
	c.OK = true
	c.Detail = "present"
	return c
}
