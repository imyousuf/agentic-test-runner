package computer

import (
	"context"
	"log"
	"os"
	"testing"
	"time"
)

func TestGUIDisabledIsNoOp(t *testing.T) {
	g := newGUI(false, log.New(os.Stderr, "", 0))
	start := time.Now()
	if err := g.ShowCountdown(context.Background(), ActionDesc{Description: "click"}, 5); err != nil {
		t.Fatalf("ShowCountdown returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("expected immediate return when disabled, took %v", elapsed)
	}
	if got := g.BackendName(); got != "none" {
		t.Errorf("expected backend=none when disabled, got %s", got)
	}
}

func TestGUIEnabledProbesBackend(t *testing.T) {
	// Even when enabled, if no usable backend is on PATH, ShowCountdown must
	// not return an error or block — terminal countdown stays authoritative.
	// We can't easily mock exec.LookPath, so just verify probe runs and
	// returns a known label.
	g := newGUI(true, log.New(os.Stderr, "", 0))
	name := g.BackendName()
	switch name {
	case "zenity", "notify-send", "osascript", "powershell-toast", "none":
		// any of these is acceptable
	default:
		t.Errorf("unexpected backend name: %s", name)
	}
}

func TestGUIDisabledShowCountdownNeverBlocks(t *testing.T) {
	g := newGUI(false, nil)
	done := make(chan struct{})
	go func() {
		_ = g.ShowCountdown(context.Background(), ActionDesc{Description: "x"}, 10)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ShowCountdown(disabled, 10s) blocked")
	}
}
