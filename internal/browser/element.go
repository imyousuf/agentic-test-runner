package browser

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

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

	el, err := b.findElement(page, target)
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

	el, err := b.findElement(page, target)
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

	el, err := b.findElement(page, target)
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

	fromEl, err := b.findElement(page, fromTarget)
	if err != nil {
		return fmt.Errorf("source element not found: %w", err)
	}

	toEl, err := b.findElement(page, toTarget)
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
	_, err = b.findElement(page, target)
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
func tryFind(page *rod.Page, timeout time.Duration, fn func(*rod.Page) (*rod.Element, error)) (*rod.Element, error) {
	return fn(page.Timeout(timeout))
}

func (b *Browser) findElement(page *rod.Page, target string) (*rod.Element, error) {
	perAttempt := 500 * time.Millisecond

	// XPath selectors
	if strings.HasPrefix(target, "//") {
		return tryFind(page, 3*time.Second, func(p *rod.Page) (*rod.Element, error) {
			return p.ElementX(target)
		})
	}

	// CSS selectors: unambiguous prefixes or structural analysis
	if strings.HasPrefix(target, "#") || strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "[") || looksLikeCSSSelector(target) {
		return tryFind(page, 3*time.Second, func(p *rod.Page) (*rod.Element, error) {
			return p.Element(target)
		})
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

	return nil, fmt.Errorf("element not found: %s", target)
}

// GetElementScreenshot captures a screenshot of a specific element.
func (b *Browser) GetElementScreenshot(target string) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	el, err := b.findElement(page, target)
	if err != nil {
		return nil, err
	}

	return el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
}

// findElementByCSS finds an element using a CSS selector directly, without
// the multi-strategy fallback chain. Use this when the caller knows the
// target is explicitly a CSS selector.
func (b *Browser) findElementByCSS(page *rod.Page, selector string) (*rod.Element, error) {
	return tryFind(page, 3*time.Second, func(p *rod.Page) (*rod.Element, error) {
		return p.Element(selector)
	})
}

// GetElementScreenshotByCSS captures a screenshot of a specific element
// identified by CSS selector. Unlike GetElementScreenshot, this does not
// attempt fallback strategies (UID, text, aria-label, etc.).
func (b *Browser) GetElementScreenshotByCSS(selector string) ([]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

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
func (b *Browser) ScrollElement(selector string, x, y int, toBottom, toTop bool) (*ScrollResult, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
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

// GetMultipleElementScreenshots captures screenshots of all elements matching a CSS selector.
func (b *Browser) GetMultipleElementScreenshots(selector string) ([][]byte, error) {
	page, err := b.CurrentPage()
	if err != nil {
		return nil, err
	}

	elements, err := page.Timeout(3 * time.Second).Elements(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to find elements: %w", err)
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("no elements found for selector: %s", selector)
	}

	var screenshots [][]byte
	for i, el := range elements {
		data, err := el.Screenshot(proto.PageCaptureScreenshotFormatPng, 0)
		if err != nil {
			return nil, fmt.Errorf("screenshot failed for element %d: %w", i, err)
		}
		screenshots = append(screenshots, data)
	}

	return screenshots, nil
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

	js := `function() {
		const mode = "` + mode + `";
		if (mode === "flat") {
			return [{tag: "text", text: this.textContent.trim()}];
		} else if (mode === "links") {
			return Array.from(this.querySelectorAll("a")).map(a => ({
				tag: "a", text: a.textContent.trim(), href: a.href
			}));
		} else if (mode === "headings") {
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
	}`

	result, err := el.Eval(js)
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

	// Use el.Eval which scopes `this` to the element
	propsArg := "[]"
	if len(properties) > 0 {
		parts := make([]string, len(properties))
		for i, p := range properties {
			parts[i] = `"` + p + `"`
		}
		propsArg = "[" + strings.Join(parts, ",") + "]"
	}

	js := `function() {
		const props = ` + propsArg + `;
		const cs = window.getComputedStyle(this);
		const out = {};
		if (props.length > 0) {
			props.forEach(p => {
				const v = cs[p] || cs.getPropertyValue(p);
				if (v !== undefined && v !== "") out[p] = v;
			});
		} else {
			const defaults = [
				"fontSize", "fontWeight", "fontFamily", "lineHeight", "letterSpacing",
				"color", "backgroundColor", "display", "textAlign", "padding", "margin",
				"borderRadius", "width", "height", "position", "opacity",
				"textDecoration", "textTransform", "fontStyle"
			];
			defaults.forEach(p => {
				const v = cs[p];
				if (v !== undefined && v !== "") out[p] = v;
			});
		}
		return out;
	}`

	result, err := el.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to get computed styles: %w", err)
	}

	styles := make(map[string]string)
	for k, v := range result.Value.Map() {
		styles[k] = v.Str()
	}
	return styles, nil
}
