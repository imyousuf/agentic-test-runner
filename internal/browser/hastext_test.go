package browser

import "testing"

// The three spellings that turn up in practice, plus the shapes that must not
// be mistaken for one.
func TestSplitHasText(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		base     string
		text     string
		ok       bool
	}{
		{"double quoted", `button:has-text("Sign in")`, "button", "Sign in", true},
		{"single quoted", `button:has-text('Sign in')`, "button", "Sign in", true},
		{"unquoted", `button:has-text(Sign in)`, "button", "Sign in", true},
		{"compound base", `div.card > button:has-text("Buy")`, "div.card > button", "Buy", true},
		{"no base matches anything", `:has-text("Total")`, "*", "Total", true},
		{"text containing parens", `button:has-text("Buy (2)")`, "button", "Buy (2)", true},

		{"plain css", "button.primary", "", "", false},
		{"pseudo class", "li:first-child", "", "", false},
		{"empty text", `button:has-text("")`, "", "", false},
		{"unterminated", `button:has-text("Sign in"`, "", "", false},
		{"trailing selector is not modelled", `button:has-text("x") span`, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, text, ok := splitHasText(tt.selector)
			if ok != tt.ok {
				t.Fatalf("splitHasText(%q) ok = %v, want %v", tt.selector, ok, tt.ok)
			}
			if !ok {
				return
			}
			if base != tt.base || text != tt.text {
				t.Errorf("splitHasText(%q) = (%q, %q), want (%q, %q)",
					tt.selector, base, text, tt.base, tt.text)
			}
		})
	}
}

// findElement guesses that anything containing a colon is CSS. Prose must not
// be treated as an unambiguous selector, or a sentence would be sent to the
// repair path to be "fixed".
func TestUnambiguousCSSDoesNotClaimProse(t *testing.T) {
	unambiguous := []string{"#id", ".cls", "[name=x]", `button:has-text("Go")`}
	guessed := []string{"Total: 5 items", "Sign in", "li:first-child", "div span"}

	for _, s := range unambiguous {
		if !unambiguousCSS(s) {
			t.Errorf("%q should be treated as an explicit selector", s)
		}
	}
	for _, s := range guessed {
		if unambiguousCSS(s) {
			t.Errorf("%q is a guess, not an explicit selector", s)
		}
	}
}
