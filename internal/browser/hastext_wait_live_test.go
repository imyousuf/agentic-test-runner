package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A :has-text() target used to be looked up once, at the moment of the call.
//
// tryFind calls its function once and relies on the function to wait, which
// rod's Element and ElementX do: they retry until the context expires.
// :has-text() was resolved through Elements, which returns what matches now
// and never retries. So a button that rendered a moment late was "not found"
// within milliseconds, whatever timeout the caller had given — and
// atr.expectExists turned that into an assertion failure reading "it was not
// there after 30s", in a run whose whole attempt took 1.7s. A healthy page
// reported as a broken application, by the one kind that is never retried.

// openLateButton loads the fixture and starts its clock, returning the moment
// it did.
//
// The page is loaded with its timer unarmed and armed from here, so the delay
// runs from the same moment the test starts measuring. Left to start at page
// load, a slow navigation eats into the delay and a test that checks "it
// really did wait" fails on a busy machine for no fault of the code.
func openLateButton(t *testing.T, query string) time.Time {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_button.html?manual=1&"+query); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := testBrowser.Evaluate("window.arm()"); err != nil {
		t.Fatalf("arming the fixture: %v", err)
	}
	return start
}

// The repro from the report. The same late button, named three ways: the CSS
// and XPath forms always waited, and are here as controls.
func TestHasTextWaitsForALateElement(t *testing.T) {
	for _, target := range []string{
		`#ready`, // control: CSS waits
		`//button[normalize-space()="Ready now"]`, // control: XPath waits
		`button:has-text("Ready now")`,            // the bug
	} {
		t.Run(target, func(t *testing.T) {
			start := openLateButton(t, "ms=1500")

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			err := testBrowser.WaitForElement(ctx, target, 10*time.Second)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("WaitForElement(%q, 10s) gave up after %v: %v", target, elapsed.Round(time.Millisecond), err)
			}
			if elapsed < 1400*time.Millisecond {
				t.Errorf("returned in %v, before the button could exist — the test is not exercising the wait", elapsed)
			}
		})
	}
}

// Every action goes through the same lookup, so every action waits.
func TestActionsThroughHasTextWaitForALateElement(t *testing.T) {
	const target = `button:has-text("Ready now")`

	t.Run("Click", func(t *testing.T) {
		start := openLateButton(t, "ms=1500")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if err := testBrowser.Click(ctx, target, false); err != nil {
			t.Fatalf("Click gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
		}
		// And it clicked the late button, not the one that was already there.
		got, err := testBrowser.Evaluate(`document.getElementById("events").textContent`)
		if err != nil {
			t.Fatal(err)
		}
		if got != "click:ready;" {
			t.Errorf("events = %q, want one click on the late button", got)
		}
	})

	t.Run("Hover", func(t *testing.T) {
		start := openLateButton(t, "ms=1500")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if err := testBrowser.Hover(ctx, target); err != nil {
			t.Fatalf("Hover gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
		}
	})

	t.Run("WaitForElementVisible", func(t *testing.T) {
		start := openLateButton(t, "ms=1500")

		if err := testBrowser.WaitForElementVisible(context.Background(), target, 10*time.Second); err != nil {
			t.Fatalf("WaitForElementVisible gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
		}
	})

	// A read with no deadline of its own gets the default search budget of
	// three seconds, and the button arrives well inside it.
	t.Run("GetTextContent", func(t *testing.T) {
		start := openLateButton(t, "ms=800")

		got, err := testBrowser.GetTextContent(target, "flat")
		if err != nil {
			t.Fatalf("GetTextContent gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
		}
		if len(got.Groups) != 1 || got.Groups[0].Text != "Ready now" {
			t.Errorf("read %+v, want the late button's text", got.Groups)
		}
	})
}

// The other way an element is late: it is there from the start and its text is
// not. A lookup that polled for the element and then checked its text once
// would find the button, see "Loading…" and give up.
func TestHasTextWaitsForTextToArriveOnAnElementThatIsAlreadyThere(t *testing.T) {
	start := openLateButton(t, "ms=1500&mode=relabel")

	err := testBrowser.WaitForElement(context.Background(), `button:has-text("Ready now")`, 10*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("gave up after %v: %v", elapsed.Round(time.Millisecond), err)
	}
	if elapsed < 1400*time.Millisecond {
		t.Errorf("returned in %v, before the text could be there", elapsed)
	}
}

// Waiting must not turn into waiting for ever. A target that never matches is
// still a missing element, reported once the caller's budget is spent — not
// before, which was the bug, and not long after.
func TestHasTextThatNeverMatchesGivesUpWhenTheBudgetIsSpent(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"an existence check's half second", 500 * time.Millisecond},
		{"two seconds", 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openLateButton(t, "ms=60000")

			start := time.Now()
			err := testBrowser.WaitForElement(context.Background(), `button:has-text("Ready now")`, tt.timeout)
			elapsed := time.Since(start)

			if !errors.Is(err, ErrElementNotFound) {
				t.Fatalf("err = %v, want ErrElementNotFound", err)
			}
			// The lookup ends at its deadline; allow it the last poll's
			// round trip either side.
			if elapsed < tt.timeout-150*time.Millisecond {
				t.Errorf("gave up after %v of a %v budget — it did not wait", elapsed.Round(time.Millisecond), tt.timeout)
			}
			if elapsed > tt.timeout+2*time.Second {
				t.Errorf("took %v to spend a %v budget", elapsed.Round(time.Millisecond), tt.timeout)
			}
			// The message names the target as it was written, so a repair can
			// find it in the script.
			if !strings.Contains(err.Error(), `button:has-text("Ready now")`) {
				t.Errorf("err = %q, want it to quote the target as written", err)
			}
		})
	}
}

// An action bound to a caller's deadline stops at it.
func TestHasTextActionStopsAtTheCallersDeadline(t *testing.T) {
	openLateButton(t, "ms=60000")

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := testBrowser.Click(ctx, `button:has-text("Ready now")`, false)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrElementNotFound) {
		t.Errorf("err = %v, want ErrElementNotFound so the script runtime classifies it as drift", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("a click with 700ms to spend took %v", elapsed.Round(time.Millisecond))
	}
}

// A base selector that does not parse can never match, however long it is
// given, so it is refused at once rather than waited out.
func TestHasTextWithAMalformedBaseIsRefusedWithoutWaiting(t *testing.T) {
	openLateButton(t, "ms=60000")

	start := time.Now()
	err := testBrowser.WaitForElement(context.Background(), `button[[[bad:has-text("Ready now")`, 10*time.Second)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrInvalidSelector) {
		t.Errorf("err = %v, want ErrInvalidSelector", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v to refuse a selector that does not parse", elapsed.Round(time.Millisecond))
	}
}
