package testscript

import (
	"context"
	"testing"
	"time"
)

// runFor executes a script against the fixture with a run budget of its own.
func runFor(t *testing.T, budget time.Duration, source string) (*Result, time.Duration) {
	t.Helper()

	if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
		t.Fatalf("navigate to fixture: %v", err)
	}

	start := time.Now()
	res, err := Run(context.Background(), Options{
		Browser: testBrowser,
		Source:  source,
		Name:    t.Name() + ".js",
		BaseURL: fixtureURL,
		Timeout: budget,
	})
	if err != nil {
		t.Fatalf("Run returned an error (it should report failures in Result): %v", err)
	}
	return res, time.Since(start)
}

// A wait with a longer timeout than the run has left used to hold the run for
// the whole of it. The run's deadline interrupts the JavaScript, but a host
// call is not interruptible once it is inside Go, and the browser's waits
// ignored the context they were handed — so a script with two seconds to live
// sat in a twenty-second wait and reported its timeout eighteen seconds late.
func TestAWaitDoesNotOutliveTheRun(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"waitFor", `atr.waitFor("#never", {timeout: 20000});`},
		{"waitFor visible", `atr.waitFor("#never", {timeout: 20000, visible: true});`},
		{"expectExists", `atr.expectExists("#never", {timeout: 20000});`},
		{"expectExists, has-text", `atr.expectExists('div:has-text("never rendered")', {timeout: 20000});`},
		{"expectExists, xpath", `atr.expectExists('//div[@id="never"]', {timeout: 20000});`},
		{"expectText", `atr.expectText("#never", "anything", {timeout: 20000});`},
		{"waitForText", `atr.waitForText("text that never appears", {timeout: 20000});`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, took := runFor(t, 2*time.Second, `atr.step(1, "Wait far too long", () => {`+tt.body+`});`)

			if res.Passed {
				t.Fatal("expected a failure")
			}
			if took > 6*time.Second {
				t.Errorf("a run with a 2s budget took %v: the wait outlived it", took.Round(time.Millisecond))
			}
			// Running out of time says nothing about the application. An
			// assertion here would be the run's own impatience reported as a
			// regression.
			if res.Failure.Kind != KindTimeout {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, KindTimeout, res.Failure.Message)
			}
			if res.Failure.Step != 1 {
				t.Errorf("Step = %d, want the step that was running", res.Failure.Step)
			}
		})
	}
}

// Binding the waits to the run's deadline means a wait can now be cut short by
// it, and a wait cut short has seen nothing. The two calls that treat "not
// found" as an answer must not take it for one: exists() would hand the script
// a false it then asserts on, and expectMissing() would declare the thing gone.
// Either way the run's own impatience comes out as a verdict on the
// application — an assertion, or a pass.
//
// The sleep puts the run's deadline inside the half second the lookup waits.
func TestALookupCutShortByTheRunIsNotAnAnswer(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"exists, then an assertion on it",
			`atr.sleep(700); expect(atr.exists("#never")).toBeTruthy();`},
		{"exists, then a branch that fails",
			`atr.sleep(700); if (!atr.exists("#never")) { atr.fail("the thing is missing"); }`},
		{"exists on a plain target",
			`atr.sleep(700); if (!atr.exists("No such words")) { atr.fail("the thing is missing"); }`},
		// The heading is there and stays there. A lookup cut short would
		// report it gone, and the step — the whole run — would pass.
		{"expectMissing on something that is still there",
			`atr.sleep(700); atr.expectMissing("#heading", {timeout: 5000});`},
		{"expectMissing on something that never was",
			`atr.sleep(700); atr.expectMissing("#never", {timeout: 5000});`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Several times each: which side of the deadline the call returns
			// on is a race, and the wrong answer was only sometimes given.
			for i := 0; i < 5; i++ {
				res, _ := runFor(t, time.Second, `atr.step(1, "Run out of time inside a lookup", () => {`+tt.body+`});`)

				if res.Passed {
					t.Fatalf("run %d passed: a lookup the run cut short was taken as an answer", i+1)
				}
				if res.Failure.Kind != KindTimeout {
					t.Fatalf("run %d: kind = %q, want %q (%s)", i+1, res.Failure.Kind, KindTimeout, res.Failure.Message)
				}
			}
		})
	}
}

// A wait that ends because the run was cancelled has not timed out. A timeout
// is retried; a cancellation is somebody asking for the run to stop.
func TestACancelledRunIsNotATimeout(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"waitFor", `atr.waitFor("#never", {timeout: 20000});`},
		{"waitFor visible", `atr.waitFor("#never", {timeout: 20000, visible: true});`},
		{"waitForText", `atr.waitForText("text that never appears", {timeout: 20000});`},
		{"expectExists", `atr.expectExists("#never", {timeout: 20000});`},
		{"exists", `atr.sleep(300); atr.exists("#never"); atr.fail("carried on after a cancelled lookup");`},
		{"click", `atr.click("#never");`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := testBrowser.Navigate(context.Background(), fixtureURL); err != nil {
				t.Fatalf("navigate to fixture: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(500*time.Millisecond, cancel)
			defer cancel()

			start := time.Now()
			res, err := Run(ctx, Options{
				Browser: testBrowser,
				Source:  `atr.step(1, "Be interrupted", () => {` + tt.body + `});`,
				Name:    t.Name() + ".js",
				BaseURL: fixtureURL,
				Timeout: time.Minute,
			})
			if err != nil {
				t.Fatalf("Run returned an error: %v", err)
			}
			if res.Passed {
				t.Fatal("a cancelled run passed")
			}
			if res.Failure.Kind != KindEnvironment {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, KindEnvironment, res.Failure.Message)
			}
			if took := time.Since(start); took > 5*time.Second {
				t.Errorf("a run cancelled after 500ms took %v to stop", took.Round(time.Millisecond))
			}
		})
	}
}
