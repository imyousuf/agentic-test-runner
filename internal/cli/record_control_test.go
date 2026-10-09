package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The bug this replaced: "record" ran the recorder and ignored its arguments,
// so "atr record stop" started a recording instead of stopping one. Every typo
// did the same. The parent must refuse anything it does not know.
func TestRecordRefusesAnUnknownSubcommand(t *testing.T) {
	for _, arg := range []string{"stop-it", "bogus", "strt"} {
		cmd := newRecordCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{arg})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%q was accepted, and the recorder may have started", arg)
		}
	}
}

// Bare "atr record" records nothing now. It has to say so rather than exit 0
// and leave a script believing it started something.
func TestBareRecordFailsAndPointsAtStart(t *testing.T) {
	cmd := newRecordCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("bare \"record\" succeeded, so a script cannot tell it did nothing")
	}
	if !strings.Contains(err.Error(), "atr record start") {
		t.Errorf("the error does not name the command to use: %v", err)
	}
}

func TestRecordHasTheControlSubcommands(t *testing.T) {
	want := map[string]bool{
		"start": false, "stop": false, "status": false,
		"list": false, "encode": false, "repair": false, "rm": false, "doctor": false,
	}
	for _, sub := range newRecordCmd().Commands() {
		want[sub.Name()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("atr record has no %q subcommand", name)
		}
	}
}

// start takes every flag the old top-level command took, or a script that
// upgrades breaks on a flag that quietly disappeared.
func TestStartKeptEveryRecordingFlag(t *testing.T) {
	start := newRecordStartCmd()
	for _, name := range []string{
		"output", "title", "attach", "quality", "max-width", "fps",
		"max-duration", "max-size", "keep-last", "max-log-size", "redact-query",
		"heartbeat", "policy", "encode", "change-threshold", "keep-every", "keep-all",
	} {
		if start.Flags().Lookup(name) == nil {
			t.Errorf("atr record start lost --%s", name)
		}
	}
	if start.Flags().Lookup("foreground") == nil {
		t.Error("atr record start has no --foreground")
	}
}

func TestStopAndStatusRefuseStrayArguments(t *testing.T) {
	stop := newRecordStopCmd()
	stop.SetOut(&bytes.Buffer{})
	stop.SetErr(&bytes.Buffer{})
	stop.SetArgs([]string{"one", "two"})
	if err := stop.Execute(); err == nil {
		t.Error("stop took two ids")
	}

	status := newRecordStatusCmd()
	status.SetOut(&bytes.Buffer{})
	status.SetErr(&bytes.Buffer{})
	status.SetArgs([]string{"extra"})
	if err := status.Execute(); err == nil {
		t.Error("status took an argument")
	}
}

// Nothing running is not the same as a broken directory, and each has to say
// which it is.
func TestStopSaysWhenNothingIsRecording(t *testing.T) {
	dir := t.TempDir()
	cmd := newRecordStopCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output", dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("stopping nothing succeeded")
	}
	if !strings.Contains(err.Error(), "nothing is recording") {
		t.Errorf("unclear message: %v", err)
	}
}

func TestStopNamesAnIDThatIsNotRunning(t *testing.T) {
	dir := t.TempDir()
	// A directory that looks like a recording but has no live marker.
	if err := os.MkdirAll(dir+"/20260101-000000-old", 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := newRecordStopCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--output", dir, "20260101-000000-old"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("stopping a recording that is not running succeeded")
	}
	if !strings.Contains(err.Error(), "nothing is recording") {
		t.Errorf("unclear message: %v", err)
	}
}

func TestChildReasonDropsTheDuplicatedPrefix(t *testing.T) {
	cases := map[string]string{
		"Error: no browser found\n":            "no browser found",
		"ATR recording\nError: disk is full\n": "disk is full",
		"  Error: trailing space  ":            "trailing space",
		"":                                     "",
		"   \n  ":                              "",
		"plain failure":                        "plain failure",
	}
	for in, want := range cases {
		if got := childReason([]byte(in)); got != want {
			t.Errorf("childReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetachAttrIsSet(t *testing.T) {
	if detachAttr() == nil {
		t.Fatal("the recorder would stay in the caller's process group")
	}
}

// The recorder is this same binary re-run with --foreground appended. If the
// flag were ever dropped the child would spawn a child of its own, forever.
func TestStartAcceptsForegroundTwice(t *testing.T) {
	cmd := newRecordStartCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--foreground", "--foreground"})
	// It fails on the browser, not on the flags, which is all this checks.
	if err := cmd.Flags().Parse([]string{"--foreground", "--foreground"}); err != nil {
		t.Fatalf("repeating --foreground is a parse error: %v", err)
	}
	if fg, _ := cmd.Flags().GetBool("foreground"); !fg {
		t.Error("--foreground did not survive being passed twice")
	}
}

// A smoke test over the built binary, so the wiring is checked the way a caller
// meets it. It needs no browser: every case here fails before attaching.
func TestBinaryRejectsTheOldFootguns(t *testing.T) {
	exe := buildATR(t)
	for _, args := range [][]string{
		{"record", "stop-recording"},
		{"record", "begin"},
		{"record"},
	} {
		out, err := exec.Command(exe, args...).CombinedOutput()
		if err == nil {
			t.Errorf("atr %s exited 0:\n%s", strings.Join(args, " "), out)
		}
		if strings.Contains(string(out), "ATR recording") {
			t.Errorf("atr %s started a recording:\n%s", strings.Join(args, " "), out)
		}
	}
}

func buildATR(t *testing.T) string {
	t.Helper()
	exe := t.TempDir() + "/atr"
	build := exec.Command("go", "build", "-o", exe, "../../cmd/atr")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build atr here: %v\n%s", err, out)
	}
	return exe
}
