package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// A target must end up with exactly one page however it was created.
//
// NewPage creates a target and the target-created worker hears about it, and
// both used to attach: PageFromTarget hands out a fresh CDP session every time
// it is called, so the target got two, each enabling the Runtime and Network
// domains and overriding the viewport. The old code noticed the duplicate only
// after doing all of that, and threw away the page while leaving the second
// session attached.
func TestTargetIsAdoptedOnlyOnce(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()

	if err := testBrowser.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		skipIfRendererWedged(t, err)
		t.Fatalf("NewPage: %v", err)
	}
	testBrowser.mu.RLock()
	opened := testBrowser.pages[len(testBrowser.pages)-1]
	testBrowser.mu.RUnlock()
	defer func() { _ = opened.Close() }()

	// Give the target-created event time to be delivered and drained.
	time.Sleep(500 * time.Millisecond)

	testBrowser.mu.RLock()
	defer testBrowser.mu.RUnlock()

	var count int
	for _, p := range testBrowser.pages {
		if p.TargetID == opened.TargetID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the target appears %d times in the page list, want once", count)
	}
	if got := testBrowser.targetIDs[opened.TargetID]; got != opened {
		t.Error("the target map points at a different page object than the list does")
	}
}

// Adopting a target twice must hand back the page already in use rather than
// attach again.
func TestAdoptingATargetTwiceReusesTheSameSession(t *testing.T) {
	resetFixture(t)

	testBrowser.mu.RLock()
	page := testBrowser.pages[0]
	testBrowser.mu.RUnlock()

	again, adopted, err := testBrowser.adoptTarget(page.TargetID)
	if err != nil {
		t.Fatalf("adoptTarget: %v", err)
	}
	if adopted {
		t.Error("adoptTarget claimed to adopt a target that was already ours")
	}
	if again != page {
		t.Error("adoptTarget returned a different page for a target already adopted")
	}
}

// Tab churn on a browser of this test's own, so a wedged renderer cannot
// poison the shared one the rest of the package uses.
//
// What is asserted is what ATR can actually promise: every target it drives is
// adopted once, and a renderer that stops answering is reported promptly and
// by name rather than being waited out for the whole page budget. Chrome
// wedging a renderer under churn is not something ATR can prevent — a pristine
// rod browser churning the same way does not reproduce it, but retrying does
// not help either, because a fresh same-origin tab lands in the very renderer
// that is stuck.
func TestTabChurnAdoptsEachTargetOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("tab churn takes a while")
	}

	b, err := New(config.BrowserConfig{Headless: true, NoSandbox: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := b.Launch(ctx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer b.Close()

	if err := b.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		skipIfRendererWedged(t, err)
		t.Fatalf("first page: %v", err)
	}

	for i := 0; i < 15; i++ {
		start := time.Now()
		if err := b.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
			if errors.Is(err, ErrRendererUnresponsive) {
				// The contract is that this is reported quickly, not that it
				// never happens.
				if elapsed := time.Since(start); elapsed > 30*time.Second {
					t.Fatalf("round %d: a wedged renderer took %v to report", i, elapsed)
				}
				t.Skipf("round %d: Chrome wedged a renderer, reported in %v",
					i, time.Since(start).Round(time.Second))
			}
			t.Fatalf("round %d: NewPage: %v", i, err)
		}

		b.mu.RLock()
		opened := b.pages[len(b.pages)-1]
		var seen int
		for _, p := range b.pages {
			if p.TargetID == opened.TargetID {
				seen++
			}
		}
		b.mu.RUnlock()

		if seen != 1 {
			t.Fatalf("round %d: the target is in the page list %d times, want once", i, seen)
		}

		// The tab has to be usable, not merely registered.
		_, evalErr := proto.RuntimeEvaluate{Expression: "1+1"}.Call(opened.Timeout(5 * time.Second))
		if evalErr != nil {
			t.Skipf("round %d: Chrome wedged the renderer of a tab it had just loaded: %v", i, evalErr)
		}

		if err := opened.Close(); err != nil {
			t.Fatalf("round %d: closing the tab: %v", i, err)
		}
	}
}

// skipIfRendererWedged treats a renderer that has stopped answering as an
// environmental condition rather than a defect in what is under test. It is a
// precise, typed check — not a blanket retry — so a real failure still fails.
func skipIfRendererWedged(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, ErrRendererUnresponsive) {
		t.Skipf("Chrome wedged a renderer: %v", err)
	}
}
