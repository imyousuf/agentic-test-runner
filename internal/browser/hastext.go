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

// resolveHasText finds the first element matching base whose text contains
// text, matched case-insensitively as Playwright does.
func resolveHasText(page *rod.Page, base, want string) (*rod.Element, error) {
	elements, err := page.Elements(base)
	if err != nil {
		return nil, invalidSelector(base, err)
	}

	want = strings.ToLower(want)
	for _, el := range elements {
		got, err := el.Text()
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(got), want) {
			return el, nil
		}
	}
	return nil, fmt.Errorf("%w: %s:has-text(%q)", ErrElementNotFound, base, want)
}

// resolveHasTextAll returns every element matching base whose text contains
// want, for the callers that operate on a set.
func resolveHasTextAll(page *rod.Page, base, want string) ([]*rod.Element, error) {
	elements, err := page.Elements(base)
	if err != nil {
		return nil, invalidSelector(base, err)
	}

	want = strings.ToLower(want)
	var out []*rod.Element
	for _, el := range elements {
		got, err := el.Text()
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(got), want) {
			out = append(out, el)
		}
	}
	return out, nil
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

// elementsMatching is Elements with :has-text() support, for the callers that
// operate on every match rather than the first.
func elementsMatching(page *rod.Page, selector string) ([]*rod.Element, error) {
	if base, want, ok := splitHasText(selector); ok {
		return resolveHasTextAll(page, base, want)
	}
	elements, err := page.Elements(selector)
	if err != nil {
		return nil, invalidSelector(selector, err)
	}
	return elements, nil
}
