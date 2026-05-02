package computer

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestComputer(t *testing.T, mode Mode, seconds int) (*Computer, *bytes.Buffer) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// vcaesar/screenshot's NumActiveDisplays() trips a checkptr
		// pointer-arithmetic violation under `go test -race` on the
		// Windows GitHub runner. Computer is documented as Linux-X11
		// only in v1, so skip rather than chase the upstream bug.
		t.Skip("internal/computer tests skipped on Windows: vcaesar/screenshot checkptr violation under -race")
	}
	buf := &bytes.Buffer{}
	c, err := New(Config{
		CountdownMode:    mode,
		CountdownSeconds: seconds,
		Output:           buf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, buf
}

func TestNewRejectsInvalidMode(t *testing.T) {
	_, err := New(Config{CountdownMode: Mode("bogus"), CountdownSeconds: 3})
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
}

func TestNewRejectsZeroSecondsWhenGated(t *testing.T) {
	_, err := New(Config{CountdownMode: ModePerRequest, CountdownSeconds: 0})
	if err == nil {
		t.Fatal("expected error for zero seconds with active gate, got nil")
	}
}

func TestNewAcceptsZeroSecondsWhenOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("internal/computer skipped on Windows: vcaesar/screenshot checkptr violation under -race")
	}
	if _, err := New(Config{CountdownMode: ModeOff}); err != nil {
		t.Fatalf("expected zero seconds OK with mode=off, got: %v", err)
	}
}

func TestConfirmOffSkipsCountdown(t *testing.T) {
	c, buf := newTestComputer(t, ModeOff, 3)
	start := time.Now()
	if err := c.Confirm(context.Background(), ActionDesc{Description: "click"}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("expected immediate return in mode=off, took %v", elapsed)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output in mode=off, got %q", buf.String())
	}
}

func TestConfirmPerRequestRunsCountdown(t *testing.T) {
	c, buf := newTestComputer(t, ModePerRequest, 1)
	if err := c.Confirm(context.Background(), ActionDesc{Description: "click"}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "About to: click") {
		t.Errorf("expected action description in output, got %q", out)
	}
	if !strings.Contains(out, "1...") {
		t.Errorf("expected countdown tick in output, got %q", out)
	}
	if !strings.Contains(out, "go.") {
		t.Errorf("expected completion marker in output, got %q", out)
	}
}

func TestConfirmAbortsOnCanceledContext(t *testing.T) {
	c, buf := newTestComputer(t, ModePerRequest, 5)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Confirm(ctx, ActionDesc{Description: "click"})
	}()

	// Cancel after 200ms (well within the 5s countdown)
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != ErrAborted {
			t.Errorf("expected ErrAborted, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm did not return after context cancellation")
	}
	if !strings.Contains(buf.String(), "Aborted") {
		t.Errorf("expected Aborted in output, got %q", buf.String())
	}
}

func TestConfirmPerAppCachesApproval(t *testing.T) {
	c, _ := newTestComputer(t, ModePerApp, 1)
	action := ActionDesc{Description: "click", AppID: "firefox"}

	// First call: should run countdown (~1s)
	start := time.Now()
	if err := c.Confirm(context.Background(), action); err != nil {
		t.Fatalf("first Confirm: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("expected first call to take ~1s, took %v", elapsed)
	}

	// Second call: should skip countdown (cached)
	start = time.Now()
	if err := c.Confirm(context.Background(), action); err != nil {
		t.Fatalf("second Confirm: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("expected second call to be instant, took %v", elapsed)
	}

	if got := c.ApprovedAppCount(); got != 1 {
		t.Errorf("expected 1 approved app, got %d", got)
	}
}

func TestConfirmPerAppDifferentAppsBothPrompt(t *testing.T) {
	c, _ := newTestComputer(t, ModePerApp, 1)

	if err := c.Confirm(context.Background(), ActionDesc{Description: "click", AppID: "firefox"}); err != nil {
		t.Fatalf("firefox Confirm: %v", err)
	}
	if err := c.Confirm(context.Background(), ActionDesc{Description: "click", AppID: "chrome"}); err != nil {
		t.Fatalf("chrome Confirm: %v", err)
	}

	if got := c.ApprovedAppCount(); got != 2 {
		t.Errorf("expected 2 approved apps, got %d", got)
	}
}

func TestConfirmPerAppEmptyAppIDAlwaysPrompts(t *testing.T) {
	c, _ := newTestComputer(t, ModePerApp, 1)
	action := ActionDesc{Description: "click", AppID: ""}

	start := time.Now()
	if err := c.Confirm(context.Background(), action); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("expected countdown to run when AppID is empty, took %v", elapsed)
	}
	// No approval cached because AppID was empty
	if got := c.ApprovedAppCount(); got != 0 {
		t.Errorf("expected 0 approved apps with empty AppID, got %d", got)
	}
}

func TestResetApprovals(t *testing.T) {
	c, _ := newTestComputer(t, ModePerApp, 1)

	if err := c.Confirm(context.Background(), ActionDesc{Description: "click", AppID: "firefox"}); err != nil {
		t.Fatal(err)
	}
	if c.ApprovedAppCount() != 1 {
		t.Fatal("expected 1 approved app before reset")
	}

	c.ResetApprovals()
	if c.ApprovedAppCount() != 0 {
		t.Errorf("expected 0 approved apps after reset, got %d", c.ApprovedAppCount())
	}

	// Next call should prompt again
	start := time.Now()
	if err := c.Confirm(context.Background(), ActionDesc{Description: "click", AppID: "firefox"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("expected countdown after reset, took %v", elapsed)
	}
}

func TestModeIsValid(t *testing.T) {
	tests := []struct {
		mode Mode
		want bool
	}{
		{ModePerRequest, true},
		{ModePerApp, true},
		{ModeOff, true},
		{Mode(""), false},
		{Mode("bogus"), false},
	}
	for _, tt := range tests {
		if got := tt.mode.IsValid(); got != tt.want {
			t.Errorf("Mode(%q).IsValid() = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
