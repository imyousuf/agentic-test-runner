package testscript

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// setTextLater changes an element's text after ms milliseconds, which is what
// a page doing real work looks like from the outside.
func setTextLater(id, text string, ms int) string {
	return fmt.Sprintf(
		`atr.eval("(function(){setTimeout(function(){document.getElementById('%s').textContent='%s';},%d);return true})()");`,
		id, text, ms)
}

// The bug this replaces: a compiled script waits for a state and then asserts
// it, and when the application stops reaching that state the *wait* fails
// first — so a regression is reported as a timeout, retried, and in CI read as
// infrastructure. One call cannot be misattributed that way.
func TestExpectTextWaitsForTheStateItAsserts(t *testing.T) {
	res := runWithLibrary(t, "", `
		atr.step(1, "The status catches up", () => {
			`+setTextLater("status", "signed in", 1200)+`
			atr.expectText("#status", "signed in", {timeout: 5000});
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

// The whole point of the call: a state the application never reaches is the
// application being wrong, not the environment being slow.
func TestExpectTextFailsAsAnAssertionNotATimeout(t *testing.T) {
	res := run(t, `
		atr.step(1, "The status never changes", () => {
			atr.expectText("#status", "signed in", {timeout: 800});
		});
	`)

	if res.Passed {
		t.Fatal("a state the page never reached passed")
	}
	if res.Failure.Kind != KindAssertion {
		t.Errorf("kind = %q, want %q — a regression reported as a timeout is retried, "+
			"and in CI read as infrastructure", res.Failure.Kind, KindAssertion)
	}
	// The message has to name both sides, or a red build says nothing.
	for _, want := range []string{"signed in", "idle", "#status"} {
		if !strings.Contains(res.Failure.Message, want) {
			t.Errorf("the message does not mention %q: %s", want, res.Failure.Message)
		}
	}
}

// An element that never appears is the same verdict, with a message that says
// which of the two things went wrong.
func TestExpectTextOnAMissingTargetIsAnAssertion(t *testing.T) {
	res := run(t, `
		atr.step(1, "Look for something that is not there", () => {
			atr.expectText("#no-such-element", "anything", {timeout: 800});
		});
	`)

	if res.Passed {
		t.Fatal("a missing target passed")
	}
	if res.Failure.Kind != KindAssertion {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindAssertion)
	}
	if !strings.Contains(res.Failure.Message, "not on the page") {
		t.Errorf("the message does not say the element was never there: %s", res.Failure.Message)
	}
}

// The compiler emits both .toBe and .toContain, so both have to survive the
// move into one call.
func TestExpectTextContains(t *testing.T) {
	res := run(t, `
		atr.step(1, "Substring", () => {
			atr.expectText("#heading", "Welcome", {contains: true});
		});
		atr.step(2, "Exact match rejects the same substring", () => {
			try {
				atr.expectText("#heading", "Welcome", {timeout: 300});
				atr.fail("an exact match accepted a substring");
			} catch (e) {
				if (e.name !== "AssertionError") { throw e; }
			}
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

// A fault is not an answer, and waiting one out wastes the whole budget before
// reporting the wrong thing.
func TestExpectTextDoesNotWaitOutAFault(t *testing.T) {
	res := run(t, `
		atr.step(1, "A selector the browser refuses", () => {
			atr.expectText("#a:has-text(", "x", {timeout: 30000});
		});
	`)

	if res.Passed {
		t.Fatal("an unparseable selector passed")
	}
	if res.Failure.Kind != KindScript {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindScript)
	}
	if res.Duration > 10e9 {
		t.Errorf("waited %s on a selector that can never match", res.Duration)
	}
}

// An empty expectation is satisfied by anything at all — trivially so under
// contains, where strings.Contains(x, "") is always true. Shipping a primitive
// whose degenerate form cannot fail would be the false pass this whole design
// exists to keep out, arriving through the call added to prevent it.
func TestExpectTextRefusesAnEmptyExpectation(t *testing.T) {
	for _, opts := range []string{"{}", "{contains: true}"} {
		res := run(t, `atr.step(1, "Check", () => { atr.expectText("#status", "", `+opts+`); });`)

		if res.Passed {
			t.Errorf("expectText with %s and an empty expectation passed", opts)
			continue
		}
		if res.Failure.Kind != KindScript {
			t.Errorf("kind = %q, want %q — the script is wrong, not the application",
				res.Failure.Kind, KindScript)
		}
		if !strings.Contains(res.Failure.Message, "expectExists") {
			t.Errorf("the message does not point at the call that was meant: %s", res.Failure.Message)
		}
	}
}

// A lookup for an element that is not on the page carries its own floor, so a
// 400ms budget can spend three seconds. A failure that misreports its own
// patience is a failure nobody can reason about — "after 400ms" when it waited
// 3s sends the reader looking for a fast-failing page that does not exist.
func TestExpectTextReportsHowLongItActuallyWaited(t *testing.T) {
	res := run(t, `
		atr.step(1, "Look for something that is not there", () => {
			atr.expectText("#no-such-element", "anything", {timeout: 400});
		});
	`)

	if res.Passed {
		t.Fatal("a missing target passed")
	}
	if strings.Contains(res.Failure.Message, "after 400ms") {
		t.Errorf("the message repeats the budget it was given rather than the time it spent: %s",
			res.Failure.Message)
	}
	if !strings.Contains(res.Failure.Message, "after ") {
		t.Errorf("the message does not say how long it waited: %s", res.Failure.Message)
	}
}

// Running out of time is not the application being wrong. A lookup whose
// deadline expires is mapped to not-found by the browser layer, so without a
// guard an interrupted or over-budget run is reported as an assertion:
// terminal, never retried, exit 1 — infrastructure laundered into a
// regression, which is the direction that teaches people to ignore red.
func TestACancelledRunIsNotReportedAsAnAssertion(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	res, err := Run(ctx, Options{
		Browser: testBrowser,
		Source:  `atr.step(1, "Wait", () => { atr.expectText("#nothing-here", "x", {timeout: 20000}); });`,
		Name:    t.Name() + ".js",
		BaseURL: fixtureURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Passed {
		t.Fatal("a cancelled run passed")
	}
	if res.Failure.Kind == KindAssertion {
		t.Errorf("a cancelled run was reported as the application being wrong: %v", res.Failure)
	}
	if res.Failure.Kind != KindEnvironment {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindEnvironment)
	}
}

// Reading the text of an element that is not there carries the element
// lookup's own floor, so going straight to the read made a 100ms budget spend
// three seconds — and then report that it had waited 100ms.
func TestExpectTextStaysNearItsBudget(t *testing.T) {
	start := time.Now()
	res := run(t, `atr.step(1, "Wait", () => { atr.expectText("#nothing-here", "x", {timeout: 300}); });`)
	elapsed := time.Since(start)

	if res.Passed {
		t.Fatal("a missing target passed")
	}
	// The floor a lookup needs to answer at all, plus room for a slow CI box —
	// but nowhere near the three seconds it used to take.
	if elapsed > 2*time.Second {
		t.Errorf("a 300ms budget spent %s", elapsed.Round(time.Millisecond))
	}
}

// When a call's budget reaches past the run's own, the two deadlines expire
// within microseconds of each other and which is observed first is a coin
// flip — so the same broken page was reported as a timeout on one run and as
// the application being wrong on the next.
//
// Erring towards a timeout is the recoverable direction: a timeout is retried
// and then triaged, while an assertion is terminal and exits 1 on what may
// have been a slow box.
func TestACallThatOutlivesTheRunIsNeverAnAssertion(t *testing.T) {
	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatal(err)
	}

	// Both budgets land in the same window, deliberately.
	for i := range 5 {
		res, err := Run(context.Background(), Options{
			Browser: testBrowser,
			Source:  `atr.step(1, "Wait", () => { atr.expectText("#nothing-here", "x", {timeout: 600}); });`,
			Name:    t.Name() + ".js",
			BaseURL: fixtureURL,
			Timeout: 600 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.Passed {
			t.Fatal("a missing target passed")
		}
		if res.Failure.Kind == KindAssertion {
			t.Fatalf("run %d reported a run that ran out of time as the application being wrong: %v",
				i, res.Failure)
		}
	}
}
