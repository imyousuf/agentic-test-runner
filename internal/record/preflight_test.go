package record

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingErrorSaysWhyAndHowToFixIt(t *testing.T) {
	err := &MissingError{
		Dependency:  "ffmpeg",
		Why:         "--encode=mp4 is set",
		Fix:         ffmpegFixes,
		Alternative: "drop --encode",
		Docs:        docsMP4,
	}
	msg := err.Error()

	for _, want := range []string{
		"ffmpeg", "--encode=mp4 is set", "drop --encode", docsMP4,
		"apt install ffmpeg", "brew install ffmpeg", "winget install",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not mention %q:\n%s", want, msg)
		}
	}
}

func TestTheHostPlatformFixComesFirst(t *testing.T) {
	fixes := orderFixes([]Fix{
		{Platform: "windows", Command: "w"},
		{Platform: hostPlatform(), Command: "mine"},
		{Platform: "macos", Command: "m"},
	})
	if fixes[0].Command != "mine" {
		t.Errorf("first fix is %q, want the host's own", fixes[0].Command)
	}
	if len(fixes) != 3 {
		t.Errorf("orderFixes lost a fix: %+v", fixes)
	}
}

func TestPreflightFailsOnAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	root := filepath.Join(t.TempDir(), "locked")
	if err := os.MkdirAll(root, 0o500); err != nil {
		t.Fatal(err)
	}

	checks := Preflight{Root: filepath.Join(root, "recordings"), Pages: 1}.Run()
	err := FirstError(checks)
	if err == nil {
		t.Fatal("Preflight passed on a directory it cannot write to")
	}
	if !strings.Contains(err.Error(), "--output") {
		t.Errorf("the error should offer --output:\n%v", err)
	}
}

func TestPreflightFailsWhenTheBrowserHasNoPage(t *testing.T) {
	checks := Preflight{Root: t.TempDir(), Pages: 0}.Run()
	err := FirstError(checks)
	if err == nil {
		t.Fatal("Preflight passed with no page open")
	}
	if !strings.Contains(err.Error(), "no open page") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestPreflightSkipsFFmpegWhenNoEncodeWasAsked(t *testing.T) {
	for _, c := range (Preflight{Root: t.TempDir(), Pages: 1}).Run() {
		if c.Name == "ffmpeg" {
			t.Error("ffmpeg was checked even though --encode is off")
		}
	}
}

func TestAWarningDoesNotFailPreflight(t *testing.T) {
	checks := []Check{
		{Name: "a", OK: true},
		{Name: "b", OK: false, Warn: true, Detail: "just a warning"},
	}
	if err := FirstError(checks); err != nil {
		t.Errorf("a warning failed the preflight: %v", err)
	}
}
