package browser

import (
	"context"
	"testing"
)

func TestIsHTMLTagName(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"div", true},
		{"header", true},
		{"footer", true},
		{"nav", true},
		{"main", true},
		{"section", true},
		{"span", true},
		{"h1", true},
		{"a", true},
		{"button", true},
		{"input", true},
		{"HEADER", true}, // case insensitive
		{"Footer", true}, // case insensitive
		{"signup", false},
		{"login", false},
		{"Submit", false},
		{"e0", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isHTMLTagName(tt.input); got != tt.want {
				t.Errorf("isHTMLTagName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeCSSSelector(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// HTML tag names
		{"bare tag header", "header", true},
		{"bare tag footer", "footer", true},
		{"bare tag nav", "nav", true},
		{"bare tag main", "main", true},
		{"bare tag section", "section", true},
		{"bare tag div", "div", true},
		{"bare tag h1", "h1", true},
		{"bare tag p", "p", true},

		// Combinators
		{"child combinator", "main > section", true},
		{"adjacent sibling", "h1 + p", true},
		{"general sibling", "h1 ~ p", true},
		{"complex combinator", "main > section:nth-child(8)", true},
		{"nested child", "footer > ul > li", true},

		// Pseudo-classes/elements
		{"nth-child", "li:nth-child(2)", true},
		{"first-child", "p:first-child", true},
		{"last-of-type", "div:last-of-type", true},
		{"not pseudo", "input:not([type=hidden])", true},

		// Universal selector
		{"universal", "*", true},
		{"universal descendant", "div * span", true},

		// Class in mid-string (div.class pattern)
		{"tag with class", "div.container", true},
		{"tag with class chain", "div.foo.bar", true},
		{"input with class", "input.large", true},

		// Descendant selectors (all tag names)
		{"descendant two tags", "header nav", true},
		{"descendant three tags", "ul li a", true},
		{"descendant div p", "div p", true},

		// Plain text — should NOT match
		{"text Sign In", "Sign In", false},
		{"text Submit", "Submit", false},
		{"text Cancel Order", "Cancel Order", false},
		{"text Login", "Login", false},
		{"text email", "email", false},
		{"text username", "username", false},
		{"text password", "password", false},
		{"text Search (html tag)", "Search", true}, // <search> is a valid HTML5 tag

		// UIDs — should NOT match
		{"uid e0", "e0", false},
		{"uid e5", "e5", false},
		{"uid e123", "e123", false},

		// Mixed text with one tag name — should NOT match
		{"mixed form Submit", "form Submit", false},
		{"mixed div Login", "div Login", false},

		// Attribute selectors (contain [)
		{"attribute selector", "[data-testid=foo]", true},

		// Empty
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeCSSSelector(tt.input); got != tt.want {
				t.Errorf("looksLikeCSSSelector(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// Filling by snapshot UID must work, not just by CSS selector.
//
// UID lookup resolves through a 500ms per-attempt timeout. If the element
// comes back still bound to that timeout's context, the typing that follows
// inherits an all-but-expired deadline and fails with "context deadline
// exceeded" — which is what happened before tryFind rebound the element to
// the page's own context. Agents reach for UIDs by default, because that is
// what browser_snapshot hands them, so this path matters more than the
// selector one.
func TestFillBySnapshotUID(t *testing.T) {
	resetFixture(t)

	elements, err := testBrowser.Snapshot(false)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	var uid string
	for _, el := range elements {
		if el.TagName == "input" && el.Attributes["type"] != "checkbox" && el.Attributes["type"] != "radio" {
			uid = el.UID
			break
		}
	}
	if uid == "" {
		t.Skip("fixture has no fillable input")
	}

	if err := testBrowser.Fill(context.Background(), uid, "typed-by-uid"); err != nil {
		t.Fatalf("Fill by UID %s: %v", uid, err)
	}

	res, err := testBrowser.Evaluate(`document.querySelectorAll("button, input, select, textarea, a, [role], [aria-label], [data-testid]")[` +
		uid[1:] + `].value`)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if got, _ := res.(string); got != "typed-by-uid" {
		t.Errorf("field holds %q, want %q", got, "typed-by-uid")
	}
}
