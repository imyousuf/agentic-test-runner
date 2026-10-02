package browser

import (
	"reflect"
	"testing"
	"time"
)

func readingNames(target string) []string {
	var names []string
	for _, r := range readingsOf(target) {
		names = append(names, r.name)
	}
	return names
}

// Which ways a target is read, and in what order. The order is precedence.
func TestReadingsOfAPlainTarget(t *testing.T) {
	always := []string{"aria-label", "data-testid", "name", "placeholder", "exact text", "button or link text", "label", "any text"}

	tests := []struct {
		name   string
		target string
		want   []string
	}{
		{"ordinary text", "Sign in", always},
		{"a snapshot UID", "e12", append([]string{"snapshot UID"}, always...)},
		// Things that begin like a UID and are not one.
		{"a word beginning with e", "email", always},
		{"a UID with text after it", "e2e suite", always},
		{"a bare e", "e", always},
		{"a negative index", "e-1", always},
		// A word that is also an element name is tried as a selector first.
		{"an element name", "details", append([]string{"selector"}, always...)},
		{"an element name, capitalised", "Details", append([]string{"selector"}, always...)},
		{"prose with a colon", "Total: 5 items", append([]string{"selector"}, always...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readingNames(tt.target); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readingsOf(%q) =\n  %v\nwant\n  %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestCSSString(t *testing.T) {
	tests := []struct{ in, want string }{
		{`Sign in`, `"Sign in"`},
		{`Say "hi"`, `"Say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{`it's`, `"it's"`},
		{"two\nlines", `"two\a lines"`},
		{``, `""`},
	}
	for _, tt := range tests {
		if got := cssString(tt.in); got != tt.want {
			t.Errorf("cssString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestXPathLiteral(t *testing.T) {
	tests := []struct{ in, want string }{
		{`Sign in`, `"Sign in"`},
		{`it's`, `"it's"`},
		{`Say "hi"`, `'Say "hi"'`},
		{`It's "fine"`, `concat("It's ", '"', "fine", '"')`},
		{`"'`, `concat('"', "'")`},
		{`'"`, `concat("'", '"')`},
		{``, `""`},
	}
	for _, tt := range tests {
		if got := xpathLiteral(tt.in); got != tt.want {
			t.Errorf("xpathLiteral(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// Whether a target that could be a selector was written as one. It decides
// whether the target may also be matched as part of some text.
func TestWrittenAsSelector(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		// Selector punctuation.
		{`a[href="/logout"]`, true},
		{`div[role="dialog"]`, true},
		{`main > section`, true},
		{`li:first-child`, true},
		{`div.card`, true},
		// Nothing but element names, in lower case.
		{`dialog`, true},
		{`ul li`, true},
		{`main`, true},
		// Words, which happen to be element names.
		{`Details`, false},
		{`Main Menu`, false},
		{`Address`, false},
		// Not a selector by any reading.
		{`Sign in`, false},
		{`Welcome back`, false},
		{`email`, false},
		// Has a colon, so it is guessed at; the browser settles it.
		{`Total: 5 items`, true},
	}
	for _, tt := range tests {
		if got := writtenAsSelector(tt.target); got != tt.want {
			t.Errorf("writtenAsSelector(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

func TestInPartAfter(t *testing.T) {
	tests := []struct {
		budget time.Duration
		want   time.Duration
	}{
		{200 * time.Millisecond, 0},                      // too short to divide
		{500 * time.Millisecond, 250 * time.Millisecond}, // an existence check
		{3 * time.Second, time.Second},                   // the default budget
		{15 * time.Second, time.Second},                  // the longest
	}
	for _, tt := range tests {
		if got := inPartAfter(tt.budget); got != tt.want {
			t.Errorf("inPartAfter(%v) = %v, want %v", tt.budget, got, tt.want)
		}
	}
}
