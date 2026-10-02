package testscript

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The compile prompt tells the model that targets are "standard CSS or XPath,
// plus one extension: :has-text(...)". A script is entitled to rely on that
// for every call that takes a target, not only the ones that click.
//
// It could not. atr.text and atr.expectText resolved their target through a
// reader that had no XPath branch, so a script that clicked through an XPath
// and then read through the same one failed its first replay as a script
// fault — "invalid selector" — and was sent for repair. The downstream suite
// that found this had six such failures in a week, and ended up telling the
// compiler "read text through CSS only" in every spec, against the prompt.

// targetForm is one spelling of each element in the fixture.
type targetForm struct {
	name    string
	heading string
	button  string
	input   string
	status  string
	missing string
}

var targetForms = []targetForm{
	{
		name:    "css",
		heading: `#heading`,
		button:  `#submit`,
		input:   `#username`,
		status:  `#status`,
		missing: `#no-such-node`,
	},
	{
		name:    "xpath",
		heading: `//h1[@id="heading"]`,
		button:  `//button[normalize-space()="Sign in"]`,
		input:   `//input[@name="username"]`,
		status:  `//div[@id="status"]`,
		missing: `//div[@id="no-such-node"]`,
	},
	{
		name:    "has-text",
		heading: `h1:has-text("Welcome back")`,
		button:  `button:has-text("Sign in")`,
		input:   `input:has-text("Username")`,
		status:  `div:has-text("idle")`,
		missing: `div:has-text("no such text anywhere")`,
	},
}

// js quotes a Go string as a JavaScript string literal.
func js(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// targetAPIs is every script call that takes a target, each as a step body
// that fails unless the call reached the element the target names.
var targetAPIs = []struct {
	name string
	body func(f targetForm) string
}{
	{"text", func(f targetForm) string {
		return fmt.Sprintf(`expect(atr.text(%s)).toBe("Welcome back");`, js(f.heading))
	}},
	{"expectText", func(f targetForm) string {
		return fmt.Sprintf(`atr.expectText(%s, "Welcome back", {timeout: 3000});`, js(f.heading))
	}},
	{"expectText contains", func(f targetForm) string {
		return fmt.Sprintf(`atr.expectText(%s, "come ba", {contains: true, timeout: 3000});`, js(f.heading))
	}},
	{"expectExists", func(f targetForm) string {
		return fmt.Sprintf(`atr.expectExists(%s, {timeout: 3000});`, js(f.heading))
	}},
	{"exists", func(f targetForm) string {
		return fmt.Sprintf(`if (!atr.exists(%s)) { atr.fail("exists() said the heading was absent"); }`, js(f.heading))
	}},
	{"exists on an absent target", func(f targetForm) string {
		return fmt.Sprintf(`if (atr.exists(%s)) { atr.fail("exists() found something that is not there"); }`, js(f.missing))
	}},
	{"expectMissing", func(f targetForm) string {
		return fmt.Sprintf(`atr.expectMissing(%s, {timeout: 1500});`, js(f.missing))
	}},
	{"waitFor", func(f targetForm) string {
		return fmt.Sprintf(`atr.waitFor(%s, {timeout: 3000});`, js(f.heading))
	}},
	{"waitFor visible", func(f targetForm) string {
		return fmt.Sprintf(`atr.waitFor(%s, {timeout: 3000, visible: true});`, js(f.heading))
	}},
	{"scroll", func(f targetForm) string {
		return fmt.Sprintf(`atr.scroll({selector: %s, y: 0});`, js(f.heading))
	}},
	{"hover", func(f targetForm) string {
		return fmt.Sprintf(`atr.hover(%s);`, js(f.button))
	}},
	{"fill", func(f targetForm) string {
		return fmt.Sprintf(`atr.fill(%s, "testuser");
			expect(atr.eval("document.getElementById('username').value")).toBe("testuser");`, js(f.input))
	}},
	{"click", func(f targetForm) string {
		return fmt.Sprintf(`atr.click(%s);
			atr.expectText("#status", "signed in", {timeout: 3000});`, js(f.button))
	}},
	{"doubleClick", func(f targetForm) string {
		return fmt.Sprintf(`atr.doubleClick(%s);
			atr.expectText("#status", "signed in", {timeout: 3000});`, js(f.button))
	}},
	// The shape the report describes: act through a target, then read through
	// one written the same way.
	{"click then read", func(f targetForm) string {
		return fmt.Sprintf(`atr.click(%s);
			atr.waitForText("signed in");
			expect(atr.text(%s)).toBe("signed in");`, js(f.button), js(strings.Replace(f.status, "idle", "signed in", 1)))
	}},
}

func TestEveryScriptCallAcceptsTheSameTargetGrammar(t *testing.T) {
	for _, api := range targetAPIs {
		for _, form := range targetForms {
			t.Run(api.name+"/"+form.name, func(t *testing.T) {
				res := run(t, `atr.step(1, "Use the target", () => {`+api.body(form)+`});`)
				if !res.Passed {
					t.Errorf("atr.%s through %s: %v", api.name, form.name, res.Failure)
				}
			})
		}
	}
}

// The repro from the report: a script whose expectText takes an XPath passes
// against a page where the text is present.
func TestExpectTextThroughXPath(t *testing.T) {
	res := run(t, `
		atr.step(1, "Verify the heading through the XPath it would be clicked with", () => {
			atr.click('//h1[contains(., "Welcome")]');
			atr.expectText('//h1[contains(., "Welcome")]', "Welcome back");
		});
	`)

	if !res.Passed {
		t.Fatalf("expectText through an XPath failed: %v", res.Failure)
	}
}

// Accepting XPath must not blunt what a read reports when the page really is
// wrong: each failure keeps the kind it has through CSS, because the kind is
// what decides whether the run is repaired, retried or reported.
func TestXPathReadsKeepTheFailureTaxonomy(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantKind FailureKind
		wantText string
	}{
		{
			name:     "a read of an XPath that matches nothing is drift",
			body:     `atr.text('//p[@id="no-such-paragraph"]');`,
			wantKind: KindNotFound,
			wantText: `//p[@id="no-such-paragraph"]`,
		},
		{
			name:     "expectText on an XPath that never appears is the application",
			body:     `atr.expectText('//p[@id="no-such-paragraph"]', "anything", {timeout: 800});`,
			wantKind: KindAssertion,
			wantText: "it was not on the page",
		},
		{
			name:     "expectText that reads the wrong thing through an XPath is the application",
			body:     `atr.expectText('//h1[@id="heading"]', "Goodbye", {timeout: 800});`,
			wantKind: KindAssertion,
			wantText: `got "Welcome back"`,
		},
		{
			name:     "an XPath the browser cannot parse is a script fault, in a read",
			body:     `atr.text('//h1[@id=');`,
			wantKind: KindScript,
			wantText: "invalid selector",
		},
		{
			name:     "an XPath the browser cannot parse is a script fault, in expectText",
			body:     `atr.expectText('//h1[@id=', "Welcome back", {timeout: 800});`,
			wantKind: KindScript,
		},
		{
			name:     "an XPath the browser cannot parse is a script fault, in a click",
			body:     `atr.click('//h1[@id=');`,
			wantKind: KindScript,
			wantText: "invalid selector",
		},
		{
			name:     "an XPath the browser cannot parse is a script fault, in a scroll",
			body:     `atr.scroll({selector: '//h1[@id=', y: 10});`,
			wantKind: KindScript,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, `atr.step(1, "Read", () => {`+tt.body+`});`)

			if res.Passed {
				t.Fatal("expected the step to fail")
			}
			if res.Failure.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q (%s)", res.Failure.Kind, tt.wantKind, res.Failure.Message)
			}
			if tt.wantText != "" && !strings.Contains(res.Failure.Message, tt.wantText) {
				t.Errorf("message = %q, want it to contain %q", res.Failure.Message, tt.wantText)
			}
		})
	}
}
