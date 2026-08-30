package testscript

import (
	"fmt"
	"strings"
	"testing"
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
