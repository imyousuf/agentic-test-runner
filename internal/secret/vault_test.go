package secret

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFetchByCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{})

	got, err := v.Fetch(context.Background(), Request{Command: "echo hunter2"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got != "hunter2" {
		// The trailing newline echo adds is not part of the secret.
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestFetchKeepTrailingNewline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{KeepTrailingNewline: true})

	got, err := v.Fetch(context.Background(), Request{Command: "echo hunter2"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got != "hunter2\n" {
		t.Errorf("got %q, want %q", got, "hunter2\n")
	}
}

func TestFetchByRef(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{Refs: map[string]string{"github/password": "echo s3cret"}})

	got, err := v.Fetch(context.Background(), Request{Ref: "github/password"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("got %q, want %q", got, "s3cret")
	}
}

func TestFetchUnknownRefListsConfigured(t *testing.T) {
	v := New(Config{Refs: map[string]string{"github/password": "echo x"}})

	_, err := v.Fetch(context.Background(), Request{Ref: "gitlab/password"})
	if err == nil {
		t.Fatal("want an error for an unknown ref")
	}
	if !strings.Contains(err.Error(), "github/password") {
		t.Errorf("error should name the configured refs so the model can correct itself, got: %v", err)
	}
}

func TestFetchRejectsAmbiguousAndEmptyRequests(t *testing.T) {
	v := New(Config{Refs: map[string]string{"a": "echo x"}})

	if _, err := v.Fetch(context.Background(), Request{Ref: "a", Command: "echo y"}); err == nil {
		t.Error("want an error when both ref and command are given")
	}
	if _, err := v.Fetch(context.Background(), Request{}); err == nil {
		t.Error("want an error when neither ref nor command is given")
	}
}

// A failing command must surface stderr — the model needs it to fix a wrong
// entry name — while stdout stays withheld, because a partially-successful
// manager can print secret material before exiting non-zero.
func TestFetchFailureReportsStderrNotStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{})

	_, err := v.Fetch(context.Background(), Request{
		Command: "echo LEAKED_SECRET; echo 'entry not found' >&2; exit 1",
	})
	if err == nil {
		t.Fatal("want an error from a non-zero exit")
	}
	if strings.Contains(err.Error(), "LEAKED_SECRET") {
		t.Errorf("stdout must never reach the error message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "entry not found") {
		t.Errorf("stderr should be surfaced, got: %v", err)
	}
}

func TestFetchEmptyOutputIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{})

	if _, err := v.Fetch(context.Background(), Request{Command: "true"}); err == nil {
		t.Error("want an error when the command prints nothing: filling a field with an empty password silently is worse than failing")
	}
}

func TestFetchTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{Timeout: 100 * time.Millisecond})

	start := time.Now()
	_, err := v.Fetch(context.Background(), Request{Command: "sleep 10"})
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Fetch did not honour the timeout, took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should identify the timeout, got: %v", err)
	}
}

func TestRedactScrubsFetchedValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{Refs: map[string]string{"github/password": "echo correct-horse"}})

	if _, err := v.Fetch(context.Background(), Request{Ref: "github/password"}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got := v.Redact("the page echoed correct-horse back at us")
	if strings.Contains(got, "correct-horse") {
		t.Errorf("Redact left the secret in place: %q", got)
	}
	if !strings.Contains(got, "github/password") {
		t.Errorf("Redact should say which secret it scrubbed, got: %q", got)
	}
}

// Short values are left alone: they collide with ordinary words, and
// scrubbing them would corrupt more output than it protects.
func TestRedactIgnoresShortValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command")
	}
	v := New(Config{})

	if _, err := v.Fetch(context.Background(), Request{Command: "echo abc"}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	const text = "abc appears in ordinary output"
	if got := v.Redact(text); got != text {
		t.Errorf("short value should not be redacted, got: %q", got)
	}
}

func TestRedactIsANoOpWithoutFetches(t *testing.T) {
	v := New(Config{})
	const text = "nothing to scrub here"
	if got := v.Redact(text); got != text {
		t.Errorf("got %q, want %q", got, text)
	}
}
