package browser

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// What one look at the page is told about a target: which readings apply.
func TestQueryForAPlainTarget(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		wantSelector string
		wantUID      int
	}{
		{"ordinary text", "Sign in", "", -1},
		{"a snapshot UID", "e12", "", 12},
		{"the first snapshot UID", "e0", "", 0},
		// Things that begin like a UID and are not one.
		{"a word beginning with e", "email", "", -1},
		{"a UID with text after it", "e2e suite", "", -1},
		{"a bare e", "e", "", -1},
		{"a negative index", "e-1", "", -1},
		// A word that is also an element name is tried as a selector first.
		{"an element name", "details", "details", -1},
		{"an element name, capitalised", "Details", "Details", -1},
		{"prose with a colon", "Total: 5 items", "Total: 5 items", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := queryFor(tt.target)

			if q.Selector != tt.wantSelector {
				t.Errorf("Selector = %q, want %q", q.Selector, tt.wantSelector)
			}
			if q.UID != tt.wantUID {
				t.Errorf("UID = %d, want %d", q.UID, tt.wantUID)
			}
			if q.Target != tt.target {
				t.Errorf("Target = %q, want the target as written", q.Target)
			}
			// A UID is an index into what a snapshot numbers.
			if q.Snapshot != snapshotSelector {
				t.Errorf("Snapshot = %q, want the snapshot's own selector", q.Snapshot)
			}
			// The in-part readings are off until the lookup turns them on.
			if q.InPart {
				t.Error("InPart is on from the start")
			}
		})
	}
}

// The attribute readings, in the order they take precedence, with the target
// quoted so that punctuation in it is not syntax.
func TestQueryForQuotesTheTarget(t *testing.T) {
	q := queryFor(`Say "hi"`)

	want := []string{
		`[aria-label="Say \"hi\""]`,
		`[data-testid="Say \"hi\""]`,
		`[name="Say \"hi\""]`,
		`[placeholder="Say \"hi\""]`,
	}
	if !reflect.DeepEqual(q.Attributes, want) {
		t.Errorf("Attributes =\n  %q\nwant\n  %q", q.Attributes, want)
	}
	if !strings.Contains(q.ExactText, `normalize-space(text())='Say "hi"'`) {
		t.Errorf("ExactText = %s, want the target as an XPath literal", q.ExactText)
	}
	// Scoped to what a page says: the body, less its code and styles.
	if !strings.HasPrefix(q.ExactText, "//body/") || !strings.Contains(q.ExactText, "self::script") {
		t.Errorf("ExactText = %s, want it kept to the body's content", q.ExactText)
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
