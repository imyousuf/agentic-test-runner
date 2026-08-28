package browser

import (
	"strings"
	"testing"
)

// rod evaluates what it is given as `(script).apply(this, arguments)`, so the
// string must be an expression that evaluates to a function. These cases are
// the three shapes that has to be distinguished, plus the ones the old prefix
// heuristic got wrong.
func TestWrapJSExpression(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		// Already a function: wrapping would return it instead of calling it.
		{"function expression", "function() { return 1 }", "function() { return 1 }"},
		{"arrow function", "() => document.title", "() => document.title"},
		{"arrow with params", "(a,b) => a+b", "(a,b) => a+b"},
		{"async arrow", "async () => await fetch('/api')", "async () => await fetch('/api')"},

		// Expressions: wrapped so rod gets a function that returns the value.
		{"bare expression", "document.title", "() => (document.title)"},
		{"bare property", "window.location.href", "() => (window.location.href)"},
		{"method call", "document.querySelector('div').textContent", "() => (document.querySelector('div').textContent)"},
		{"arithmetic", "1 + 2", "() => (1 + 2)"},

		// An IIFE is an expression, not a function. Passing it through made rod
		// call .apply on whatever it returned.
		{"IIFE", "(function(){ return 1 })()", "() => ((function(){ return 1 })())"},
		{"arrow IIFE", "(() => 1)()", "() => ((() => 1)())"},

		// A property on something whose name begins "async" is not async code.
		{"async-prefixed identifier", "asyncThing.value", "() => (asyncThing.value)"},

		// The old heuristic keyed on the substring "return ", so an expression
		// merely mentioning the word was passed through unwrapped.
		{"expression mentioning return", `document.title + " return "`, `() => (document.title + " return ")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapJSExpression(tt.input); got != tt.expect {
				t.Errorf("wrapJSExpression(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expect)
			}
		})
	}
}

// A statement body has to be wrapped as a body, not as an expression. goja
// rejects a top-level return outright, so these must be parsed inside a
// function or the commonest case would look unparseable.
func TestWrapJSStatementBodies(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string // substrings the result must contain
	}{
		{"if with return", "if (true) { return 1 }", []string{"() => {", "if (true) { return 1 }"}},
		{"try block", "try { risky() } catch (e) { }", []string{"() => {", "try { risky() }"}},
		{"declaration then return", "const x = 1; return x", []string{"() => {", "return x"}},
		{"multiple statements", "window.scrollTo(0,0); document.title", []string{"() => {", "return (document.title)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapJSExpression(tt.input)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("wrapJSExpression(%q)\n  got:  %q\n  missing: %q", tt.input, got, want)
				}
			}
		})
	}
}

// The value of a body with a trailing expression is that expression — what a
// dev console does, and what the browser_eval schema already promises.
//
// Without this a body would evaluate to undefined, the next assertion would
// fail, and a script defect would be reported as the application misbehaving
// with no repair offered.
func TestTrailingExpressionBecomesTheValue(t *testing.T) {
	got := wrapJSExpression("const n = 41; n + 1")
	if !strings.Contains(got, "return (n + 1)") {
		t.Errorf("got %q, want the trailing expression returned", got)
	}
	if !strings.Contains(got, "const n = 41;") {
		t.Errorf("got %q, want the leading statements preserved", got)
	}
}

// A body that already returns is left alone.
func TestExplicitReturnIsNotDoubled(t *testing.T) {
	got := wrapJSExpression("if (a) { return 1 } return 2")
	if strings.Count(got, "return") != 2 {
		t.Errorf("got %q, want the two returns already there and no more", got)
	}
}

// Input goja cannot parse falls back to the old behaviour, so nothing is worse
// off than before.
func TestUnparseableFallsBackToTheOldHeuristic(t *testing.T) {
	const broken = "function( { this is not javascript"
	if got, want := wrapJSExpression(broken), legacyWrapJSExpression(broken); got != want {
		t.Errorf("got %q, want the legacy wrapping %q", got, want)
	}
}

func TestEmptyScriptIsUntouched(t *testing.T) {
	if got := wrapJSExpression("   "); got != "   " {
		t.Errorf("got %q", got)
	}
}
