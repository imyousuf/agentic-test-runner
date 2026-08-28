package browser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

var base64Std = base64.StdEncoding

// htmlTagNames is the set of standard HTML5 element tag names.
var htmlTagNames = map[string]bool{
	"a": true, "abbr": true, "address": true, "area": true, "article": true,
	"aside": true, "audio": true, "b": true, "base": true, "bdi": true,
	"bdo": true, "blockquote": true, "body": true, "br": true, "button": true,
	"canvas": true, "caption": true, "cite": true, "code": true, "col": true,
	"colgroup": true, "data": true, "datalist": true, "dd": true, "del": true,
	"details": true, "dfn": true, "dialog": true, "div": true, "dl": true,
	"dt": true, "em": true, "embed": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true,
	"header": true, "hgroup": true, "hr": true, "html": true, "i": true,
	"iframe": true, "img": true, "input": true, "ins": true, "kbd": true,
	"label": true, "legend": true, "li": true, "link": true, "main": true,
	"map": true, "mark": true, "menu": true, "meta": true, "meter": true,
	"nav": true, "noscript": true, "object": true, "ol": true, "optgroup": true,
	"option": true, "output": true, "p": true, "picture": true, "pre": true,
	"progress": true, "q": true, "rp": true, "rt": true, "ruby": true,
	"s": true, "samp": true, "script": true, "search": true, "section": true,
	"select": true, "slot": true, "small": true, "source": true, "span": true,
	"strong": true, "style": true, "sub": true, "summary": true, "sup": true,
	"table": true, "tbody": true, "td": true, "template": true, "textarea": true,
	"tfoot": true, "th": true, "thead": true, "time": true, "title": true,
	"tr": true, "track": true, "u": true, "ul": true, "var": true,
	"video": true, "wbr": true,
}

// cssSelectorWithDot matches patterns like "div.class", "input.large".
var cssSelectorWithDot = regexp.MustCompile(`\w\.\w`)

// isHTMLTagName returns true if s is a standard HTML5 element tag name.
func isHTMLTagName(s string) bool {
	_, ok := htmlTagNames[strings.ToLower(s)]
	return ok
}

// looksLikeCSSSelector returns true if target appears to be a CSS selector
// rather than plain text, a UID, or an attribute value. This is called after
// the caller has already checked for #, ., and [ prefixes.
func looksLikeCSSSelector(target string) bool {
	// Contains CSS combinator operators
	if strings.ContainsAny(target, ">+~") {
		return true
	}

	// Contains pseudo-class/pseudo-element syntax
	if strings.Contains(target, ":") {
		return true
	}

	// Contains attribute selector brackets
	if strings.Contains(target, "[") {
		return true
	}

	// Contains universal selector
	if target == "*" || strings.Contains(target, "* ") || strings.Contains(target, " *") {
		return true
	}

	// Contains class selector pattern mid-string (e.g., "div.class")
	if cssSelectorWithDot.MatchString(target) {
		return true
	}

	// Single token: check if it's a known HTML tag name
	if !strings.Contains(target, " ") {
		return isHTMLTagName(target)
	}

	// Multiple space-separated tokens: CSS descendant selector if all tokens are tag names
	tokens := strings.Fields(target)
	for _, token := range tokens {
		if !isHTMLTagName(token) {
			return false
		}
	}
	return true
}

// ElementInfo contains information about an element.
type ElementInfo struct {
	UID        string            `json:"uid"`
	TagName    string            `json:"tag_name"`
	Role       string            `json:"role,omitempty"`
	Name       string            `json:"name,omitempty"`
	Text       string            `json:"text,omitempty"`
	Value      string            `json:"value,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Bounds     *ElementBounds    `json:"bounds,omitempty"`
	Visible    bool              `json:"visible"`
	Focusable  bool              `json:"focusable"`
	Children   []ElementInfo     `json:"children,omitempty"`
}

// ElementBounds contains position and size of an element.
type ElementBounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Click clicks on an element identified by target.
func (b *Browser) Click(ctx context.Context, target string, doubleClick bool) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	el, err := b.findElement(bindDeadline(page, ctx), target)
	if err != nil {
		return err
	}

	if doubleClick {
		return el.Click(proto.InputMouseButtonLeft, 2)
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// Fill types text into an element.
func (b *Browser) Fill(ctx context.Context, target, value string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	el, err := b.findElement(bindDeadline(page, ctx), target)
	if err != nil {
		return err
	}

	// Check if it's a select element
	tagName, err := el.Property("tagName")
	if err == nil && strings.ToLower(tagName.String()) == "select" {
		return el.Select([]string{value}, true, rod.SelectorTypeText)
	}

	// Clear existing content and type new value
	err = el.SelectAllText()
	if err != nil {
		return err
	}

	return el.Input(value)
}

// Hover hovers over an element.
func (b *Browser) Hover(ctx context.Context, target string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	el, err := b.findElement(bindDeadline(page, ctx), target)
	if err != nil {
		return err
	}

	return el.Hover()
}

// PressKey presses a key or key combination.
func (b *Browser) PressKey(key string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	// Convert key names to rod input keys and press
	keys := parseKeyCombo(key)
	return page.Keyboard.Type(keys...)
}

// parseKeyCombo parses a key combination string into input.Key slice.
func parseKeyCombo(combo string) []input.Key {
	var keys []input.Key
	parts := strings.Split(combo, "+")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch strings.ToLower(part) {
		case "control", "ctrl":
			keys = append(keys, input.ControlLeft)
		case "shift":
			keys = append(keys, input.ShiftLeft)
		case "alt":
			keys = append(keys, input.AltLeft)
		case "meta", "command", "cmd":
			keys = append(keys, input.MetaLeft)
		case "enter", "return":
			keys = append(keys, input.Enter)
		case "tab":
			keys = append(keys, input.Tab)
		case "escape", "esc":
			keys = append(keys, input.Escape)
		case "backspace":
			keys = append(keys, input.Backspace)
		case "delete":
			keys = append(keys, input.Delete)
		case "space":
			keys = append(keys, input.Space)
		case "arrowup", "up":
			keys = append(keys, input.ArrowUp)
		case "arrowdown", "down":
			keys = append(keys, input.ArrowDown)
		case "arrowleft", "left":
			keys = append(keys, input.ArrowLeft)
		case "arrowright", "right":
			keys = append(keys, input.ArrowRight)
		default:
			// Single character keys
			if len(part) == 1 {
				keys = append(keys, input.Key(strings.ToUpper(part)[0]))
			}
		}
	}
	return keys
}

// keyNameToRod converts common key names to rod key constants.
func keyNameToRod(name string) rune {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "enter", "return":
		return '\r'
	case "tab":
		return '\t'
	case "escape", "esc":
		return '\x1b'
	case "backspace":
		return '\b'
	case "space":
		return ' '
	case "arrowup", "up":
		return rune(0xE013)
	case "arrowdown", "down":
		return rune(0xE015)
	case "arrowleft", "left":
		return rune(0xE012)
	case "arrowright", "right":
		return rune(0xE014)
	default:
		if len(name) == 1 {
			return rune(name[0])
		}
		return rune(name[0])
	}
}

// Drag drags an element to another element.
func (b *Browser) Drag(ctx context.Context, fromTarget, toTarget string) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	fromEl, err := b.findElement(bindDeadline(page, ctx), fromTarget)
	if err != nil {
		return fmt.Errorf("source element not found: %w", err)
	}

	toEl, err := b.findElement(bindDeadline(page, ctx), toTarget)
	if err != nil {
		return fmt.Errorf("target element not found: %w", err)
	}

	// Use rod's built-in drag functionality via JavaScript
	_, err = page.Eval(`(from, to) => {
		const fromRect = from.getBoundingClientRect();
		const toRect = to.getBoundingClientRect();

		const fromX = fromRect.left + fromRect.width / 2;
		const fromY = fromRect.top + fromRect.height / 2;
		const toX = toRect.left + toRect.width / 2;
		const toY = toRect.top + toRect.height / 2;

		const dataTransfer = new DataTransfer();

		from.dispatchEvent(new DragEvent('dragstart', {
			bubbles: true, clientX: fromX, clientY: fromY, dataTransfer
		}));

		to.dispatchEvent(new DragEvent('dragover', {
			bubbles: true, clientX: toX, clientY: toY, dataTransfer
		}));

		to.dispatchEvent(new DragEvent('drop', {
			bubbles: true, clientX: toX, clientY: toY, dataTransfer
		}));

		from.dispatchEvent(new DragEvent('dragend', {
			bubbles: true, clientX: toX, clientY: toY, dataTransfer
		}));
	}`, fromEl, toEl)

	return err
}

// WaitForElement waits for an element to appear.
func (b *Browser) WaitForElement(ctx context.Context, target string, timeout time.Duration) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	page = page.Timeout(timeout)
	_, err = b.findElementWithin(page, target, timeout)
	return err
}

// WaitForElementVisible waits for an element to appear and be visible.
// It retries until the element is both present and visible, or the timeout expires.
func (b *Browser) WaitForElementVisible(ctx context.Context, target string, timeout time.Duration) error {
	page, err := b.CurrentPage()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	poll := 200 * time.Millisecond

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("element not visible within timeout: %s", target)
		}
		attemptTimeout := poll
		if attemptTimeout > remaining {
			attemptTimeout = remaining
		}

		el, err := tryFind(page, attemptTimeout, func(p *rod.Page) (*rod.Element, error) {
			return b.findElement(p, target)
		})
		if err == nil {
			visible, vErr := el.Visible()
			if vErr == nil && visible {
				return nil
			}
		}
		time.Sleep(poll)
	}
}

// Snapshot returns the accessibility tree of the current page.
func (b *Browser) Snapshot(verbose bool) ([]ElementInfo, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	// Get all interactive elements
	elements, err := page.Elements("button, input, select, textarea, a, [role], [aria-label], [data-testid]")
	if err != nil {
		return nil, err
	}

	var infos []ElementInfo
	for i, el := range elements {
		info := ElementInfo{
			UID:        fmt.Sprintf("e%d", i),
			Attributes: make(map[string]string),
		}

		// Get tag name
		if tagName, err := el.Property("tagName"); err == nil {
			info.TagName = strings.ToLower(tagName.String())
		}

		// Get role
		if role, err := el.Attribute("role"); err == nil && role != nil {
			info.Role = *role
		}

		// Get aria-label
		if label, err := el.Attribute("aria-label"); err == nil && label != nil {
			info.Name = *label
		}

		// Get text content
		if text, err := el.Text(); err == nil {
			info.Text = strings.TrimSpace(text)
			if info.Text != "" && info.Name == "" {
				info.Name = info.Text
			}
		}

		// Get value for inputs
		if val, err := el.Property("value"); err == nil {
			info.Value = val.String()
		}

		// Get visibility
		if visible, err := el.Visible(); err == nil {
			info.Visible = visible
		}

		// Get attributes if verbose
		if verbose {
			for _, attr := range []string{"id", "class", "name", "type", "placeholder", "data-testid", "href"} {
				if val, err := el.Attribute(attr); err == nil && val != nil && *val != "" {
					info.Attributes[attr] = *val
				}
			}

			// Get bounds
			if shape, err := el.Shape(); err == nil {
				if box := shape.Box(); box != nil {
					info.Bounds = &ElementBounds{
						X:      box.X,
						Y:      box.Y,
						Width:  box.Width,
						Height: box.Height,
					}
				}
			}
		}

		infos = append(infos, info)
	}

	return infos, nil
}

// findElement finds an element using various strategies.
// tryFind attempts to find an element using the given function with a per-attempt timeout.
// elementActionTimeout bounds what a caller does with an element after
// finding it — reading a property, selecting text, typing, clicking.
// Generous, because rod waits for the element to become interactable and the
// page may still be settling.
const elementActionTimeout = 30 * time.Second

// How long a lookup may spend waiting for a target to appear.
//
// The floor is what a caller gets when it has set no deadline of its own; the
// ceiling stops an ambient deadline — a behaviour step's 60s, say — from being
// spent entirely on one selector that is never going to match.
const (
	minElementSearchTimeout = 3 * time.Second
	maxElementSearchTimeout = 15 * time.Second
)

// searchTimeout is the budget a lookup gets on this page.
//
// It comes from the caller's own deadline, because a fixed budget is wrong in
// both directions: a script step with 60s to spend used to give up on a slow
// page after 3s, and an existence check that wanted an answer in 500ms had no
// way to ask for one. A page carries whatever deadline its caller bound to it,
// so that is what this reads.
//
// A caller asking for less than the floor gets exactly what it asked for —
// that is a deliberately impatient lookup, not an under-specified one.
func searchTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return minElementSearchTimeout
	}
	switch remaining := time.Until(deadline); {
	case remaining <= 0:
		// Already past the deadline. Let the lookup run and fail on the
		// context rather than inventing a budget the caller does not have.
		return time.Millisecond
	case remaining < minElementSearchTimeout:
		return remaining
	case remaining > maxElementSearchTimeout:
		return maxElementSearchTimeout
	default:
		return remaining
	}
}

// bindDeadline returns a page bound to the caller's context, so that a lookup
// made through it can see how long the caller is prepared to wait. Without
// this every action method takes a ctx and then drops it on the floor.
func bindDeadline(page *rod.Page, ctx context.Context) *rod.Page {
	if ctx == nil {
		return page
	}
	if _, ok := ctx.Deadline(); !ok {
		return page
	}
	return page.Context(ctx)
}

// asNotFound reports a failed lookup as ErrElementNotFound.
//
// These branches return early, so they never reach the ErrElementNotFound at
// the end of the fallback chain. rod retries a selector until its context
// expires and then surfaces the context error, which means "this selector
// never matched" arrives as "context deadline exceeded". Callers classifying
// a failure need to tell a vanished element apart from a slow one, and for a
// lookup those are the same event — so only the deadline is translated, and
// any other error is passed through as itself.
func asNotFound(target string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s", ErrElementNotFound, target)
	}
	return err
}

// ErrElementNotFound is returned when no strategy locates a target. Callers
// that need to tell "the page changed shape" apart from "the operation
// failed" — the script runtime's failure taxonomy, for one — match on this
// with errors.Is rather than on message text.
var ErrElementNotFound = errors.New("element not found")

func tryFind(page *rod.Page, timeout time.Duration, fn func(*rod.Page) (*rod.Element, error)) (*rod.Element, error) {
	el, err := fn(page.Timeout(timeout))
	if err != nil {
		return nil, err
	}

	// Rebind the element to a fresh, action-sized deadline.
	//
	// page.Timeout(d) hands fn a page whose context expires after d, and an
	// element found through it inherits that context. That timeout is meant
	// to bound the *search*; left in place it follows the element into
	// whatever the caller does next. Fill, for instance, then makes three
	// more CDP calls (Property, SelectAllText, Input) against whatever is
	// left of a 500ms UID budget — which is enough when the connection is
	// idle and not enough when anything else is talking to the same target,
	// so fills fail with "context deadline exceeded" only under load.
	//
	// Rebinding to the page's own context would fix that but leave the action
	// unbounded, and rod waits for interactability: an element that never
	// becomes interactable would block the caller forever. So the action gets
	// its own budget instead.
	return el.Context(page.Timeout(elementActionTimeout).GetContext()), nil
}

func (b *Browser) findElement(page *rod.Page, target string) (*rod.Element, error) {
	return b.findElementWithin(page, target, searchTimeout(page.GetContext()))
}

// findElementWithin is findElement with the budget stated outright, for
// callers that have been given an explicit timeout to honour.
func (b *Browser) findElementWithin(page *rod.Page, target string, budget time.Duration) (*rod.Element, error) {
	// Each strategy in the fallback chain gets a slice of the budget rather
	// than a fixed 500ms, so that a generous caller is generous all the way
	// down. At the default budget this works out at the same 500ms as before.
	perAttempt := budget / 6
	if perAttempt < 500*time.Millisecond {
		perAttempt = 500 * time.Millisecond
	}
	if perAttempt > minElementSearchTimeout {
		perAttempt = minElementSearchTimeout
	}

	// XPath selectors
	if strings.HasPrefix(target, "//") {
		el, err := tryFind(page, budget, func(p *rod.Page) (*rod.Element, error) {
			return p.ElementX(target)
		})
		return el, asNotFound(target, err)
	}

	// :has-text() is not CSS, so it has to be resolved before querySelector
	// ever sees it.
	if base, want, ok := splitHasText(target); ok {
		el, err := tryFind(page, budget, func(p *rod.Page) (*rod.Element, error) {
			return resolveHasText(p, base, want)
		})
		return el, asNotFound(target, err)
	}

	// CSS selectors: unambiguous prefixes or structural analysis
	if strings.HasPrefix(target, "#") || strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "[") || looksLikeCSSSelector(target) {
		el, err := tryFind(page, budget, func(p *rod.Page) (*rod.Element, error) {
			return p.Element(target)
		})
		if err != nil && unambiguousCSS(target) {
			// Only a selector the caller clearly meant as CSS is reported as
			// malformed. A guessed one falls through to the strategies below,
			// so prose containing a colon is still matched as text.
			if invalid := invalidSelector(target, err); errors.Is(invalid, ErrInvalidSelector) {
				return nil, invalid
			}
		}
		if err == nil || unambiguousCSS(target) {
			return el, asNotFound(target, err)
		}
	}

	// Try by UID (e.g., "e0", "e1")
	if strings.HasPrefix(target, "e") {
		if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
			elements, err := p.Elements("button, input, select, textarea, a, [role], [aria-label], [data-testid]")
			if err != nil {
				return nil, err
			}
			var idx int
			if _, err := fmt.Sscanf(target, "e%d", &idx); err == nil && idx >= 0 && idx < len(elements) {
				return elements[idx], nil
			}
			return nil, fmt.Errorf("UID %s not found", target)
		}); err == nil {
			return el, nil
		}
	}

	// Try by aria-label
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.Element(fmt.Sprintf(`[aria-label="%s"]`, target))
	}); err == nil {
		return el, nil
	}

	// Try by data-testid
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.Element(fmt.Sprintf(`[data-testid="%s"]`, target))
	}); err == nil {
		return el, nil
	}

	// Try by name attribute
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.Element(fmt.Sprintf(`[name="%s"]`, target))
	}); err == nil {
		return el, nil
	}

	// Try by placeholder
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.Element(fmt.Sprintf(`[placeholder="%s"]`, target))
	}); err == nil {
		return el, nil
	}

	// Try by XPath exact text match — precise, avoids matching containers
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.ElementX(fmt.Sprintf(`//*[normalize-space(text())="%s"]`, target))
	}); err == nil {
		return el, nil
	}

	// Try by text content for buttons and links only (not container roles)
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.ElementR("button, a", target)
	}); err == nil {
		return el, nil
	}

	// Try by label text (for form fields associated via for/id)
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		// Find label with matching text, then get its associated input via "for" attribute
		label, err := p.ElementR("label", "^"+regexp.QuoteMeta(target)+"$")
		if err != nil {
			return nil, err
		}
		forAttr, err := label.Attribute("for")
		if err != nil || forAttr == nil || *forAttr == "" {
			return nil, fmt.Errorf("label has no for attribute")
		}
		return p.Element("#" + *forAttr)
	}); err == nil {
		return el, nil
	}

	// Final attempt: any element containing the text via regex
	if el, err := tryFind(page, perAttempt, func(p *rod.Page) (*rod.Element, error) {
		return p.ElementR("*", target)
	}); err == nil {
		return el, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrElementNotFound, target)
}

// GetElementScreenshot captures a screenshot of a specific element.
func (b *Browser) GetElementScreenshot(target string) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	defer b.hideHud(page)()

	el, err := b.findElement(page, target)
	if err != nil {
		return nil, err
	}

	return el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
}

// findElementByCSS finds an element using a CSS selector directly, without
// the multi-strategy fallback chain. Use this when the caller knows the
// target is explicitly a CSS selector.
//
// Budget and failure reporting match findElement deliberately. This used to
// spend a fixed three seconds whatever the caller allowed, and to return rod's
// raw deadline error — which classify() reads as KindEnvironment, meaning
// retryable but not repairable. So a renamed id behind atr.text was retried
// until the run gave up instead of being handed to the repair path, while the
// identical rename behind atr.click was repaired.
func (b *Browser) findElementByCSS(page *rod.Page, selector string) (*rod.Element, error) {
	if base, want, ok := splitHasText(selector); ok {
		el, err := tryFind(page, searchTimeout(page.GetContext()), func(p *rod.Page) (*rod.Element, error) {
			return resolveHasText(p, base, want)
		})
		return el, asNotFound(selector, err)
	}

	el, err := tryFind(page, searchTimeout(page.GetContext()), func(p *rod.Page) (*rod.Element, error) {
		return p.Element(selector)
	})
	if invalid := invalidSelector(selector, err); errors.Is(invalid, ErrInvalidSelector) {
		return nil, invalid
	}
	return el, asNotFound(selector, err)
}

// GetElementScreenshotByCSS captures a screenshot of a specific element
// identified by CSS selector. Unlike GetElementScreenshot, this does not
// attempt fallback strategies (UID, text, aria-label, etc.).
func (b *Browser) GetElementScreenshotByCSS(selector string) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	defer b.hideHud(page)()

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return nil, err
	}

	return el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
}

// ScrollResult contains the scroll state after a scroll operation.
type ScrollResult struct {
	ScrollTop    int `json:"scrollTop"`
	ScrollLeft   int `json:"scrollLeft"`
	ScrollHeight int `json:"scrollHeight"`
	ScrollWidth  int `json:"scrollWidth"`
	ClientHeight int `json:"clientHeight"`
	ClientWidth  int `json:"clientWidth"`
}

// ScrollElement scrolls within an element that has overflow scroll/auto.
// For body/html selectors, delegates to window.scrollTo() since element-level
// scrollTo() doesn't work for page-level scrolling.
func (b *Browser) ScrollElement(selector string, x, y int, toBottom, toTop bool) (*ScrollResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	selectorLower := strings.ToLower(strings.TrimSpace(selector))
	isPageScroll := selectorLower == "body" || selectorLower == "html"

	if isPageScroll {
		// Use window.scrollTo for page-level scrolling
		js := fmt.Sprintf(`() => {
			const doc = document.documentElement;
			if (%v) {
				window.scrollTo(0, doc.scrollHeight);
			} else if (%v) {
				window.scrollTo(0, 0);
			} else {
				window.scrollTo(%d, %d);
			}
			return {
				scrollTop: Math.round(window.scrollY),
				scrollLeft: Math.round(window.scrollX),
				scrollHeight: doc.scrollHeight,
				scrollWidth: doc.scrollWidth,
				clientHeight: window.innerHeight,
				clientWidth: window.innerWidth
			};
		}`, toBottom, toTop, x, y)

		result, err := page.Eval(js)
		if err != nil {
			return nil, fmt.Errorf("scroll failed: %w", err)
		}

		raw := result.Value.Map()
		return &ScrollResult{
			ScrollTop:    int(raw["scrollTop"].Num()),
			ScrollLeft:   int(raw["scrollLeft"].Num()),
			ScrollHeight: int(raw["scrollHeight"].Num()),
			ScrollWidth:  int(raw["scrollWidth"].Num()),
			ClientHeight: int(raw["clientHeight"].Num()),
			ClientWidth:  int(raw["clientWidth"].Num()),
		}, nil
	}

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return nil, err
	}

	js := fmt.Sprintf(`function() {
		if (%v) {
			this.scrollTo(0, this.scrollHeight);
		} else if (%v) {
			this.scrollTo(0, 0);
		} else {
			this.scrollTo(%d, %d);
		}
		return {
			scrollTop: Math.round(this.scrollTop),
			scrollLeft: Math.round(this.scrollLeft),
			scrollHeight: this.scrollHeight,
			scrollWidth: this.scrollWidth,
			clientHeight: this.clientHeight,
			clientWidth: this.clientWidth
		};
	}`, toBottom, toTop, x, y)

	result, err := el.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("scroll failed: %w", err)
	}

	raw := result.Value.Map()
	return &ScrollResult{
		ScrollTop:    int(raw["scrollTop"].Num()),
		ScrollLeft:   int(raw["scrollLeft"].Num()),
		ScrollHeight: int(raw["scrollHeight"].Num()),
		ScrollWidth:  int(raw["scrollWidth"].Num()),
		ClientHeight: int(raw["clientHeight"].Num()),
		ClientWidth:  int(raw["clientWidth"].Num()),
	}, nil
}

// StyleDiffEntry represents a single CSS property comparison.
type StyleDiffEntry struct {
	Current string `json:"current"`
	Target  string `json:"target"`
}

// StyleDiffResult contains the computed style diff between two elements across pages.
type StyleDiffResult struct {
	Selector      string                    `json:"selector"`
	Matches       map[string]string         `json:"matches"`
	Mismatches    map[string]StyleDiffEntry `json:"mismatches"`
	MatchCount    int                       `json:"matchCount"`
	MismatchCount int                       `json:"mismatchCount"`
	Score         float64                   `json:"score"`
}

// GetComputedStylesDiff compares computed styles of an element on the current page
// against an element on another page (identified by page index).
func (b *Browser) GetComputedStylesDiff(selector string, againstPageIndex int, properties []string, selectorTarget string) (*StyleDiffResult, error) {
	// Lock page switching for the duration of this compound operation
	b.pageSwitchMu.Lock()
	defer b.pageSwitchMu.Unlock()

	// Remember current page
	b.mu.RLock()
	currentIdx := b.current
	b.mu.RUnlock()

	// Get styles from current page
	sourceStyles, err := b.GetComputedStyles(selector, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to get source styles: %w", err)
	}

	// Switch to target page
	if err := b.SelectPage(againstPageIndex); err != nil {
		return nil, fmt.Errorf("failed to select target page %d: %w", againstPageIndex, err)
	}

	// Get styles from target page
	targetSelector := selector
	if selectorTarget != "" {
		targetSelector = selectorTarget
	}
	targetStyles, err := b.GetComputedStyles(targetSelector, properties)

	// Always switch back to original page
	switchBackErr := b.SelectPage(currentIdx)

	if err != nil {
		return nil, fmt.Errorf("failed to get target styles: %w", err)
	}
	if switchBackErr != nil {
		return nil, fmt.Errorf("failed to switch back to original page: %w", switchBackErr)
	}

	// Compute diff
	result := &StyleDiffResult{
		Selector:   selector,
		Matches:    make(map[string]string),
		Mismatches: make(map[string]StyleDiffEntry),
	}

	allProps := make(map[string]bool)
	for k := range sourceStyles {
		allProps[k] = true
	}
	for k := range targetStyles {
		allProps[k] = true
	}

	for prop := range allProps {
		src := sourceStyles[prop]
		tgt := targetStyles[prop]
		if src == tgt {
			result.Matches[prop] = src
			result.MatchCount++
		} else {
			result.Mismatches[prop] = StyleDiffEntry{Current: src, Target: tgt}
			result.MismatchCount++
		}
	}

	total := result.MatchCount + result.MismatchCount
	if total > 0 {
		result.Score = float64(result.MatchCount) / float64(total) * 100
	}

	return result, nil
}

// ElementScreenshotResult contains the screenshot data or error for a single element.
type ElementScreenshotResult struct {
	Index int    `json:"index"`
	Data  []byte `json:"-"`
	Error string `json:"error,omitempty"`
}

// GetMultipleElementScreenshots captures screenshots of all elements matching a CSS selector.
// It saves each screenshot as it completes, skipping elements that fail or timeout.
// perElementTimeout of 0 uses a default of 30 seconds per element.
func (b *Browser) GetMultipleElementScreenshots(selector string, perElementTimeout ...time.Duration) ([]ElementScreenshotResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	elements, err := elementsMatching(page.Timeout(3*time.Second), selector)
	if err != nil {
		return nil, fmt.Errorf("failed to find elements: %w", err)
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("no elements found for selector: %s", selector)
	}

	timeout := 30 * time.Second
	if len(perElementTimeout) > 0 && perElementTimeout[0] > 0 {
		timeout = perElementTimeout[0]
	}

	var results []ElementScreenshotResult
	for i, el := range elements {
		result := ElementScreenshotResult{Index: i}

		// Give each capture its own budget.
		//
		// The elements above came from a page bounded to 3 seconds for the
		// *lookup*, and every element carries that context with it. Left
		// alone, all the captures share one 3-second window between them, so
		// a page with several images fails partway through with "context
		// deadline exceeded" — and the per-element timeout below never
		// applies, because it only bounds the wait on the channel, not the
		// CDP call underneath.
		el := el.Context(page.Timeout(timeout).GetContext())

		// Use a goroutine with timeout for each element screenshot
		type screenshotResult struct {
			data []byte
			err  error
		}
		ch := make(chan screenshotResult, 1)
		go func(element *rod.Element) {
			data, err := element.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
			ch <- screenshotResult{data, err}
		}(el)

		select {
		case sr := <-ch:
			if sr.err != nil {
				result.Error = fmt.Sprintf("screenshot failed: %v", sr.err)
			} else {
				result.Data = sr.data
			}
		case <-time.After(timeout):
			result.Error = "screenshot timed out"
		}

		results = append(results, result)
	}

	return results, nil
}

// GetElementFullHeightScreenshot captures a screenshot of an element expanded to its full scroll height.
// Useful for elements with overflow:scroll/auto that have content beyond the visible area.
// Temporarily mutates the element's CSS to expand it, takes the screenshot, then restores.
func (b *Browser) GetElementFullHeightScreenshot(selector string) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return nil, err
	}

	// Store original styles and expand to full height
	_, err = el.Eval(`function() {
		this.__atr_orig_overflow = this.style.overflow;
		this.__atr_orig_height = this.style.height;
		this.__atr_orig_maxHeight = this.style.maxHeight;
		this.style.overflow = 'visible';
		this.style.height = this.scrollHeight + 'px';
		this.style.maxHeight = 'none';
	}`)
	if err != nil {
		return nil, fmt.Errorf("failed to expand element: %w", err)
	}

	// Take screenshot
	data, screenshotErr := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)

	// Always restore original styles
	_, restoreErr := el.Eval(`function() {
		this.style.overflow = this.__atr_orig_overflow;
		this.style.height = this.__atr_orig_height;
		this.style.maxHeight = this.__atr_orig_maxHeight;
		delete this.__atr_orig_overflow;
		delete this.__atr_orig_height;
		delete this.__atr_orig_maxHeight;
	}`)

	if screenshotErr != nil {
		return nil, fmt.Errorf("screenshot failed: %w", screenshotErr)
	}
	if restoreErr != nil {
		return nil, fmt.Errorf("failed to restore element styles: %w", restoreErr)
	}

	return data, nil
}

// TextGroup represents a group of text content with its HTML tag context.
type TextGroup struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
	Href string `json:"href,omitempty"`
}

// TextResult contains the extracted text content from an element.
type TextResult struct {
	Selector string      `json:"selector"`
	Mode     string      `json:"mode"`
	Groups   []TextGroup `json:"groups"`
}

// GetTextContent extracts structured text content from an element.
// mode can be: "structured" (default), "flat", "links", "headings"
func (b *Browser) GetTextContent(selector string, mode string) (*TextResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return nil, err
	}

	if mode == "" {
		mode = "structured"
	}

	result, err := el.Eval(`function(m) {
		if (m === "flat") {
			return [{tag: "text", text: this.textContent.trim()}];
		} else if (m === "links") {
			return Array.from(this.querySelectorAll("a")).map(a => ({
				tag: "a", text: a.textContent.trim(), href: a.href
			}));
		} else if (m === "headings") {
			return Array.from(this.querySelectorAll("h1,h2,h3,h4,h5,h6")).map(h => ({
				tag: h.tagName.toLowerCase(), text: h.textContent.trim()
			}));
		} else {
			const groups = [];
			function walk(node) {
				if (node.nodeType === Node.TEXT_NODE) {
					const text = node.textContent.trim();
					if (text) {
						const parent = node.parentElement;
						const tag = parent ? parent.tagName.toLowerCase() : "text";
						const href = (parent && parent.tagName === "A") ? parent.href : undefined;
						groups.push({tag: tag, text: text, href: href});
					}
					return;
				}
				if (node.nodeType === Node.ELEMENT_NODE) {
					for (const child of node.childNodes) walk(child);
				}
			}
			walk(this);
			return groups;
		}
	}`, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to extract text: %w", err)
	}

	tr := &TextResult{Selector: selector, Mode: mode}
	for _, item := range result.Value.Arr() {
		g := TextGroup{
			Tag:  item.Get("tag").Str(),
			Text: item.Get("text").Str(),
		}
		if href := item.Get("href").Str(); href != "" {
			g.Href = href
		}
		tr.Groups = append(tr.Groups, g)
	}
	return tr, nil
}

// FontCheckResult contains the result of checking if a font is loaded and rendering.
type FontCheckResult struct {
	Family   string `json:"family"`
	Declared bool   `json:"declared"`
	Loaded   bool   `json:"loaded"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Fallback string `json:"fallback,omitempty"`
}

// CheckFont checks if a font family is actually loaded and rendering in the browser.
// Uses the CSS Font Loading API (document.fonts) to determine real load status.
func (b *Browser) CheckFont(family string) (*FontCheckResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	result, err := page.Eval(`(family) => {
		const res = {
			family: family,
			declared: false,
			loaded: false,
			status: "not_found",
			reason: "",
			fallback: ""
		};

		// Check document.fonts for @font-face declarations
		const fonts = [...document.fonts.values()];
		const matching = fonts.filter(f => f.family.replace(/['"]/g, '') === family);
		if (matching.length > 0) {
			res.declared = true;
			const statuses = matching.map(f => f.status);
			if (statuses.includes("loaded")) {
				res.loaded = true;
				res.status = "loaded";
			} else if (statuses.includes("loading")) {
				res.status = "loading";
				res.reason = "font is still loading";
			} else if (statuses.includes("error")) {
				res.status = "error";
				res.reason = "font failed to load (check CORS or URL)";
			} else {
				res.status = "unloaded";
				res.reason = "font declared but not yet requested";
			}
		} else {
			// Not in @font-face declarations — check if it's used in the page
			// and detect via canvas rendering difference
			const canvas = document.createElement('canvas');
			const ctx = canvas.getContext('2d');
			const testStr = 'mmmmmmmmmmlli';
			ctx.font = '72px monospace';
			const monoWidth = ctx.measureText(testStr).width;
			ctx.font = '72px "' + family + '", monospace';
			const testWidth = ctx.measureText(testStr).width;
			if (testWidth !== monoWidth) {
				// The font rendered differently from fallback — it's available
				res.declared = true;
				res.loaded = true;
				res.status = "loaded";
				res.reason = "system font or already cached";
			}
		}

		// Find fallback: look for elements using this font family
		if (!res.loaded) {
			const allEls = document.querySelectorAll('*');
			for (const el of allEls) {
				const cs = window.getComputedStyle(el);
				if (cs.fontFamily.includes(family)) {
					const families = cs.fontFamily.split(',').map(f => f.trim().replace(/['"]/g, ''));
					const idx = families.findIndex(f => f === family);
					if (idx >= 0 && idx < families.length - 1) {
						res.fallback = families.slice(idx + 1).join(', ');
					}
					break;
				}
			}
		}

		return res;
	}`, family)
	if err != nil {
		return nil, fmt.Errorf("font check failed: %w", err)
	}

	raw := result.Value.Map()
	return &FontCheckResult{
		Family:   raw["family"].Str(),
		Declared: raw["declared"].Bool(),
		Loaded:   raw["loaded"].Bool(),
		Status:   raw["status"].Str(),
		Reason:   raw["reason"].Str(),
		Fallback: raw["fallback"].Str(),
	}, nil
}

// DownloadedImage contains info about a downloaded or screenshotted image.
type DownloadedImage struct {
	Index  int    `json:"index"`
	Source string `json:"source,omitempty"` // img src URL
	Method string `json:"method"`           // "download" or "screenshot"
	Data   []byte `json:"-"`
	Error  string `json:"error,omitempty"`
}

// DownloadImages downloads images found within elements matching a CSS selector.
// It finds all <img> elements within scope and fetches their src via the browser.
// If fallbackScreenshot is true and no <img> tags are found, it screenshots each matching element.
func (b *Browser) DownloadImages(selector string, fallbackScreenshot bool) ([]DownloadedImage, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	// First, try to find <img> elements within the selector scope
	imgResult, err := page.Eval(`(sel) => {
		const container = document.querySelector(sel);
		if (!container) return { error: "selector not found" };
		const imgs = container.querySelectorAll('img');
		const results = [];
		for (const img of imgs) {
			if (img.src) {
				results.push({ src: img.src, alt: img.alt || '' });
			}
		}
		return { imgs: results, containerCount: container.querySelectorAll(sel === container.tagName.toLowerCase() ? '*' : sel).length };
	}`, selector)
	if err != nil {
		return nil, fmt.Errorf("failed to find images: %w", err)
	}

	raw := imgResult.Value.Map()
	if errVal := raw["error"]; errVal.Val() != nil {
		return nil, fmt.Errorf("%s: %s", errVal.Str(), selector)
	}

	imgs := raw["imgs"].Arr()

	if len(imgs) > 0 {
		// Download each image via browser fetch (avoids CORS issues)
		var results []DownloadedImage
		for i, img := range imgs {
			src := img.Get("src").Str()
			result := DownloadedImage{
				Index:  i,
				Source: src,
				Method: "download",
			}

			// Fetch via browser to bypass CORS
			fetchResult, err := page.Eval(`async (url) => {
				try {
					const resp = await fetch(url);
					if (!resp.ok) return { error: resp.status + ' ' + resp.statusText };
					const blob = await resp.blob();
					const reader = new FileReader();
					return new Promise((resolve) => {
						reader.onload = () => resolve({ data: reader.result });
						reader.onerror = () => resolve({ error: 'read failed' });
						reader.readAsDataURL(blob);
					});
				} catch(e) {
					return { error: e.message };
				}
			}`, src)
			if err != nil {
				result.Error = fmt.Sprintf("fetch failed: %v", err)
				results = append(results, result)
				continue
			}

			fetchMap := fetchResult.Value.Map()
			if errVal := fetchMap["error"]; errVal.Val() != nil {
				errStr := errVal.Str()
				result.Error = errStr
				results = append(results, result)
				continue
			}

			// Parse data URL to raw bytes
			dataURL := fetchMap["data"].Str()
			if commaIdx := strings.Index(dataURL, ","); commaIdx >= 0 {
				import64 := dataURL[commaIdx+1:]
				// Decode base64
				decoded, err := decodeBase64(import64)
				if err != nil {
					result.Error = fmt.Sprintf("decode failed: %v", err)
				} else {
					result.Data = decoded
				}
			} else {
				result.Error = "invalid data URL"
			}

			results = append(results, result)
		}
		return results, nil
	}

	// No <img> tags found
	if !fallbackScreenshot {
		return nil, fmt.Errorf("no <img> elements found within selector: %s", selector)
	}

	// Fallback: screenshot matching elements
	elements, err := elementsMatching(page.Timeout(3*time.Second), selector)
	if err != nil {
		return nil, fmt.Errorf("failed to find elements for screenshot fallback: %w", err)
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("no elements found for selector: %s", selector)
	}

	var results []DownloadedImage
	for i, el := range elements {
		result := DownloadedImage{
			Index:  i,
			Method: "screenshot",
		}
		data, err := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
		if err != nil {
			result.Error = fmt.Sprintf("screenshot failed: %v", err)
		} else {
			result.Data = data
		}
		results = append(results, result)
	}
	return results, nil
}

// decodeBase64 decodes a base64-encoded string (standard or URL-safe encoding).
func decodeBase64(s string) ([]byte, error) {
	return base64Std.DecodeString(s)
}

// ComputedStylesEntry contains computed styles for a single element in a multi-element query.
type ComputedStylesEntry struct {
	Index  int               `json:"index"`
	Text   string            `json:"text"`
	Styles map[string]string `json:"styles"`
}

// GetMultipleComputedStyles returns computed styles for all elements matching a CSS selector.
func (b *Browser) GetMultipleComputedStyles(selector string, properties []string) ([]ComputedStylesEntry, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	elements, err := elementsMatching(page.Timeout(3*time.Second), selector)
	if err != nil {
		return nil, fmt.Errorf("failed to find elements: %w", err)
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("no elements found for selector: %s", selector)
	}

	if properties == nil {
		properties = []string{}
	}

	var results []ComputedStylesEntry
	for i, el := range elements {
		entry := ComputedStylesEntry{Index: i}

		// Get text content
		if text, err := el.Text(); err == nil {
			t := strings.TrimSpace(text)
			if len(t) > 100 {
				t = t[:100] + "..."
			}
			entry.Text = t
		}

		result, err := el.Eval(`function(props) {
			const cs = window.getComputedStyle(this);
			const out = {};
			if (props && props.length > 0) {
				props.forEach(p => {
					const v = cs[p] || cs.getPropertyValue(p);
					if (v !== undefined && v !== "") out[p] = v;
				});
			} else {
				const defaults = [
					"fontSize", "fontWeight", "fontFamily", "lineHeight", "letterSpacing",
					"color", "backgroundColor", "display", "textAlign", "padding", "margin",
					"borderRadius", "width", "height", "position", "opacity",
					"textDecoration", "textTransform", "fontStyle",
					"fontFeatureSettings", "textRendering", "webkitFontSmoothing", "fontKerning"
				];
				defaults.forEach(p => {
					const v = cs[p];
					if (v !== undefined && v !== "") out[p] = v;
				});
			}
			return out;
		}`, properties)
		if err != nil {
			entry.Styles = map[string]string{"error": err.Error()}
			results = append(results, entry)
			continue
		}

		entry.Styles = make(map[string]string)
		for k, v := range result.Value.Map() {
			entry.Styles[k] = v.Str()
		}
		results = append(results, entry)
	}

	return results, nil
}

// BatchStyleResult contains the result for a single selector in a batch query.
type BatchStyleResult struct {
	Selector string            `json:"selector"`
	Matched  bool              `json:"matched"`
	Element  string            `json:"element,omitempty"`
	Styles   map[string]string `json:"styles"`
}

// GetBatchComputedStyles returns computed styles for each selector independently.
// If a selector matches nothing, Matched is false (no error).
func (b *Browser) GetBatchComputedStyles(selectors []string, properties []string) ([]BatchStyleResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	if properties == nil {
		properties = []string{}
	}

	var results []BatchStyleResult
	for _, sel := range selectors {
		result := BatchStyleResult{Selector: sel}

		el, err := b.findElementByCSS(page, sel)
		if err != nil {
			result.Matched = false
			results = append(results, result)
			continue
		}

		result.Matched = true

		// Get element description
		if desc, err := el.Eval(`function() { return '<' + this.tagName.toLowerCase() + (this.className ? ' class="' + this.className + '"' : '') + '>'; }`); err == nil {
			result.Element = desc.Value.Str()
		}

		styleResult, err := el.Eval(`function(props) {
			const cs = window.getComputedStyle(this);
			const out = {};
			if (props && props.length > 0) {
				props.forEach(p => {
					const v = cs[p] || cs.getPropertyValue(p);
					if (v !== undefined && v !== "") out[p] = v;
				});
			} else {
				const defaults = [
					"fontSize", "fontWeight", "fontFamily", "lineHeight", "letterSpacing",
					"color", "backgroundColor", "display", "textAlign", "padding", "margin",
					"borderRadius", "width", "height", "position", "opacity",
					"textDecoration", "textTransform", "fontStyle",
					"fontFeatureSettings", "textRendering", "webkitFontSmoothing", "fontKerning"
				];
				defaults.forEach(p => {
					const v = cs[p];
					if (v !== undefined && v !== "") out[p] = v;
				});
			}
			return out;
		}`, properties)
		if err != nil {
			result.Matched = true
			result.Styles = map[string]string{"error": err.Error()}
			results = append(results, result)
			continue
		}

		result.Styles = make(map[string]string)
		for k, v := range styleResult.Value.Map() {
			result.Styles[k] = v.Str()
		}
		results = append(results, result)
	}

	return results, nil
}

// BatchDiffDetail represents a single mismatched property in a batch diff.
type BatchDiffDetail struct {
	Property string `json:"property"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// BatchDiffResult contains the diff result for a single selector in a batch diff.
type BatchDiffResult struct {
	Selector   string            `json:"selector"`
	Matched    bool              `json:"matched"`
	Score      float64           `json:"score"`
	Matches    int               `json:"matches"`
	Mismatches int               `json:"mismatches"`
	Details    []BatchDiffDetail `json:"details"`
}

// BatchDiffOutput contains all batch diff results with an overall score.
type BatchDiffOutput struct {
	Results      []BatchDiffResult `json:"results"`
	OverallScore float64           `json:"overall_score"`
}

// GetBatchComputedStylesDiff compares styles for multiple selectors between current and target page.
func (b *Browser) GetBatchComputedStylesDiff(selectors []string, againstPageIndex int, properties []string, selectorTarget string) (*BatchDiffOutput, error) {
	b.pageSwitchMu.Lock()
	defer b.pageSwitchMu.Unlock()

	b.mu.RLock()
	currentIdx := b.current
	b.mu.RUnlock()

	output := &BatchDiffOutput{}
	totalScore := 0.0
	matchedCount := 0

	for _, sel := range selectors {
		result := BatchDiffResult{Selector: sel}

		// Get source styles
		sourceStyles, err := b.GetComputedStyles(sel, properties)
		if err != nil {
			result.Matched = false
			output.Results = append(output.Results, result)
			continue
		}

		// Switch to target page
		if err := b.SelectPage(againstPageIndex); err != nil {
			result.Matched = false
			result.Details = []BatchDiffDetail{{Property: "error", Expected: "", Actual: err.Error()}}
			output.Results = append(output.Results, result)
			continue
		}

		// Get target styles
		targetSel := sel
		if selectorTarget != "" {
			targetSel = selectorTarget
		}
		targetStyles, err := b.GetComputedStyles(targetSel, properties)

		// Switch back
		b.SelectPage(currentIdx)

		if err != nil {
			result.Matched = false
			output.Results = append(output.Results, result)
			continue
		}

		result.Matched = true

		// Compute diff
		allProps := make(map[string]bool)
		for k := range sourceStyles {
			allProps[k] = true
		}
		for k := range targetStyles {
			allProps[k] = true
		}

		for prop := range allProps {
			src := sourceStyles[prop]
			tgt := targetStyles[prop]
			if src == tgt {
				result.Matches++
			} else {
				result.Mismatches++
				result.Details = append(result.Details, BatchDiffDetail{
					Property: prop,
					Expected: src,
					Actual:   tgt,
				})
			}
		}

		total := result.Matches + result.Mismatches
		if total > 0 {
			result.Score = float64(result.Matches) / float64(total) * 100
		}

		totalScore += result.Score
		matchedCount++
		output.Results = append(output.Results, result)
	}

	if matchedCount > 0 {
		output.OverallScore = totalScore / float64(matchedCount)
	}

	return output, nil
}

// CleanSnapshotOptions controls the clean DOM snapshot output.
type CleanSnapshotOptions struct {
	Depth     int  // max tree depth (0 = unlimited)
	SVGFull   bool // include full SVG content
	MaxLength int  // max output chars (0 = 5000)
}

// CleanDOMNode represents a node in the clean JSON DOM tree.
type CleanDOMNode struct {
	Tag      string            `json:"tag"`
	ID       string            `json:"id,omitempty"`
	Classes  []string          `json:"classes,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Text     string            `json:"text,omitempty"`
	Children []CleanDOMNode    `json:"children,omitempty"`
}

// GetCleanSnapshot returns a cleaned, indented DOM subtree for the given selector.
func (b *Browser) GetCleanSnapshot(selector string, opts CleanSnapshotOptions) (string, *CleanDOMNode, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return "", nil, err
	}

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return "", nil, fmt.Errorf("no element found for selector: %q", selector)
	}

	maxDepth := opts.Depth
	if maxDepth <= 0 {
		maxDepth = 999
	}
	maxLen := opts.MaxLength
	if maxLen <= 0 {
		maxLen = 5000
	}

	result, err := el.Eval(`function(maxDepth, svgFull) {
		const KEEP_ATTRS = new Set(['class','id','href','src','alt','role','type','width','height','action','method','for','name','value','placeholder']);
		const KEEP_DATA = new Set(['data-theme','data-variant','data-state']);
		const SKIP_TAGS = new Set(['script','style','noscript','link']);
		const IMG_SKIP = new Set(['srcset','sizes','loading','decoding','fetchpriority']);

		function isHidden(el) {
			if (!el.offsetParent && el.tagName !== 'HTML' && el.tagName !== 'BODY') {
				const cs = window.getComputedStyle(el);
				if (cs.display === 'none' || cs.visibility === 'hidden') return true;
			}
			return false;
		}

		function isEmptyWrapper(el) {
			if (el.tagName !== 'DIV' && el.tagName !== 'SPAN') return false;
			if (el.id || el.className || el.getAttribute('style')) return false;
			const children = Array.from(el.children);
			return children.length === 1;
		}

		function cleanNode(el, depth) {
			if (el.nodeType === Node.TEXT_NODE) {
				let text = el.textContent.trim();
				if (!text) return null;
				if (text.length > 80) text = text.substring(0, 77) + '…';
				return {tag: '#text', text: text};
			}
			if (el.nodeType !== Node.ELEMENT_NODE) return null;

			const tag = el.tagName.toLowerCase();
			if (SKIP_TAGS.has(tag)) return null;
			if (isHidden(el)) return null;

			// SVG collapse
			if (tag === 'svg' && !svgFull) {
				const w = el.getAttribute('width') || '';
				const h = el.getAttribute('height') || '';
				return {tag:'svg', attrs:{width:w, height:h}};
			}

			// Flatten empty wrappers
			if (isEmptyWrapper(el) && depth < maxDepth) {
				const child = el.children[0];
				return cleanNode(child, depth);
			}

			const node = {tag: tag};

			// Collect attributes
			const attrs = {};
			if (el.id) node.id = el.id;
			if (el.className && typeof el.className === 'string') {
				const classes = el.className.trim().split(/\s+/).filter(c => c);
				if (classes.length > 0) node.classes = classes;
			}

			for (const attr of el.attributes) {
				const name = attr.name;
				if (name === 'class' || name === 'id') continue;
				if (name.startsWith('aria-')) continue;
				if (name.startsWith('data-') && !KEEP_DATA.has(name)) continue;
				if (tag === 'img' && IMG_SKIP.has(name)) continue;
				if (KEEP_ATTRS.has(name) || KEEP_DATA.has(name)) {
					attrs[name] = attr.value;
				}
			}
			if (Object.keys(attrs).length > 0) node.attrs = attrs;

			// Children
			if (depth >= maxDepth) {
				const childCount = el.children.length;
				if (childCount > 0) {
					node.text = '<!-- ' + childCount + ' children -->';
				}
				return node;
			}

			const children = [];
			for (const child of el.childNodes) {
				const cleaned = cleanNode(child, depth + 1);
				if (cleaned) children.push(cleaned);
			}
			if (children.length > 0) node.children = children;

			return node;
		}

		return cleanNode(this, 0);
	}`, maxDepth, opts.SVGFull)
	if err != nil {
		return "", nil, fmt.Errorf("clean snapshot failed: %w", err)
	}

	// Convert gson to plain map
	rawData := result.Value.Val()
	dataMap, ok := rawData.(map[string]interface{})
	if !ok {
		return "", nil, fmt.Errorf("unexpected result type from clean snapshot")
	}

	// Build the JSON tree
	jsonTree := parseCleanNode(dataMap)

	// Build HTML string
	html := renderCleanHTML(dataMap, 0)

	// Truncate if needed
	if len(html) > maxLen {
		html = html[:maxLen] + "\n<!-- output truncated -->"
	}

	return html, jsonTree, nil
}

func parseCleanNode(m map[string]interface{}) *CleanDOMNode {
	if m == nil {
		return nil
	}
	node := &CleanDOMNode{}
	if tag, ok := m["tag"].(string); ok {
		node.Tag = tag
	}
	if id, ok := m["id"].(string); ok {
		node.ID = id
	}
	if text, ok := m["text"].(string); ok {
		node.Text = text
	}
	if classes, ok := m["classes"].([]interface{}); ok {
		for _, c := range classes {
			if s, ok := c.(string); ok {
				node.Classes = append(node.Classes, s)
			}
		}
	}
	if attrs, ok := m["attrs"].(map[string]interface{}); ok {
		node.Attrs = make(map[string]string)
		for k, v := range attrs {
			if s, ok := v.(string); ok {
				node.Attrs[k] = s
			}
		}
	}
	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			if cm, ok := child.(map[string]interface{}); ok {
				if cn := parseCleanNode(cm); cn != nil {
					node.Children = append(node.Children, *cn)
				}
			}
		}
	}
	return node
}

func renderCleanHTML(m map[string]interface{}, indent int) string {
	if m == nil {
		return ""
	}
	prefix := strings.Repeat("  ", indent)
	tag, _ := m["tag"].(string)

	if tag == "#text" {
		text, _ := m["text"].(string)
		return prefix + text + "\n"
	}

	// Build opening tag
	var sb strings.Builder
	sb.WriteString(prefix + "<" + tag)
	if id, ok := m["id"].(string); ok && id != "" {
		sb.WriteString(` id="` + id + `"`)
	}
	if classes, ok := m["classes"].([]interface{}); ok && len(classes) > 0 {
		var classNames []string
		for _, c := range classes {
			if s, ok := c.(string); ok {
				classNames = append(classNames, s)
			}
		}
		sb.WriteString(` class="` + strings.Join(classNames, " ") + `"`)
	}
	if attrs, ok := m["attrs"].(map[string]interface{}); ok {
		for k, v := range attrs {
			if s, ok := v.(string); ok && s != "" {
				sb.WriteString(` ` + k + `="` + s + `"`)
			}
		}
	}

	// Self-closing tags
	children, hasChildren := m["children"].([]interface{})
	text, _ := m["text"].(string)

	if !hasChildren && text == "" {
		sb.WriteString(" />\n")
		return sb.String()
	}

	sb.WriteString(">")

	// Inline text for leaf elements
	if !hasChildren && text != "" {
		sb.WriteString(text + "</" + tag + ">\n")
		return sb.String()
	}

	// Text as placeholder (e.g., "<!-- N children -->")
	if text != "" && !hasChildren {
		sb.WriteString(text + "</" + tag + ">\n")
		return sb.String()
	}

	sb.WriteString("\n")
	for _, child := range children {
		if cm, ok := child.(map[string]interface{}); ok {
			sb.WriteString(renderCleanHTML(cm, indent+1))
		}
	}
	sb.WriteString(prefix + "</" + tag + ">\n")
	return sb.String()
}

// GetComputedStyles returns computed CSS properties for an element identified by CSS selector.
// If properties is empty, a useful default set of layout/typography properties is returned.
func (b *Browser) GetComputedStyles(selector string, properties []string) (map[string]string, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	el, err := b.findElementByCSS(page, selector)
	if err != nil {
		return nil, err
	}

	// Pass properties as a parameter to avoid JS string injection
	if properties == nil {
		properties = []string{}
	}

	result, err := el.Eval(`function(props) {
		const cs = window.getComputedStyle(this);
		const out = {};
		if (props && props.length > 0) {
			props.forEach(p => {
				const v = cs[p] || cs.getPropertyValue(p);
				if (v !== undefined && v !== "") out[p] = v;
			});
		} else {
			const defaults = [
				"fontSize", "fontWeight", "fontFamily", "lineHeight", "letterSpacing",
				"color", "backgroundColor", "display", "textAlign", "padding", "margin",
				"borderRadius", "width", "height", "position", "opacity",
				"textDecoration", "textTransform", "fontStyle",
				"fontFeatureSettings", "textRendering", "webkitFontSmoothing", "fontKerning"
			];
			defaults.forEach(p => {
				const v = cs[p];
				if (v !== undefined && v !== "") out[p] = v;
			});
		}
		return out;
	}`, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to get computed styles: %w", err)
	}

	styles := make(map[string]string)
	for k, v := range result.Value.Map() {
		styles[k] = v.Str()
	}
	return styles, nil
}
