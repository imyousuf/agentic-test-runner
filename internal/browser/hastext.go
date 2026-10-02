package browser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

// ErrInvalidSelector reports a selector the browser cannot parse.
//
// Distinct from "matched nothing": a selector that does not parse is a defect
// in the script, and the script runtime classifies it as such so the repair
// path can rewrite it. Reported as an ordinary failure it looked
// environmental, and an environmental failure is retried — which is how one
// compile spent its entire iteration budget on a selector that could never
// match.
var ErrInvalidSelector = errors.New("invalid selector")

// hasTextMarker is the Playwright extension the behaviour compiler reaches for
// unprompted. It is not CSS: querySelector rejects it outright.
//
// Supporting it is cheaper than forbidding it. ATR already matches elements by
// their text in findElement's fallback chain, so the capability exists; what
// was missing was the spelling people and models actually write.
const hasTextMarker = ":has-text("

// splitHasText separates a selector's CSS part from its :has-text() filter.
//
// Accepts the three spellings that turn up in practice — double-quoted,
// single-quoted and bare — and treats a bare filter as matching any element,
// the way Playwright's *:has-text() does.
func splitHasText(selector string) (base, text string, ok bool) {
	idx := strings.Index(selector, hasTextMarker)
	if idx < 0 {
		return "", "", false
	}
	rest := selector[idx+len(hasTextMarker):]
	end := strings.LastIndex(rest, ")")
	if end < 0 {
		return "", "", false
	}

	base = strings.TrimSpace(selector[:idx])
	if base == "" {
		base = "*"
	}
	// Anything after the closing paren is a selector shape we do not model.
	if strings.TrimSpace(rest[end+1:]) != "" {
		return "", "", false
	}

	text = strings.TrimSpace(rest[:end])
	text = strings.Trim(text, `"'`)
	if text == "" {
		return "", "", false
	}
	return base, text, true
}

// hasTextQuery finds, in the page, the elements matching a CSS selector whose
// text contains a string, matched case-insensitively as Playwright does.
//
// It runs in the page so that one lookup is one round trip. Filtering here
// instead — Elements, then Text on each — costs a CDP call per candidate, which
// was tolerable while a lookup ran once and is not now that it polls: div
// :has-text() on a real page is hundreds of calls a poll, all queued on the
// target the script's next action is waiting for. It was also two looks at a
// page that may change between them.
//
// What counts as an element's text is what rod's Element.Text reads, so that
// moving the match into the page did not change what matches: a field's value
// or placeholder, a select's chosen option, and visible text otherwise. An
// element with no innerText at all (SVG) falls back to its text content.
//
// Case is folded upwards, not downwards. JavaScript's toLowerCase is
// context-sensitive — a capital sigma becomes a different letter at the end of
// a word than in the middle of one — so a lower-cased needle can fail to occur
// in the lower-cased text that contains it. toUpperCase has no such rule.
const hasTextQuery = `(base, want, all) => {
	const textOf = (el) => {
		switch (el.tagName) {
		case 'INPUT':
		case 'TEXTAREA':
			return el.value || el.placeholder || '';
		case 'SELECT':
			return Array.from(el.selectedOptions).map((o) => o.innerText).join();
		default:
			return el.innerText == null ? (el.textContent || '') : el.innerText;
		}
	};
	const needle = want.toUpperCase();
	const matches = (el) => textOf(el).toUpperCase().includes(needle);
	const candidates = document.querySelectorAll(base);
	if (all) {
		return Array.from(candidates).filter(matches);
	}
	for (const el of candidates) {
		if (matches(el)) {
			return el;
		}
	}
	return null;
}`

// resolveHasText finds the first element matching base whose text contains
// want, waiting for one until the page's context expires.
//
// The waiting is the point. This used to ask once and report what it saw, so
// an element still rendering was "not found" within milliseconds whatever the
// caller was prepared to wait — where the same element named by CSS or XPath
// was polled for. ElementByJS retries exactly as Element and ElementX do,
// which is what makes the three interchangeable: the caller's deadline, read
// off the page, is the only thing that ends the search.
//
// So the page handed in must carry a deadline, as every page from tryFind
// does. The deadline arrives as a context error, which the callers translate
// with asNotFound like any other lookup that ran out of time.
func resolveHasText(page *rod.Page, base, want string) (*rod.Element, error) {
	el, err := page.ElementByJS(rod.Eval(hasTextQuery, base, want, false))
	if err != nil {
		return nil, invalidSelector(base, err)
	}
	return el, nil
}

// resolveHasTextAll returns every element matching base whose text contains
// want, for the callers that operate on a set.
//
// It does not wait, and that is deliberate: it answers what matches now, as
// Elements and ElementsX do for the other two spellings. A set has no moment
// at which it is known to be complete, so the callers treat an empty one as
// "nothing matched" whichever way the selector was written.
func resolveHasTextAll(page *rod.Page, base, want string) ([]*rod.Element, error) {
	elements, err := page.ElementsByJS(rod.Eval(hasTextQuery, base, want, true))
	if err != nil {
		return nil, invalidSelector(base, err)
	}
	return elements, nil
}

// invalidSelector reports a selector the page refused to parse.
//
// Only an unambiguous selector earns this. findElement guesses that anything
// containing a colon is CSS, which catches prose like "Total: 5 items"; sending
// that to the repair path would ask the model to fix a sentence.
func invalidSelector(selector string, err error) error {
	if err != nil && strings.Contains(err.Error(), "SyntaxError") {
		return fmt.Errorf("%w: %s", ErrInvalidSelector, selector)
	}
	return err
}

// unambiguousCSS reports whether the caller clearly meant a CSS selector,
// rather than findElement having guessed.
func unambiguousCSS(target string) bool {
	return strings.HasPrefix(target, "#") || strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "[") || strings.Contains(target, hasTextMarker)
}

// elementsMatching is Elements with XPath and :has-text() support, for the
// callers that operate on every match rather than the first. The grammar is
// findBySelector's.
func elementsMatching(page *rod.Page, selector string) ([]*rod.Element, error) {
	if isXPath(selector) {
		elements, err := page.ElementsX(selector)
		if err != nil {
			return nil, invalidSelector(selector, err)
		}
		return elements, nil
	}
	if base, want, ok := splitHasText(selector); ok {
		return resolveHasTextAll(page, base, want)
	}
	elements, err := page.Elements(selector)
	if err != nil {
		return nil, invalidSelector(selector, err)
	}
	return elements, nil
}
