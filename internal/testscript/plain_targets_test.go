package testscript

import (
	"strings"
	"testing"
	"time"
)

// The compile prompt says a target that is plain visible text "is a valid
// target and usually the clearest one". For anything but a click it was not a
// working one. The ways of reading a plain target each waited a slice of the
// budget in turn, and inside the half second atr.exists allows only the first
// of them was ever tried — so exists("Sign in") was false with the button on
// the page, expectExists("Welcome") blamed the application for a heading that
// was there all along, and expectMissing("Sign in") passed.

func TestPlainTargetsWorkInEveryCallThatTakesOne(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		// Each of the fixture's elements by what a person would call it.
		{"exists, button text", `if (!atr.exists("Sign in")) { atr.fail("exists() missed the button"); }`},
		{"exists, heading text", `if (!atr.exists("Welcome back")) { atr.fail("exists() missed the heading"); }`},
		{"exists, text in part", `if (!atr.exists("Welcome")) { atr.fail("exists() missed part of the heading"); }`},
		{"exists, placeholder", `if (!atr.exists("Username")) { atr.fail("exists() missed the field"); }`},
		{"exists, name", `if (!atr.exists("password")) { atr.fail("exists() missed the field"); }`},
		{"exists, absent", `if (atr.exists("No such words")) { atr.fail("exists() found what is not there"); }`},

		{"expectExists, default budget", `atr.expectExists("Welcome");`},
		{"expectExists, three seconds", `atr.expectExists("Welcome", {timeout: 3000});`},
		{"expectExists, half a second", `atr.expectExists("Sign in", {timeout: 500});`},
		{"waitFor", `atr.waitFor("Welcome", {timeout: 3000});`},
		{"waitFor visible", `atr.waitFor("Sign in", {timeout: 3000, visible: true});`},
		{"expectMissing, absent", `atr.expectMissing("No such words", {timeout: 1000});`},

		{"click", `atr.click("Sign in"); atr.expectText("#status", "signed in", {timeout: 3000});`},
		{"fill by placeholder", `atr.fill("Username", "testuser");
			expect(atr.eval("document.getElementById('username').value")).toBe("testuser");`},
		{"hover", `atr.hover("Welcome back");`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			res := run(t, `atr.step(1, "Use a plain target", () => {`+tt.body+`});`)
			if !res.Passed {
				t.Fatalf("%v", res.Failure)
			}
			// Everything here is on the page already (or, for the two absent
			// ones, given up on inside a second), so nothing should take long.
			if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
				t.Errorf("took %v", elapsed.Round(time.Millisecond))
			}
		})
	}
}

// The false pass. An absence check that only ever looks for an aria-label
// "confirms" the absence of anything named any other way.
func TestExpectMissingSeesAPlainTargetThatIsStillThere(t *testing.T) {
	for _, target := range []string{"Sign in", "Welcome back", "Welcome", "Username"} {
		t.Run(target, func(t *testing.T) {
			res := run(t, `atr.step(1, "Assert something present is gone", () => {
				atr.expectMissing(`+js(target)+`, {timeout: 800});
			});`)

			if res.Passed {
				t.Fatalf("expectMissing(%q) passed with it on the page", target)
			}
			if res.Failure.Kind != KindAssertion {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, KindAssertion, res.Failure.Message)
			}
		})
	}
}

// And it still passes once the thing has really gone.
func TestExpectMissingPassesWhenAPlainTargetGoes(t *testing.T) {
	res := run(t, `atr.step(1, "The heading goes away", () => {
		`+removeLater("heading", 800)+`
		atr.expectMissing("Welcome back", {timeout: 5000});
	});`)

	if !res.Passed {
		t.Fatalf("%v", res.Failure)
	}
}

// Text that renders late is waited for and found when it arrives.
func TestPlainTextIsWaitedFor(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"expectExists", `atr.expectExists("Ready now", {timeout: 10000});`},
		{"waitFor", `atr.waitFor("Ready now", {timeout: 10000});`},
		{"click", `atr.click("Ready now"); atr.expectText("#status", "clicked ready", {timeout: 3000});`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			res := run(t, `atr.step(1, "Use a button that is still rendering", () => {
				`+buttonLater("ready", "Ready now", 1500)+`
				`+tt.body+`
			});`)
			elapsed := time.Since(start)

			if !res.Passed {
				t.Fatalf("%v", res.Failure)
			}
			if elapsed < 1400*time.Millisecond {
				t.Errorf("passed in %v, before the button could exist", elapsed.Round(time.Millisecond))
			}
			if elapsed > 4*time.Second {
				t.Errorf("took %v; the button was there at 1.5s", elapsed.Round(time.Millisecond))
			}
		})
	}
}

// A miss is still a miss, with the right kind for the call that made it.
func TestAPlainTargetThatIsNotThereFailsAsItShould(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantKind FailureKind
	}{
		{"click", `atr.click("No such words");`, KindNotFound},
		{"expectExists", `atr.expectExists("No such words", {timeout: 800});`, KindAssertion},
		{"waitFor", `atr.waitFor("No such words", {timeout: 800});`, KindTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, `atr.step(1, "Use something that is not there", () => {`+tt.body+`});`)
			if res.Passed {
				t.Fatal("expected a failure")
			}
			if res.Failure.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, tt.wantKind, res.Failure.Message)
			}
		})
	}
}

// A click on text the title shares used to resolve to the <title> and sit for
// thirty seconds waiting for it to become clickable.
func TestAPlainTargetIsNotTheTitle(t *testing.T) {
	start := time.Now()
	res := run(t, `atr.step(1, "Look for the title's text on the page", () => {
		if (atr.exists("Fixture")) { atr.fail("the title was found as page text"); }
		atr.click("Fixture");
	});`)

	if res.Passed {
		t.Fatal("clicked text that is only in the title")
	}
	if res.Failure.Kind != KindNotFound {
		t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, KindNotFound, res.Failure.Message)
	}
	if !strings.Contains(res.Failure.Message, "Fixture") {
		t.Errorf("message = %q, want it to name the target", res.Failure.Message)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("took %v to report text that is not on the page", elapsed.Round(time.Millisecond))
	}
}
