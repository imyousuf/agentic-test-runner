package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
)

// :has-text() is matched in the page now, in one round trip, where it used to
// be matched here by asking rod for each candidate's text in turn. Moving it
// must not have changed what matches, so these pin what "an element's text"
// means — and check it against rod's own Element.Text, which is what the old
// code compared.

func openHasTextSemantics(t *testing.T) *rod.Page {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text_semantics.html"); err != nil {
		t.Fatal(err)
	}
	page, err := testBrowser.CurrentPage()
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func idOf(t *testing.T, el *rod.Element) string {
	t.Helper()
	id, err := el.Attribute("id")
	if err != nil {
		t.Fatalf("reading the id of the element found: %v", err)
	}
	if id == nil {
		return ""
	}
	return *id
}

func TestHasTextMatchesWhatAnElementReads(t *testing.T) {
	tests := []struct {
		name   string
		target string
		wantID string
		// rodAgrees is false where the page-side match is deliberately wider
		// than rod's Text: an SVG element has no innerText for it to read.
		rodAgrees bool
	}{
		{"case is ignored, needle lower", `button:has-text("sign in")`, "shout", true},
		{"case is ignored, needle mixed", `button:has-text("SiGn In")`, "shout", true},
		{"text containing parentheses", `button:has-text("Buy (2)")`, "buy", true},
		{"text containing an apostrophe", `button:has-text("Don't")`, "quote", true},
		// The quotes around a needle are trimmed, so one that is to reach the
		// page has to sit inside it.
		{"text containing a double quote", `button:has-text("t "ask")`, "quote", true},
		{"single-quoted", `button:has-text('Buy')`, "buy", true},
		{"unquoted", `button:has-text(Buy)`, "buy", true},
		{"an empty field reads as its placeholder", `input:has-text("Search orders")`, "placeheld", true},
		{"a filled field reads as its value", `input:has-text("typed value")`, "valued", true},
		{"a textarea reads as its value", `textarea:has-text("Long form")`, "area", true},
		{"a select reads as its chosen option", `select:has-text("Second choice")`, "pick", true},
		{"text inside a child counts for the parent", `p:has-text("inner bold")`, "nested", true},
		{"and for the child", `b:has-text("inner bold")`, "bold", true},
		{"text spanning a child boundary", `p:has-text("Outer inner bold tail")`, "nested", true},
		{"an element that is not rendered still has text", `p:has-text("Hidden words")`, "hidden", true},
		{"an SVG label", `text:has-text("Chart title")`, "svg-label", false},
		{"the first match in document order", `li.item:has-text("alpha")`, "alpha-one", true},
		{"a compound base", `ul > li.item:has-text("Beta")`, "beta", true},
		// Case folding that depends on position in a word must not stop a
		// needle matching inside a longer one: a lower-cased "ΣΟΣ" ends in a
		// final sigma, and "ΣΟΣΙΑΣ" lower-cased has an ordinary one there.
		{"a needle ending where a word does not", `p:has-text("ΣΟΣ")`, "greek", true},
		{"the same, in lower case", `p:has-text("σοσιασ οδοσ")`, "greek", true},
		// The first element in document order whose text contains the needle
		// is the root, which is what a bare filter has always resolved to.
		{"a bare filter matches any element", `:has-text("Alpha two")`, "<html>", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := openHasTextSemantics(t)

			el, err := testBrowser.findBySelector(page.Timeout(2*time.Second), tt.target)
			if err != nil {
				t.Fatalf("%s: %v", tt.target, err)
			}
			got := idOf(t, el)
			if tt.wantID == "<html>" {
				// The root has no id to check, so check that it is the root.
				tag, err := el.Property("tagName")
				if err != nil {
					t.Fatal(err)
				}
				got = "<" + strings.ToLower(tag.String()) + ">"
			}
			if got != tt.wantID {
				t.Errorf("%s matched %s, want %s", tt.target, got, tt.wantID)
			}

			if !tt.rodAgrees {
				return
			}
			_, want, _ := splitHasText(tt.target)
			text, err := el.Text()
			if err != nil {
				t.Fatalf("rod could not read the element's text: %v", err)
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
				t.Errorf("matched an element rod reads as %q, which does not contain %q", text, want)
			}
		})
	}
}

// The other half: what must not match.
func TestHasTextDoesNotMatchWhatAnElementDoesNotRead(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{"a placeholder behind a value", `input:has-text("Ignored placeholder")`},
		{"an option that is not the chosen one", `select:has-text("First choice")`},
		{"text on an element the base excludes", `button:has-text("Alpha one")`},
		{"text that is nowhere", `li.item:has-text("Gamma")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := openHasTextSemantics(t)

			_, err := testBrowser.findBySelector(page.Timeout(400*time.Millisecond), tt.target)
			if !errors.Is(err, ErrElementNotFound) {
				t.Errorf("%s: err = %v, want ErrElementNotFound", tt.target, err)
			}
		})
	}
}

// The all-matches form shares the same test of what matches, and answers what
// is there now: an empty set is an answer, not something to wait out.
func TestHasTextAllReturnsEveryMatchInOrder(t *testing.T) {
	page := openHasTextSemantics(t)

	ids := func(selector string) []string {
		t.Helper()
		elements, err := elementsMatching(page.Timeout(3*time.Second), selector)
		if err != nil {
			t.Fatalf("%s: %v", selector, err)
		}
		var out []string
		for _, el := range elements {
			out = append(out, idOf(t, el))
		}
		return out
	}

	if got := ids(`li.item:has-text("ALPHA")`); strings.Join(got, ",") != "alpha-one,alpha-two" {
		t.Errorf("matched %v, want both alpha items in document order", got)
	}
	if got := ids(`li.item:has-text("Beta")`); strings.Join(got, ",") != "beta" {
		t.Errorf("matched %v, want only beta", got)
	}

	start := time.Now()
	if got := ids(`li.item:has-text("Gamma")`); len(got) != 0 {
		t.Errorf("matched %v, want nothing", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v to report an empty set", elapsed.Round(time.Millisecond))
	}

	if _, err := elementsMatching(page.Timeout(3*time.Second), `li[[[bad:has-text("Alpha")`); !errors.Is(err, ErrInvalidSelector) {
		t.Errorf("err = %v, want ErrInvalidSelector for a base that does not parse", err)
	}
}
