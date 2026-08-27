package testscript

import (
	"strings"
	"testing"
)

// A step used to give up on a selector after three seconds however long the
// script itself had to run, which is what made TestCompiledScriptRunsWithHudEnabled
// fail on a loaded Windows runner: the page had not settled, the fill blew a
// fixed budget, and a slow page was reported as a missing element.
//
// The element here reappears after 4.5s — past the old cap, well inside this
// script's 30s.
func TestFillWaitsForALateElement(t *testing.T) {
	res := run(t, `
		atr.step(1, "Take the field away and put it back late", () => {
			atr.eval(`+"`"+`(() => {
				const el = document.getElementById('username');
				const form = el.parentElement;
				el.remove();
				setTimeout(() => form.appendChild(el), 4500);
			})()`+"`"+`);
			atr.fill("#username", "testuser");
			atr.click("#submit");
		});
		atr.step(2, "Verify the status", () => {
			atr.waitForText("signed in");
			expect(atr.text("#status")).toBe("signed in");
		});
	`)

	if !res.Passed {
		t.Fatalf("a field that appears after 4.5s must still be filled, got: %v", res.Failure)
	}
}

// The counterpart: a selector that genuinely never matches still fails, and
// still fails as a repairable not_found rather than as something the runtime
// would retry forever.
func TestFillStillFailsOnASelectorThatNeverMatches(t *testing.T) {
	res := run(t, `
		atr.step(1, "Fill a field that does not exist", () => {
			atr.fill("#nothing-by-this-name", "testuser");
		});
	`)

	if res.Passed {
		t.Fatal("expected the step to fail")
	}
	if res.Failure == nil || res.Failure.Kind != KindNotFound {
		t.Fatalf("failure = %+v, want kind %q so the repair path can act on it", res.Failure, KindNotFound)
	}
	if !strings.Contains(res.Failure.Message, "#nothing-by-this-name") {
		t.Errorf("failure message does not name the selector: %s", res.Failure.Message)
	}
}
