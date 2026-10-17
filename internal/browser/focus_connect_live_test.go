package browser

import (
	"context"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// A process connecting to a headless browser — `atr run --behavior` reusing a
// daemon started with --headless — must not take it for one on a desktop. It
// did: Chrome's current headless mode reports a plain "Chrome" product, so
// pages were opened as background windows in a browser with no windows, and on
// a desktop session every click paid for a focus watch with nothing to watch.
func TestAConnectedHeadlessBrowserIsNotTakenForAWindowedOne(t *testing.T) {
	// The package's browser is headless.
	c, err := New(config.BrowserConfig{Headless: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(context.Background(), testBrowser.CDPEndpoint()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	if c.windowed {
		t.Error("connected to a headless browser and took it for one with windows")
	}
	if c.focus != nil {
		t.Error("a focus keeper was set up for a headless browser")
	}
}
