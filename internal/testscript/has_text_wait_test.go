package testscript

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A :has-text() target was looked up once and never waited for, so on a page
// still rendering atr.expectExists failed within milliseconds — as an
// assertion, the kind that blames the application and is never retried — with
// a message claiming a wait that had not happened: "it was not there after
// 30s", from an attempt that lasted 1.7s in total.
//
// Two things are pinned here. A :has-text() target is waited for like any
// other. And a failed wait says how long it waited, so a lookup that did not
// wait can never again claim that it did.

// buttonLater schedules a button to appear after ms milliseconds, with a click
// handler that records it was clicked.
func buttonLater(id, text string, ms int) string {
	return fmt.Sprintf(`atr.eval("(function(){setTimeout(function(){var b=document.createElement('button');b.id='%s';b.type='button';b.textContent='%s';b.onclick=function(){document.getElementById('status').textContent='clicked %s'};document.body.appendChild(b);},%d);return true})()");`,
		id, text, id, ms)
}

// The repro from the report, at the level a compiled script sees it.
func TestExpectExistsWaitsForALateHasTextTarget(t *testing.T) {
	start := time.Now()
	res := run(t, `
		atr.step(1, "The button is still rendering", () => {
			`+buttonLater("ready", "Ready now", 1500)+`
			atr.expectExists('button:has-text("Ready now")', {timeout: 10000});
		});
	`)

	if !res.Passed {
		t.Fatalf("a button that renders after 1.5s was reported missing: %v", res.Failure)
	}
	if elapsed := time.Since(start); elapsed < 1200*time.Millisecond {
		t.Errorf("passed in %v, before the button could exist — the test is not exercising the wait", elapsed)
	}
}

// Every call that waits or acts goes through the same lookup.
func TestScriptCallsWaitForALateHasTextTarget(t *testing.T) {
	const target = `'button:has-text("Ready now")'`

	tests := []struct {
		name  string
		delay int
		body  string
	}{
		{"waitFor", 1500, `atr.waitFor(` + target + `, {timeout: 10000});`},
		{"waitFor visible", 1500, `atr.waitFor(` + target + `, {timeout: 10000, visible: true});`},
		{"click", 1500, `atr.click(` + target + `);
			atr.expectText("#status", "clicked ready", {timeout: 3000});`},
		{"doubleClick", 1500, `atr.doubleClick(` + target + `);
			atr.expectText("#status", "clicked ready", {timeout: 3000});`},
		{"hover", 1500, `atr.hover(` + target + `);`},
		{"expectText", 1500, `atr.expectText(` + target + `, "Ready now", {timeout: 10000});`},
		// A read takes no timeout of its own, so it gets the lookup's default
		// three seconds — and rod polls with a backoff whose last look inside
		// three seconds is at about 1.4s, for any selector, CSS included. 800ms
		// falls between that look and the one before it with room either side;
		// the cadence is not what is under test here.
		{"text", 800, `expect(atr.text(` + target + `)).toBe("Ready now");`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, `
				atr.step(1, "Act on a button that is still rendering", () => {
					`+buttonLater("ready", "Ready now", tt.delay)+`
					`+tt.body+`
				});
			`)
			if !res.Passed {
				t.Fatalf("atr.%s did not wait for the button: %v", tt.name, res.Failure)
			}
		})
	}
}

// A branch check keeps its short budget: exists() answers "not yet" quickly
// rather than waiting the element out, and absence is still an answer.
func TestExistsOnALateHasTextTargetStaysABranchCheck(t *testing.T) {
	res := run(t, `
		atr.step(1, "Not there yet, and exists() does not hang about", () => {
			`+buttonLater("ready", "Ready now", 5000)+`
			var before = Date.now();
			if (atr.exists('button:has-text("Ready now")')) { atr.fail("exists() found a button that has not rendered"); }
			var took = Date.now() - before;
			if (took > 2000) { atr.fail("a branch check took " + took + "ms"); }
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

// Absence through :has-text(): something on its way out gets its budget.
func TestExpectMissingWaitsForAHasTextTargetToLeave(t *testing.T) {
	res := run(t, `
		atr.step(1, "The heading goes away", () => {
			`+removeLater("heading", 1000)+`
			atr.expectMissing('h1:has-text("Welcome back")', {timeout: 5000});
		});
	`)

	if !res.Passed {
		t.Fatalf("expected pass, got %v", res.Failure)
	}
}

var waitedInMessage = regexp.MustCompile(`after (\S+?)\)?$`)

// reportedWait reads the duration a failure message says it waited.
func reportedWait(t *testing.T, message string) time.Duration {
	t.Helper()
	m := waitedInMessage.FindStringSubmatch(message)
	if m == nil {
		t.Fatalf("message %q does not say how long it waited", message)
	}
	d, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatalf("message %q: %q is not a duration: %v", message, m[1], err)
	}
	return d
}

// A failed wait reports the time it spent. The requested timeout and the
// elapsed time are the same number only when the lookup really waited, which
// is the thing a reader of the message needs to be able to tell.
func TestAFailedWaitReportsHowLongItActuallyWaited(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		timeout  time.Duration
		wantKind FailureKind
		wantText string
	}{
		{
			name:     "expectExists, has-text",
			body:     `atr.expectExists('button:has-text("Never rendered")', {timeout: 1200});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindAssertion,
			wantText: "it was not there after",
		},
		{
			name:     "expectExists, css",
			body:     `atr.expectExists("#never-rendered", {timeout: 1200});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindAssertion,
			wantText: "it was not there after",
		},
		{
			name:     "expectExists, xpath",
			body:     `atr.expectExists('//button[@id="never-rendered"]', {timeout: 1200});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindAssertion,
			wantText: "it was not there after",
		},
		{
			name:     "waitFor, has-text",
			body:     `atr.waitFor('button:has-text("Never rendered")', {timeout: 1200});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindTimeout,
			wantText: "gave up after",
		},
		{
			name:     "waitFor visible, has-text",
			body:     `atr.waitFor('button:has-text("Never rendered")', {timeout: 1200, visible: true});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindTimeout,
			wantText: "gave up after",
		},
		{
			name:     "expectMissing, has-text",
			body:     `atr.expectMissing('h1:has-text("Welcome back")', {timeout: 1200});`,
			timeout:  1200 * time.Millisecond,
			wantKind: KindAssertion,
			wantText: "it was still there after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			res := run(t, `atr.step(1, "Wait for something that does not happen", () => {`+tt.body+`});`)
			ran := time.Since(start)

			if res.Passed {
				t.Fatal("expected the step to fail")
			}
			if res.Failure.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, tt.wantKind, res.Failure.Message)
			}
			if !strings.Contains(res.Failure.Message, tt.wantText) {
				t.Fatalf("message = %q, want it to contain %q", res.Failure.Message, tt.wantText)
			}

			waited := reportedWait(t, res.Failure.Message)

			// It cannot have waited longer than the whole run took. This is
			// the check the old message failed: it said 30s of a 1.7s run.
			if waited > ran {
				t.Errorf("the message says it waited %v, but the whole run took %v", waited, ran.Round(time.Millisecond))
			}
			// And it did wait: the budget was spent, give or take a poll.
			if waited < tt.timeout-200*time.Millisecond {
				t.Errorf("waited %v of a %v budget — the lookup came back early", waited, tt.timeout)
			}
			if waited > tt.timeout+2*time.Second {
				t.Errorf("waited %v on a %v budget", waited, tt.timeout)
			}
		})
	}
}

// A wait on a selector that cannot parse is not a wait that timed out. It can
// never match, so retrying it — which is what a timeout asks for — only
// spends the retries before anything looks at the script.
func TestWaitForOnAnUnparseableSelectorIsAScriptFault(t *testing.T) {
	for _, target := range []string{`#a[[[bad`, `//h1[@id=`, `h1[[[bad:has-text("Welcome")`} {
		t.Run(target, func(t *testing.T) {
			for _, opts := range []string{`{timeout: 5000}`, `{timeout: 5000, visible: true}`} {
				start := time.Now()
				res := run(t, `atr.step(1, "Wait on a broken selector", () => {
					atr.waitFor(`+js(target)+`, `+opts+`);
				});`)
				elapsed := time.Since(start)

				if res.Passed {
					t.Fatalf("%s: expected the step to fail", opts)
				}
				if res.Failure.Kind != KindScript {
					t.Errorf("%s: kind = %q, want %q (%s)", opts, res.Failure.Kind, KindScript, res.Failure.Message)
				}
				if res.Failure.Kind.Retryable() {
					t.Errorf("%s: a selector that cannot parse is being retried; it can never match", opts)
				}
				// Nothing to wait for, so nothing waited.
				if elapsed > 2*time.Second {
					t.Errorf("%s: took %v to refuse a selector that does not parse", opts, elapsed.Round(time.Millisecond))
				}
			}
		})
	}
}
