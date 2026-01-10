package browser

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

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
func (b *Browser) findElement(page *rod.Page, target string) (*rod.Element, error) {
	// If target starts with special prefixes, use direct selectors
	if strings.HasPrefix(target, "#") || strings.HasPrefix(target, ".") ||
		strings.HasPrefix(target, "[") || strings.HasPrefix(target, "//") {
		return page.Element(target)
	}

	// Try by UID (e.g., "e0", "e1")
	if strings.HasPrefix(target, "e") {
		elements, err := page.Elements("button, input, select, textarea, a, [role], [aria-label], [data-testid]")
		if err == nil {
			// Parse index from UID
			var idx int
			if _, err := fmt.Sscanf(target, "e%d", &idx); err == nil && idx >= 0 && idx < len(elements) {
				return elements[idx], nil
			}
		}
	}

	// Try by aria-label
	if el, err := page.Element(fmt.Sprintf(`[aria-label="%s"]`, target)); err == nil {
		return el, nil
	}

	// Try by data-testid
	if el, err := page.Element(fmt.Sprintf(`[data-testid="%s"]`, target)); err == nil {
		return el, nil
	}

	// Try by name attribute
	if el, err := page.Element(fmt.Sprintf(`[name="%s"]`, target)); err == nil {
		return el, nil
	}

	// Try by placeholder
	if el, err := page.Element(fmt.Sprintf(`[placeholder="%s"]`, target)); err == nil {
		return el, nil
	}

	// Try by text content for buttons and links
	if el, err := page.ElementR("button, a, [role='button']", target); err == nil {
		return el, nil
	}

	// Try by label text (for form fields)
	if el, err := page.Element(fmt.Sprintf(`input[id] + label:has-text("%s"), label:has-text("%s") + input`, target, target)); err == nil {
		return el, nil
	}

	// Final attempt: any element containing the text
	if el, err := page.ElementR("*", target); err == nil {
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
