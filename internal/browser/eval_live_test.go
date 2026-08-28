package browser

import (
	"testing"
)

// The classifier is only right if the browser agrees. These run the four
// shapes through a real page, which is where the reported errors surfaced.
func TestEvaluateAcceptsEveryShape(t *testing.T) {
	resetFixture(t)

	tests := []struct {
		name   string
		script string
		want   any
	}{
		{"bare expression", "1 + 2", float64(3)},
		{"function expression", "function() { return 7 }", float64(7)},
		{"arrow function", "() => 8", float64(8)},
		// Reported as: (intermediate value)(intermediate value)(...).apply is
		// not a function.
		{"IIFE", "(function(){ return 9 })()", float64(9)},
		// Reported as: SyntaxError: Unexpected token 'try'.
		{"try block", "try { return 10 } catch (e) { return -1 }", float64(10)},
		{"declaration then return", "const x = 11; return x", float64(11)},
		// The dev-console behaviour the tool schema promises: a trailing
		// expression is the value. Without it this returns undefined and the
		// next assertion blames the application.
		{"trailing expression", "const y = 12; y", float64(12)},
		{"statement then expression", "window.scrollTo(0,0); 13", float64(13)},
		{"expression mentioning return", `"a return b"`, "a return b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testBrowser.Evaluate(tt.script)
			if err != nil {
				t.Fatalf("Evaluate(%q): %v", tt.script, err)
			}
			if got != tt.want {
				t.Errorf("Evaluate(%q) = %#v, want %#v", tt.script, got, tt.want)
			}
		})
	}
}

// A script that is genuinely wrong must still fail, and must not be run twice
// in the attempt.
func TestEvaluateStillReportsARealFailure(t *testing.T) {
	resetFixture(t)

	if _, err := testBrowser.Evaluate("nonexistentFunction()"); err == nil {
		t.Error("expected an error for a call to something undefined")
	}
}

// Side effects must happen once. The retry is guarded on SyntaxError, which
// means nothing executed; anything else must not be replayed.
func TestEvaluateDoesNotRunSideEffectsTwice(t *testing.T) {
	resetFixture(t)

	if _, err := testBrowser.Evaluate("window.__atrEvalCount = 0; 1"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := testBrowser.Evaluate("window.__atrEvalCount++; window.__atrEvalCount"); err != nil {
		t.Fatalf("incrementing: %v", err)
	}
	got, err := testBrowser.Evaluate("window.__atrEvalCount")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got != float64(1) {
		t.Errorf("counter = %#v, want 1 — the script ran more than once", got)
	}
}
