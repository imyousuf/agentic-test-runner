package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// WaitForElement and WaitForElementVisible took a context and dropped it. The
// timeout they were given was the only thing that ended them, so a wait asked
// for a minute inside a run with two seconds left held the run for the full
// minute: a script's time budget, a cancelled request and Ctrl-C all had to
// sit it out. The actions on the same page have honoured the caller's deadline
// since bindDeadline; the waits are held to it here.

func TestWaitsStopAtTheCallersDeadline(t *testing.T) {
	waits := []struct {
		name string
		wait func(ctx context.Context, target string, timeout time.Duration) error
	}{
		{"WaitForElement", func(ctx context.Context, target string, timeout time.Duration) error {
			return testBrowser.WaitForElement(ctx, target, timeout)
		}},
		{"WaitForElementVisible", func(ctx context.Context, target string, timeout time.Duration) error {
			return testBrowser.WaitForElementVisible(ctx, target, timeout)
		}},
	}
	targets := []struct{ name, target string }{
		{"css", `#never-rendered`},
		{"xpath", `//div[@id="never-rendered"]`},
		{"has-text", `div:has-text("never rendered anywhere")`},
		{"plain text", `never rendered anywhere`},
	}

	for _, w := range waits {
		for _, tg := range targets {
			t.Run(w.name+"/"+tg.name, func(t *testing.T) {
				resetFixture(t)

				ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
				defer cancel()

				start := time.Now()
				err := w.wait(ctx, tg.target, 20*time.Second)
				elapsed := time.Since(start)

				if err == nil {
					t.Fatal("waited successfully for an element that is not there")
				}
				if elapsed > 3*time.Second {
					t.Errorf("a wait inside a 700ms deadline took %v; its own 20s timeout outlived the caller", elapsed.Round(time.Millisecond))
				}
			})
		}
	}
}

// Cancelling the caller ends the wait too: Ctrl-C on a run, or a parent
// deadline further up, arrives as cancellation of the context the wait holds.
func TestWaitsStopWhenTheCallerIsCancelled(t *testing.T) {
	waits := []struct {
		name string
		wait func(ctx context.Context) error
	}{
		{"WaitForElement", func(ctx context.Context) error {
			return testBrowser.WaitForElement(ctx, "#never-rendered", 20*time.Second)
		}},
		{"WaitForElementVisible", func(ctx context.Context) error {
			return testBrowser.WaitForElementVisible(ctx, "#never-rendered", 20*time.Second)
		}},
		{"WaitForText", func(ctx context.Context) error {
			return testBrowser.WaitForText(ctx, "never rendered anywhere", 20*time.Second)
		}},
	}

	for _, w := range waits {
		t.Run(w.name, func(t *testing.T) {
			resetFixture(t)

			// A deadline far away, as a script run has, cancelled early.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			time.AfterFunc(500*time.Millisecond, cancel)
			defer cancel()

			start := time.Now()
			err := w.wait(ctx)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("waited successfully for an element that is not there")
			}
			if elapsed > 3*time.Second {
				t.Errorf("a cancelled wait took %v to return", elapsed.Round(time.Millisecond))
			}
			// Cancellation is not "the element is missing": that reading is
			// what sends a script for repair.
			if errors.Is(err, ErrElementNotFound) {
				t.Errorf("err = %v — a cancelled wait was reported as a missing element", err)
			}
		})
	}
}

// Waiting for text is a wait like the others, and took no context at all.
func TestWaitForTextStopsAtTheCallersDeadline(t *testing.T) {
	resetFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := testBrowser.WaitForText(ctx, "text that never appears on this page", 20*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waited successfully for text that is not there")
	}
	if elapsed > 3*time.Second {
		t.Errorf("a wait inside a 700ms deadline took %v", elapsed.Round(time.Millisecond))
	}

	// With time to spare it still finds what is on the page.
	if err := testBrowser.WaitForText(context.Background(), "Test Page Heading", 3*time.Second); err != nil {
		t.Errorf("waiting for text that is there: %v", err)
	}
}

// The timeout still governs when it is the shorter of the two, and a caller
// with no deadline at all is unaffected.
func TestWaitTimeoutStillAppliesInsideALongerDeadline(t *testing.T) {
	for _, ctx := range []struct {
		name string
		make func() (context.Context, context.CancelFunc)
	}{
		{"no deadline", func() (context.Context, context.CancelFunc) { return context.Background(), func() {} }},
		{"a longer deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Minute)
		}},
	} {
		t.Run(ctx.name, func(t *testing.T) {
			resetFixture(t)
			c, cancel := ctx.make()
			defer cancel()

			start := time.Now()
			err := testBrowser.WaitForElement(c, "#never-rendered", 800*time.Millisecond)
			elapsed := time.Since(start)

			if !errors.Is(err, ErrElementNotFound) {
				t.Errorf("err = %v, want ErrElementNotFound", err)
			}
			if elapsed < 600*time.Millisecond || elapsed > 3*time.Second {
				t.Errorf("an 800ms wait took %v", elapsed.Round(time.Millisecond))
			}

			// And it still finds what is there.
			if err := testBrowser.WaitForElement(c, "#test-button", 2*time.Second); err != nil {
				t.Errorf("waiting for an element that exists: %v", err)
			}
			if err := testBrowser.WaitForElementVisible(c, "#test-button", 2*time.Second); err != nil {
				t.Errorf("waiting for a visible element that exists: %v", err)
			}
		})
	}
}
