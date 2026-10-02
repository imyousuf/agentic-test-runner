package browser

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// A plain target is one that is not a selector: visible text, an aria-label,
// a data-testid, a name, a placeholder, or a UID from a snapshot. Nothing in
// the string says which, so it is read each way in turn and the first reading
// that names an element wins.

// snapshotSelector picks the elements a snapshot numbers. A UID is an index
// into exactly this list, so the snapshot and the lookup share it.
const snapshotSelector = "button, input, select, textarea, a, [role], [aria-label], [data-testid]"

// uidPattern is a snapshot UID: "e" and an index, and nothing else.
//
// It used to be read with Sscanf("e%d"), which stops at the first character
// that is not a digit and calls that a match — so the visible text "e2e
// suite" was snapshot element 2, and a click on it landed on whatever that
// happened to be.
var uidPattern = regexp.MustCompile(`^e(\d+)$`)

// reading is one way of interpreting a plain target.
type reading struct {
	name string
	// inPart is true when the reading matches the target as part of some
	// longer text rather than as the whole of something. Those are the loose
	// readings, and they are held back: see findPlainTarget.
	inPart bool
	// look takes a single look at the page and returns the element this
	// reading names, or nil when it names nothing right now. It never waits.
	look func(p *rod.Page) (*rod.Element, error)
}

// selectorReading is the name of the reading that tries a target as CSS.
const selectorReading = "selector"

// readingsOf lists the ways a plain target can be read, in order of
// precedence.
//
// The order is the contract: an aria-label beats visible text, a data-testid
// beats a name, and so on down. It is what the fallback chain has always
// tried, in the order it has always tried it.
func readingsOf(target string) []reading {
	var out []reading

	// A word that is also a valid selector — "main", "details", "div span" —
	// is tried as one first. Only a guess, so a miss is not an answer.
	if looksLikeCSSSelector(target) {
		out = append(out, reading{name: selectorReading, look: func(p *rod.Page) (*rod.Element, error) {
			return has(p.Has(target))
		}})
	}

	if m := uidPattern.FindStringSubmatch(target); m != nil {
		out = append(out, reading{name: "snapshot UID", look: func(p *rod.Page) (*rod.Element, error) {
			index, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, nil
			}
			elements, err := p.Elements(snapshotSelector)
			if err != nil || index >= len(elements) {
				return nil, err
			}
			return elements[index], nil
		}})
	}

	for _, attribute := range []string{"aria-label", "data-testid", "name", "placeholder"} {
		selector := "[" + attribute + "=" + cssString(target) + "]"
		out = append(out, reading{name: attribute, look: func(p *rod.Page) (*rod.Element, error) {
			return has(p.Has(selector))
		}})
	}

	// Exact text first: precise, and it does not match the containers the
	// text happens to sit inside. Within the body, and not in the elements
	// that hold no content: a page titled "Sign in" with a button that says
	// the same used to resolve to the <title>, which cannot be clicked, and
	// the click sat for thirty seconds waiting for it to become clickable.
	exactText := "//body/descendant-or-self::*[" + notContentXPath + "][normalize-space(text())=" + xpathLiteral(target) + "]"
	out = append(out, reading{name: "exact text", look: func(p *rod.Page) (*rod.Element, error) {
		return has(p.HasX(exactText))
	}})

	// Then text in part, for buttons and links only.
	out = append(out, reading{name: "button or link text", inPart: true, look: func(p *rod.Page) (*rod.Element, error) {
		return lookByJS(p, controlTextQuery, target)
	}})

	// A label names the field it is for.
	labelText := "^" + regexp.QuoteMeta(target) + "$"
	out = append(out, reading{name: "label", look: func(p *rod.Page) (*rod.Element, error) {
		label, err := has(p.HasR("label", labelText))
		if label == nil {
			return nil, err
		}
		field, err := label.Attribute("for")
		if err != nil || field == nil || *field == "" {
			return nil, err
		}
		return has(p.Has("[id=" + cssString(*field) + "]"))
	}})

	// Last, text anywhere on the page: the innermost element showing it.
	out = append(out, reading{name: "any text", inPart: true, look: func(p *rod.Page) (*rod.Element, error) {
		return lookByJS(p, visibleTextQuery, target)
	}})

	return out
}

// selectorPunctuation is what a selector has and ordinary words do not.
const selectorPunctuation = "[]>+~#.:*"

// writtenAsSelector reports whether a target that could be a selector was
// evidently written as one: it has selector punctuation, or it is nothing but
// element names in lower case — "dialog", "ul li" — the way a selector is
// written and a label is not.
//
// It decides whether the target may also be matched as part of some text.
// "Details" could be either, and a person who writes it means the link that
// says so; a[href="/logout"] and "dialog" are selectors, and matching them
// inside whatever text happens to contain those letters answers a question
// nobody asked.
func writtenAsSelector(target string) bool {
	if !looksLikeCSSSelector(target) {
		return false
	}
	return strings.ContainsAny(target, selectorPunctuation) || target == strings.ToLower(target)
}

// notContentXPath excludes the elements whose text is not part of what a page
// says: code, styles and markup held in reserve.
const notContentXPath = "not(self::script or self::style or self::noscript or self::template)"

// controlTextQuery finds the first button or link whose text contains the
// target.
//
// As written, not as a pattern. This was rod's ElementR, which reads its
// argument as a regular expression — so "Price (USD" was a syntax error, and a
// selector not yet on the page was a pattern that matched almost anything:
// a[href="/logout"] is the letter a followed by one of a dozen characters, and
// "Features" has one. A target is something a person typed.
const controlTextQuery = `(needle) => {
	const textOf = ` + jsTextOf + `;
	return Array.from(document.querySelectorAll('button, a')).find((el) => textOf(el).includes(needle)) || null;
}`

// visibleTextQuery finds the innermost element on the page that shows a given
// text.
//
// "Shows" is the point. This used to be rod's ElementR over every element in
// the document, which takes the first whose text matches — and the text of an
// element that is not rendered is its source. So text that appeared only in an
// inline script matched the <script>: a wait for "Order placed" was satisfied
// at once by the code that would one day display it, and a target that had not
// rendered yet resolved, on the first look, to the script that was going to
// render it. Now the text has to be in what the body renders, or in the value
// a field is showing.
//
// And "innermost", because the first element whose text contains anything on
// the page is the root. A click on a partial text landed in the middle of the
// viewport. From the body the search descends for as long as a single child
// still shows the whole match, and stops at the smallest element that does.
//
// The text is matched as written, like controlTextQuery and for its reasons.
const visibleTextQuery = `(needle) => {
	const textOf = ` + jsTextOf + `;
	const matches = (el) => textOf(el).includes(needle);

	const body = document.body;
	if (!body || needle === '') {
		return null;
	}
	const notContent = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE']);
	const shown = (el) => el.getClientRects().length > 0 || getComputedStyle(el).display === 'contents';

	let el = null;
	if (matches(body)) {
		el = body;
	} else {
		// What a field shows is its value, which is no element's text.
		el = Array.from(body.querySelectorAll('input, textarea, select')).find((f) => shown(f) && matches(f)) || null;
	}
	if (!el) {
		return null;
	}

	for (;;) {
		const child = Array.from(el.children).find((c) => !notContent.has(c.tagName) && shown(c) && matches(c));
		if (!child) {
			return el;
		}
		el = child;
	}
}`

// lookByJS takes one look at the page with a query that returns an element or
// null, and reports null as (nil, nil) rather than as an error.
func lookByJS(p *rod.Page, js string, args ...any) (*rod.Element, error) {
	el, err := p.Sleeper(rod.NotFoundSleeper).ElementByJS(rod.Eval(js, args...))
	if errors.Is(err, &rod.ElementNotFoundError{}) {
		return nil, nil
	}
	return el, err
}

// has adapts rod's Has family, which reports a miss as (false, nil, nil).
func has(found bool, el *rod.Element, err error) (*rod.Element, error) {
	if err != nil || !found {
		return nil, err
	}
	return el, nil
}

// inPartHeadStart is how long the exact readings of a target are tried alone
// before the in-part ones join them, for a lookup with time to spare.
const inPartHeadStart = time.Second

// inPartAfter is when the in-part readings join a lookup with this budget:
// half way through it, and no later than inPartHeadStart. A lookup too short
// to divide looks every way from the start.
func inPartAfter(budget time.Duration) time.Duration {
	if budget < 4*lookupLastLook {
		return 0
	}
	return min(budget/2, inPartHeadStart)
}

// findPlainTarget resolves a target that is not a selector, waiting up to
// budget for some reading of it to name an element.
//
// Every reading is tried on every look, and the looks are what is spread over
// the budget. It used to be the other way round: each reading waited a slice
// of the budget before the next was tried, and the slices added up to more
// than the budget they were cut from. So the later readings never had a turn.
// Inside the half second an existence check allows, only the aria-label was
// ever looked for — atr.exists("Sign in") was false with the button on the
// page, and atr.expectMissing("Sign in") passed. With ten seconds, text in
// the middle of a paragraph was still out of reach, so atr.expectExists
// reported a heading that was there all along as an assertion failure. And an
// action on visible text that *was* reached had first waited out the four
// attributes tried before it: two seconds for a click on something already
// there.
//
// A reading that names nothing is tried again on the next look, because the
// page may yet render what it names. A reading the browser refuses outright —
// a guessed selector that does not parse — can never name anything, and is
// dropped.
//
// Two things keep "every reading, every look" from being looser than the
// slices were.
//
// The in-part readings are held back at first. Trying everything at once
// makes precedence a matter of what has rendered so far: a click on "Save"
// while the page shows "Saved filters" and the Save button is a render away
// would land on the heading. So the exact readings get a head start, and text
// in part is only matched once the page has had that long to produce
// something better.
//
// And a target written as a selector is never matched in part at all, for as
// long as it stands as a selector. A selector that matches nothing yet is
// something to wait for, not something to look for inside the page's text.
// Once the browser refuses it as a selector — "Total: 5 items" has a colon and
// is not one — it is words like any others.
func findPlainTarget(page *rod.Page, target string, budget time.Duration) (*rod.Element, error) {
	readings := readingsOf(target)
	asSelector := writtenAsSelector(target)

	p := page.Timeout(budget)
	defer p.CancelTimeout()
	ctx := p.GetContext()

	started := time.Now()
	inPartFrom := inPartAfter(budget)

	sleep := lookupSleeper()
	for {
		inPart := !asSelector && time.Since(started) >= inPartFrom

		for i := 0; i < len(readings); {
			if readings[i].inPart && !inPart {
				i++
				continue
			}

			el, err := readings[i].look(p)
			if el != nil && err == nil {
				return actionBound(page, el), nil
			}
			if isSyntaxError(err) {
				if readings[i].name == selectorReading {
					asSelector = false
				}
				readings = append(readings[:i], readings[i+1:]...)
				continue
			}
			// Anything else — the page navigating under the look, the budget
			// running out part way through — is not an answer either way.
			i++
		}

		if err := sleep(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("%w: %s", ErrElementNotFound, target)
			}
			return nil, err
		}
	}
}

// cssString quotes a value for use inside a CSS selector.
func cssString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n', '\r', '\f':
			// A raw newline ends a CSS string; escaped by code point.
			fmt.Fprintf(&b, `\%x `, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// xpathLiteral quotes a value for use inside an XPath expression. XPath 1.0
// has no escape character, so a value containing both kinds of quote has to
// be assembled from pieces.
func xpathLiteral(value string) string {
	if !strings.Contains(value, `"`) {
		return `"` + value + `"`
	}
	if !strings.Contains(value, `'`) {
		return `'` + value + `'`
	}

	parts := strings.Split(value, `"`)
	pieces := make([]string, 0, 2*len(parts))
	for i, part := range parts {
		if i > 0 {
			pieces = append(pieces, `'"'`)
		}
		if part != "" {
			pieces = append(pieces, `"`+part+`"`)
		}
	}
	return "concat(" + strings.Join(pieces, ", ") + ")"
}
