package browser

import (
	"testing"
)

func TestWrapJSExpression(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"bare expression", "document.title", "() => (document.title)"},
		{"bare property", "window.location.href", "() => (window.location.href)"},
		{"function expression", "function() { return 1 }", "function() { return 1 }"},
		{"arrow function", "() => document.title", "() => document.title"},
		{"async function", "async () => await fetch('/api')", "async () => await fetch('/api')"},
		{"IIFE", "(function(){ return 1 })()", "(function(){ return 1 })()"},
		{"has return", "if (true) { return 1 }", "if (true) { return 1 }"},
		{"method call", "document.querySelector('div').textContent", "() => (document.querySelector('div').textContent)"},
		{"arithmetic", "1 + 2", "() => (1 + 2)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapJSExpression(tt.input)
			if got != tt.expect {
				t.Errorf("wrapJSExpression(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expect)
			}
		})
	}
}
