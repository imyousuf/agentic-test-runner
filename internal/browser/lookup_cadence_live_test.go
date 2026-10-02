package browser

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A lookup is retried until its budget runs out, but how often it looks was
// left to rod's default backoff: about 0.2s, 0.6s and 1.4s in, and then not
// again until 3s. So the default three-second budget was really a budget of
// 1.4 seconds — anything that rendered in its second half was never looked
// for again — and an explicit ten-second wait took its last useful look at
// nine. An element that arrived inside the budget was reported missing, which
// is drift, which is a repair.

// Late in the default budget, for every spelling of a selector.
func TestALookupKeepsLookingThroughItsWholeBudget(t *testing.T) {
	for _, target := range []string{
		`#ready`,
		`//button[@id="ready"]`,
		`button:has-text("Ready now")`,
	} {
		t.Run(target, func(t *testing.T) {
			// 2s: past the old last look at about 1.4s, well inside the
			// three seconds a read with no deadline of its own is given, and
			// between two of the new looks rather than on one.
			start := openLateButton(t, "ms=2000")

			got, err := testBrowser.GetTextContent(target, "flat")
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("gave up after %v on a button that rendered at 2s of a 3s budget: %v",
					elapsed.Round(time.Millisecond), err)
			}
			if len(got.Groups) != 1 || got.Groups[0].Text != "Ready now" {
				t.Errorf("read %+v, want the late button's text", got.Groups)
			}
			// Found reasonably soon after it appeared, not at the very end.
			if elapsed > 2900*time.Millisecond {
				t.Errorf("found after %v; the button was there at 2s", elapsed.Round(time.Millisecond))
			}
		})
	}
}

// The same through an action bound to a caller's deadline, with time left
// over to act on what it finds.
func TestAnActionKeepsLookingThroughItsWholeBudget(t *testing.T) {
	start := openLateButton(t, "ms=2000")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := testBrowser.Click(ctx, `#ready`, false); err != nil {
		t.Fatalf("gave up after %v on a button that rendered at 2s: %v",
			time.Since(start).Round(time.Millisecond), err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("clicked after %v; the button was there at 2s", elapsed.Round(time.Millisecond))
	}
}

// An explicit wait keeps looking through its last second. The old cadence
// looked once a second, on the second, so whatever arrived after the last of
// those was missed.
func TestAWaitKeepsLookingThroughItsLastSecond(t *testing.T) {
	for _, target := range []string{
		`#ready`,
		`button:has-text("Ready now")`,
	} {
		t.Run(target, func(t *testing.T) {
			// Past the old last look at about 3s, and half a second before
			// the wait ends.
			start := openLateButton(t, "ms=3400")

			err := testBrowser.WaitForElement(context.Background(), target, 4*time.Second)
			if err != nil {
				t.Fatalf("gave up after %v on a button that rendered at 3.4s of a 4s wait: %v",
					time.Since(start).Round(time.Millisecond), err)
			}
		})
	}
}

// Waiting for text had the same cadence.
func TestWaitForTextKeepsLookingThroughItsWholeBudget(t *testing.T) {
	start := openLateButton(t, "ms=2000")

	if err := testBrowser.WaitForText(context.Background(), "Ready now", 3*time.Second); err != nil {
		t.Fatalf("gave up after %v on text that appeared at 2s of a 3s wait: %v",
			time.Since(start).Round(time.Millisecond), err)
	}
}

// Looking more often must not mean looking for longer: a target that never
// appears is still given up on at the budget, and an impatient check stays
// impatient.
func TestALookupStillEndsAtItsBudget(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"an existence check's half second", 500 * time.Millisecond},
		{"three seconds", 3 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFixture(t)

			start := time.Now()
			err := testBrowser.WaitForElement(context.Background(), "#never-rendered", tt.timeout)
			elapsed := time.Since(start)

			if !errors.Is(err, ErrElementNotFound) {
				t.Errorf("err = %v, want ErrElementNotFound", err)
			}
			if elapsed < tt.timeout-150*time.Millisecond || elapsed > tt.timeout+time.Second {
				t.Errorf("a %v wait took %v", tt.timeout, elapsed.Round(time.Millisecond))
			}
		})
	}
}
