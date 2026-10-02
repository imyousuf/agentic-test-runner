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

// plainQuery is a plain target, worked out into everything one look at the
// page needs in order to read it every way it can be read.
//
// The fields are in the order the readings are tried, which is the order of
// precedence: an aria-label beats visible text, a data-testid beats a name,
// and so on down. It is what the fallback chain has always tried, in the order
// it has always tried it.
type plainQuery struct {
	// Target is the target as written, for the readings that match text.
	Target string `json:"target"`
	// Selector is the target as a CSS selector, when it could be one: a word
	// that is also an element name, "div span". Only a guess, so a miss is
	// not an answer and one the browser cannot parse is not an error.
	Selector string `json:"selector"`
	// UID is the index a snapshot UID names, or -1.
	UID int `json:"uid"`
	// Snapshot is the selector whose matches a UID indexes.
	Snapshot string `json:"snapshot"`
	// Attributes are the attribute readings, as selectors: aria-label,
	// data-testid, name, placeholder.
	Attributes []string `json:"attributes"`
	// ExactText is an XPath for an element whose own text is the target.
	//
	// Within the body, and not in the elements that hold no content: a page
	// titled "Sign in" with a button that says the same used to resolve to
	// the <title>, which cannot be clicked, and the click sat for thirty
	// seconds waiting for it to become clickable.
	ExactText string `json:"exactText"`
	// InPart turns on the two readings that match the target as part of some
	// longer text: a button or link containing it, and any text on the page.
	// They are the loose end of the list and are held back; see
	// findPlainTarget.
	InPart bool `json:"inPart"`
}

// queryFor works a plain target out into its readings.
func queryFor(target string) plainQuery {
	q := plainQuery{
		Target:    target,
		UID:       -1,
		Snapshot:  snapshotSelector,
		ExactText: "//body/descendant-or-self::*[" + notContentXPath + "][normalize-space(text())=" + xpathLiteral(target) + "]",
	}
	if looksLikeCSSSelector(target) {
		q.Selector = target
	}
	if m := uidPattern.FindStringSubmatch(target); m != nil {
		if index, err := strconv.Atoi(m[1]); err == nil {
			q.UID = index
		}
	}
	for _, attribute := range []string{"aria-label", "data-testid", "name", "placeholder"} {
		q.Attributes = append(q.Attributes, "["+attribute+"="+cssString(target)+"]")
	}
	return q
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

// jsShownText finds the innermost element on the page that shows a given
// text, as a JavaScript function of the text.
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
// The text is matched as written, not as a pattern. ElementR reads its
// argument as a regular expression — so "Price (USD" was a syntax error, and a
// selector not yet on the page was a pattern that matched almost anything:
// a[href="/logout"] is the letter a followed by one of a dozen characters, and
// "Features" has one. A target is something a person typed.
const jsShownText = `(needle) => {
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

// visibleTextQuery is jsShownText on its own, for a wait on text alone.
const visibleTextQuery = `(needle) => (` + jsShownText + `)(needle)`

// plainTargetQuery takes one look at the page for a plain target, reading it
// every way a plainQuery says to, and returns the first element any reading
// names.
//
// One look is one round trip. Each reading was once a call of its own, and a
// look was eight or ten of them — fast enough on a quick machine, and on a
// slow one long enough that a look begun with time in hand ran out of budget
// before it reached the readings at the end of the list. Inside the half
// second an existence check allows, text in part was simply not found there.
// It also made a look something the page could change under.
const plainTargetQuery = `(q) => {
	const textOf = ` + jsTextOf + `;
	const first = (selector) => {
		// A selector the browser cannot parse names nothing, and says so
		// quietly: the target is somebody's words, not a selector.
		try {
			return document.querySelector(selector);
		} catch (e) {
			return null;
		}
	};

	if (q.selector) {
		const el = first(q.selector);
		if (el) {
			return el;
		}
	}

	if (q.uid >= 0) {
		const numbered = document.querySelectorAll(q.snapshot);
		if (q.uid < numbered.length) {
			return numbered[q.uid];
		}
	}

	for (const selector of q.attributes) {
		const el = first(selector);
		if (el) {
			return el;
		}
	}

	// Exact text: precise, and it does not match the containers the text
	// happens to sit inside.
	const exact = document.evaluate(q.exactText, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue;
	if (exact) {
		return exact;
	}

	// Then text in part, for buttons and links only.
	if (q.inPart) {
		const control = Array.from(document.querySelectorAll('button, a')).find((el) => textOf(el).includes(q.target));
		if (control) {
			return control;
		}
	}

	// A label names the field it is for.
	const label = Array.from(document.querySelectorAll('label')).find((el) => textOf(el) === q.target);
	if (label) {
		const id = label.getAttribute('for');
		const field = id ? document.getElementById(id) : null;
		if (field) {
			return field;
		}
	}

	// Last, text anywhere on the page: the innermost element showing it.
	if (q.inPart) {
		return (` + jsShownText + `)(q.target);
	}
	return null;
}`

// selectorParsesQuery reports whether the browser accepts a string as a CSS
// selector, without looking for anything.
const selectorParsesQuery = `(selector) => {
	try {
		document.createDocumentFragment().querySelector(selector);
		return true;
	} catch (e) {
		return false;
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
// When the browser refuses it as a selector — "Total: 5 items" has a colon and
// is not one — it is words like any others.
func findPlainTarget(page *rod.Page, target string, budget time.Duration) (*rod.Element, error) {
	query := queryFor(target)

	p := page.Timeout(budget)
	defer p.CancelTimeout()
	ctx := p.GetContext()

	// Whether the target stands as a selector is the browser's to say, and it
	// is asked once. If it cannot be asked — the page is mid-navigation — the
	// target keeps the benefit of the doubt, which is the stricter reading.
	asSelector := writtenAsSelector(target)
	if asSelector {
		if parses, err := p.Eval(selectorParsesQuery, target); err == nil && !parses.Value.Bool() {
			asSelector = false
		}
	}

	started := time.Now()
	inPartFrom := inPartAfter(budget)

	sleep := lookupSleeper()
	for {
		query.InPart = !asSelector && time.Since(started) >= inPartFrom

		// Anything but an element — the page navigating under the look, the
		// budget running out part way through it — is not an answer either
		// way, so the next look asks again.
		if el, err := lookByJS(p, plainTargetQuery, query); el != nil && err == nil {
			return actionBound(page, el), nil
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
