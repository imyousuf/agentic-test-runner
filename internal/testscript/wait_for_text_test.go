package testscript

import (
	"strings"
	"testing"
)

// The fixture's own script contains the words 'signed in': it is what sets
// the status when the button is clicked. A wait for that text used to match
// the script, and returned before anything had been clicked.
func TestWaitForTextWaitsForThePageToSayIt(t *testing.T) {
	t.Run("not yet said", func(t *testing.T) {
		res := run(t, `atr.step(1, "Wait for a status nothing has caused", () => {
			atr.waitForText("signed in", {timeout: 1000});
		});`)

		if res.Passed {
			t.Fatal(`waitForText("signed in") returned with the status still idle`)
		}
		if res.Failure.Kind != KindTimeout {
			t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, KindTimeout, res.Failure.Message)
		}
		// Said once, with how long it waited.
		if strings.Count(res.Failure.Message, "waiting for text") != 1 || !strings.Contains(res.Failure.Message, "gave up after") {
			t.Errorf("message = %q", res.Failure.Message)
		}
	})

	t.Run("said after a click", func(t *testing.T) {
		res := run(t, `atr.step(1, "Click, then wait for the status", () => {
			atr.click("#submit");
			atr.waitForText("signed in", {timeout: 3000});
			expect(atr.text("#status")).toBe("signed in");
		});`)

		if !res.Passed {
			t.Fatalf("%v", res.Failure)
		}
	})

	t.Run("the title is not the page", func(t *testing.T) {
		res := run(t, `atr.step(1, "Wait for the title's text", () => {
			atr.waitForText("Fixture", {timeout: 800});
		});`)
		if res.Passed {
			t.Fatal(`waitForText matched the <title>`)
		}
	})
}
