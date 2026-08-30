package agent

import (
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/testscript"
)

const origLogin = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.click("#submit");
  atr.expectExists("#dashboard");
});`

const origCheckout = `atr.step(1, "Sign in", () => {
  atr.navigate("/login");
  atr.fill("#username", values.get("username"));
  atr.click("#submit");
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toBe("Order placed");
});`

func original() map[string]string {
	return map[string]string{
		"login.test.js":    origLogin,
		"checkout.test.js": origCheckout,
	}
}

const soundLibrary = `// Signs in and leaves the browser on the dashboard.
function signIn(username) {
  atr.navigate("/login");
  atr.fill("#username", username);
  atr.click("#submit");
}`

// The shape a good extraction has: the operations move, every claim stays
// exactly where it was.
func TestASoundExtractionIsAccepted(t *testing.T) {
	ex := &Extraction{
		Library: soundLibrary,
		Scripts: map[string]string{
			"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});`,
			"checkout.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toBe("Order placed");
});`,
		},
		Reason: "hoisted signIn",
	}

	if err := ValidateExtraction(original(), ex); err != nil {
		t.Errorf("a sound extraction was refused: %v", err)
	}
}

// Every way a proposal can be wrong, refused before a byte is written and
// before anything is run. Each of these would pass a replay.
func TestAnUnsoundExtractionIsRefused(t *testing.T) {
	tests := map[string]struct {
		library string
		scripts map[string]string
		says    string
	}{
		"an assertion is dropped": {
			library: soundLibrary,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => { signIn(values.get("username")); });`,
			},
			says: "claims",
		},
		"an assertion is loosened": {
			library: soundLibrary,
			scripts: map[string]string{
				"checkout.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});
atr.step(2, "Check out", () => {
  atr.click("#checkout");
  expect(atr.text("#status")).toContain("Order");
});`,
			},
			says: "claims",
		},
		"an assertion is dragged into the library": {
			library: soundLibrary + `
function checkDashboard() { atr.expectExists("#dashboard"); }`,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  checkDashboard();
});`,
			},
			says: "claims",
		},
		"the library runs code at load time": {
			library: `const HERE = atr.url();
function signIn(u) { atr.navigate("/login"); atr.fill("#username", u); atr.click("#submit"); }`,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => {
  signIn(values.get("username"));
  atr.expectExists("#dashboard");
});`,
			},
			says: "not one",
		},
		"a script that is not in the directory": {
			library: soundLibrary,
			scripts: map[string]string{
				"invented.test.js": `atr.step(1, "x", () => { atr.expectExists("#a"); });`,
			},
			says: "not a script in this directory",
		},
		"the rewrite cannot fail": {
			library: soundLibrary,
			scripts: map[string]string{
				"login.test.js": `atr.step(1, "Sign in", () => { signIn(values.get("username")); atr.log("done"); });`,
			},
			says: "claims",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidateExtraction(original(), &Extraction{Library: tt.library, Scripts: tt.scripts})
			if err == nil {
				t.Fatal("an unsound extraction was accepted")
			}
			if !strings.Contains(err.Error(), tt.says) {
				t.Errorf("the refusal does not say why: %v", err)
			}
		})
	}
}

// A considered refusal is an answer, not a failure: the agent is told it may
// decline when an overlap is not worth naming.
func TestADeclinedExtractionIsNotAnError(t *testing.T) {
	ex, err := parseExtraction("REASON: the two sequences differ in every argument, so naming " +
		"them would need a parameter per line.")
	if err != nil {
		t.Fatalf("a considered refusal was read as a failure: %v", err)
	}
	if !ex.Empty() {
		t.Error("a refusal produced files")
	}
	if ex.Reason == "" {
		t.Error("the refusal carries no reason")
	}
	if err := ValidateExtraction(original(), ex); err != nil {
		t.Errorf("validating a refusal failed: %v", err)
	}
}

// A library nothing calls, or rewrites with no library, are incoherent
// answers rather than refusals.
func TestAnIncoherentProposalIsAnError(t *testing.T) {
	only := "=== FILE: " + testscript.LibraryName + "\n```javascript\nfunction f() {}\n```\nREASON: x"
	if _, err := parseExtraction(only); err == nil {
		t.Error("a library that nothing calls was accepted")
	}

	scriptOnly := "=== FILE: login.test.js\n```javascript\natr.step(1, \"x\", () => {});\n```\nREASON: x"
	if _, err := parseExtraction(scriptOnly); err == nil {
		t.Error("rewrites with no library were accepted")
	}
}

// The reply format has to survive the shapes a model actually emits.
func TestTheReplyIsParsed(t *testing.T) {
	reply := "Here is the extraction.\n\n" +
		"=== FILE: " + testscript.LibraryName + "\n```javascript\n" + soundLibrary + "\n```\n\n" +
		"=== FILE: login.test.js\n```js\natr.step(1, \"Sign in\", () => { signIn(); });\n```\n\n" +
		"REASON: hoisted the three sign-in operations\n"

	ex, err := parseExtraction(reply)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ex.Library, "function signIn") {
		t.Errorf("the library was not read: %q", ex.Library)
	}
	if len(ex.Scripts) != 1 || !strings.Contains(ex.Scripts["login.test.js"], "signIn()") {
		t.Errorf("the rewrite was not read: %+v", ex.Scripts)
	}
	if ex.Reason != "hoisted the three sign-in operations" {
		t.Errorf("reason = %q", ex.Reason)
	}
}
