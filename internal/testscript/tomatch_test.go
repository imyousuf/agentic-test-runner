package testscript

import (
	"strings"
	"testing"
)

// The reported bug, exactly as it arrived: a valid id failing a correct
// assertion, with the pattern reported as //^…$// — the doubled slashes being
// the tell that the regex's own toString had been compiled as a Go pattern.
func TestToMatchAcceptsARegexLiteral(t *testing.T) {
	res := run(t, `
		atr.step(1, "Check an id", () => {
			expect("HZyLBnZ450oL8szOgE19").toMatch(/^[A-Za-z0-9_-]+$/);
		});
	`)

	if !res.Passed {
		t.Fatalf("a value that plainly matches was rejected: %v", res.Failure)
	}
}

// It still has to fail when it should.
func TestToMatchStillRejectsANonMatch(t *testing.T) {
	res := run(t, `
		atr.step(1, "Check an id", () => {
			expect("has spaces and !").toMatch(/^[A-Za-z0-9_-]+$/);
		});
	`)

	if res.Passed {
		t.Fatal("expected the assertion to fail")
	}
	if res.Failure.Kind != KindAssertion {
		t.Errorf("kind = %q, want %q", res.Failure.Kind, KindAssertion)
	}
	// The message must show the pattern once, not wrapped in a second pair.
	if strings.Contains(res.Failure.Message, "//") {
		t.Errorf("the pattern is double-delimited: %s", res.Failure.Message)
	}
}

// Flags are part of the pattern and have to be honoured.
func TestToMatchHonoursRegexFlags(t *testing.T) {
	res := run(t, `
		atr.step(1, "Case-insensitive", () => {
			expect("Order Placed").toMatch(/order placed/i);
		});
		atr.step(2, "Multiline", () => {
			expect("first\nsecond").toMatch(/^second$/m);
		});
	`)

	if !res.Passed {
		t.Fatalf("flags were ignored: %v", res.Failure)
	}
}

// Go's regexp is RE2, which has no lookahead. Compiling a JavaScript pattern
// as a Go one would not merely mismatch here — it would fail to compile.
func TestToMatchSupportsJavaScriptOnlySyntax(t *testing.T) {
	res := run(t, `
		atr.step(1, "Lookahead", () => {
			expect("abc123").toMatch(/^(?=.*\d)[a-z\d]+$/);
		});
	`)

	if !res.Passed {
		t.Fatalf("a JavaScript-only pattern failed: %v", res.Failure)
	}
}

// A string argument keeps meaning a pattern, which is what it always meant.
func TestToMatchStillAcceptsAStringPattern(t *testing.T) {
	res := run(t, `
		atr.step(1, "String pattern", () => {
			expect("Order placed").toMatch("^Order");
		});
	`)

	if !res.Passed {
		t.Fatalf("a string pattern stopped working: %v", res.Failure)
	}
}

// A malformed string pattern is the script's fault, not the application's, so
// it must stay repairable rather than being reported as a regression.
func TestToMatchWithAnUnparseablePatternIsAScriptFault(t *testing.T) {
	res := run(t, `
		atr.step(1, "Bad pattern", () => {
			expect("anything").toMatch("[unclosed");
		});
	`)

	if res.Passed {
		t.Fatal("expected the step to fail")
	}
	if res.Failure.Kind != KindScript {
		t.Errorf("kind = %q, want %q so the script can be repaired", res.Failure.Kind, KindScript)
	}
}
