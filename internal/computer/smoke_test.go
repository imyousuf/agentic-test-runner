//go:build smoke

package computer

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSmokeScreen exercises screen capture on a real display.
// Run with: go test -tags smoke -run TestSmoke -v ./internal/computer/...
func TestSmokeScreen(t *testing.T) {
	c, err := New(Config{
		CountdownMode:    ModeOff,
		CountdownSeconds: 0,
		Output:           os.Stderr,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	w, h := c.ScreenSize()
	t.Logf("Screen size: %d x %d", w, h)
	if w <= 0 || h <= 0 {
		t.Errorf("expected positive screen size, got %dx%d", w, h)
	}

	displays := c.Displays()
	t.Logf("Displays: %d", len(displays))
	if len(displays) == 0 {
		t.Error("expected at least one display")
	}
	for _, d := range displays {
		t.Logf("  [%d] bounds=%v primary=%v", d.Index, d.Bounds, d.Primary)
	}

	png, err := c.Screenshot(0)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(png) < 100 {
		t.Errorf("screenshot too small (%d bytes), likely not a real PNG", len(png))
	}
	if err := os.WriteFile("/tmp/atr_smoke_screenshot.png", png, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Logf("Wrote %d bytes to /tmp/atr_smoke_screenshot.png", len(png))
}

// TestSmokeMouse exercises mouse move + position on a real display.
func TestSmokeMouse(t *testing.T) {
	c, err := New(Config{
		CountdownMode:    ModeOff,
		CountdownSeconds: 0,
		Output:           os.Stderr,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	startX, startY := c.Position()
	t.Logf("Initial mouse: (%d, %d)", startX, startY)

	if err := c.MoveTo(context.Background(), 200, 200, false); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	x, y := c.Position()
	t.Logf("After move: (%d, %d)", x, y)
	if abs(x-200) > 5 || abs(y-200) > 5 {
		t.Errorf("expected ~(200, 200), got (%d, %d)", x, y)
	}

	// Restore original position
	_ = c.MoveTo(context.Background(), startX, startY, false)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
