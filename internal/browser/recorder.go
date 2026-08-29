package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/ysmood/gson"
)

// RecordedEvent represents a single user interaction captured during recording.
type RecordedEvent struct {
	Sequence  int       `json:"sequence"`
	Type      string    `json:"type"`       // click, double_click, fill, select_option, keypress, navigate, scroll
	Selector  string    `json:"selector"`   // CSS selector for the target element
	Value     string    `json:"value"`      // typed text, key name, URL, selected option
	TagName   string    `json:"tag_name"`   // HTML tag name of the target element
	InnerText string    `json:"inner_text"` // truncated visible text (max 50 chars)
	InputType string    `json:"input_type"` // for input elements: text, password, email, etc.
	URL       string    `json:"url"`        // page URL when event occurred
	PageTitle string    `json:"page_title"` // page title when event occurred
	Timestamp time.Time `json:"timestamp"`
}

// RecordingSession holds the state of an active recording.
type RecordingSession struct {
	mu          sync.Mutex
	active      bool
	events      []RecordedEvent
	sequence    int
	startTime   time.Time
	startURL    string
	stopFuncs   []func() error // page.Expose() cleanup functions
	removeFuncs []func() error // EvalOnNewDocument cleanup functions
	doneCh      chan struct{}  // closed when recording stops
}

// StartRecording begins recording user interactions across all open pages.
// If initialURL is non-empty, the browser navigates to it first.
func (b *Browser) StartRecording(initialURL string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.recording != nil && b.recording.active {
		return fmt.Errorf("recording already in progress")
	}

	if len(b.pages) == 0 && initialURL == "" {
		return fmt.Errorf("no pages open; provide a URL or navigate first")
	}

	session := &RecordingSession{
		active:    true,
		events:    make([]RecordedEvent, 0),
		startTime: time.Now(),
		startURL:  initialURL,
		doneCh:    make(chan struct{}),
	}
	b.recording = session

	// Navigate FIRST if URL provided (before injecting recorder,
	// because navigation destroys the JS context and Expose bindings)
	if initialURL != "" {
		if len(b.pages) == 0 {
			// Create a new page if none exist
			b.mu.Unlock()
			err := b.NewPage(context.Background(), initialURL)
			b.mu.Lock()
			if err != nil {
				b.recording = nil
				return fmt.Errorf("failed to create page: %w", err)
			}
		} else {
			page := b.pages[b.current]
			if err := page.Navigate(normalizeURL(initialURL)); err != nil {
				b.recording = nil
				return fmt.Errorf("failed to navigate: %w", err)
			}
			// b.waitLoad rather than page.WaitLoad: this runs with b.mu held,
			// and rod's WaitLoad has no deadline of its own, so a page that
			// never fires load would hold the browser lock — and therefore
			// every other daemon call — for as long as the process lives.
			// Bounded, a stalled page is a reason to record from where we
			// are, which is what the ignored error means.
			_ = b.waitLoad(page)
		}
		session.startURL = initialURL
	} else if b.current >= 0 && b.current < len(b.pages) {
		if info, err := b.pages[b.current].Info(); err == nil {
			session.startURL = info.URL
		}
	}

	// Inject recorder AFTER navigation so bindings are in the correct JS context
	for _, page := range b.pages {
		b.injectRecorder(page)
	}

	return nil
}

// StopRecording stops the recording session, cleans up injected scripts,
// and returns all recorded events. Safe to call multiple times — second
// call returns the same events without error.
func (b *Browser) StopRecording() ([]RecordedEvent, error) {
	b.mu.Lock()
	session := b.recording
	// Don't nil b.recording here — keep it around so events can be
	// retrieved by a second StopRecording call (e.g., CLI polling after
	// overlay stop). It gets cleared when a new recording starts.
	b.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("no recording in progress")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		// Already stopped (e.g., via overlay button) — return events
		events := make([]RecordedEvent, len(session.events))
		copy(events, session.events)
		return events, nil
	}

	session.active = false

	// Clean up Expose bindings
	for _, stopFn := range session.stopFuncs {
		_ = stopFn()
	}

	// Clean up EvalOnNewDocument scripts
	for _, removeFn := range session.removeFuncs {
		_ = removeFn()
	}

	// Remove overlay from all pages
	b.mu.RLock()
	for _, page := range b.pages {
		_, _ = page.Eval(`() => {
			const overlay = document.getElementById('__atr-recorder-overlay');
			if (overlay) overlay.remove();
		}`)
	}
	b.mu.RUnlock()

	// Signal done
	select {
	case <-session.doneCh:
		// Already closed
	default:
		close(session.doneCh)
	}

	events := make([]RecordedEvent, len(session.events))
	copy(events, session.events)
	return events, nil
}

// ClearRecording removes the stored session after events have been retrieved.
func (b *Browser) ClearRecording() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.recording != nil && !b.recording.active {
		b.recording = nil
	}
}

// IsRecording returns true if a recording session is active.
func (b *Browser) IsRecording() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.recording != nil && b.recording.active
}

// RecordingEventCount returns the number of events recorded so far.
func (b *Browser) RecordingEventCount() int {
	b.mu.RLock()
	session := b.recording
	b.mu.RUnlock()

	if session == nil {
		return 0
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	return len(session.events)
}

// RecordingDone returns a channel that is closed when the recording stops.
func (b *Browser) RecordingDone() <-chan struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.recording != nil {
		return b.recording.doneCh
	}
	// Return a closed channel if not recording
	ch := make(chan struct{})
	close(ch)
	return ch
}

// injectRecorder sets up the JS→Go bridge and event listeners on a page.
// Must be called while b.recording is set and active.
func (b *Browser) injectRecorder(page *rod.Page) {
	session := b.recording
	if session == nil {
		return
	}

	// Set up the JS→Go bridge via page.Expose
	stop, err := page.Expose("__atrRecordEvent", func(payload gson.JSON) (any, error) {
		return b.handleRecorderEvent(payload)
	})
	if err != nil {
		return
	}

	session.mu.Lock()
	session.stopFuncs = append(session.stopFuncs, stop)
	session.mu.Unlock()

	// Register the recorder init script to run on every new document
	remove, err := page.EvalOnNewDocument(recorderInitScript)
	if err == nil {
		session.mu.Lock()
		session.removeFuncs = append(session.removeFuncs, remove)
		session.mu.Unlock()
	}

	// Run the recorder immediately on the current document
	_, _ = page.Eval(`() => { ` + recorderInitScript + ` }`)
}

// handleRecorderEvent processes events sent from the JS recorder.
func (b *Browser) handleRecorderEvent(payload gson.JSON) (any, error) {
	eventType := payload.Get("type").Str()

	// Handle stop signal from overlay button
	if eventType == "stop" {
		go func() {
			b.StopRecording()
		}()
		return nil, nil
	}

	b.mu.RLock()
	session := b.recording
	b.mu.RUnlock()

	if session == nil {
		return nil, nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return nil, nil
	}

	session.sequence++
	event := RecordedEvent{
		Sequence:  session.sequence,
		Type:      eventType,
		Selector:  payload.Get("selector").Str(),
		Value:     payload.Get("value").Str(),
		TagName:   payload.Get("tagName").Str(),
		InnerText: payload.Get("innerText").Str(),
		InputType: payload.Get("inputType").Str(),
		URL:       payload.Get("url").Str(),
		PageTitle: payload.Get("pageTitle").Str(),
		Timestamp: time.Now(),
	}

	session.events = append(session.events, event)
	return nil, nil
}

// expectedResultsPrompt is what a recording leaves in place of assertions.
//
// It used to emit "Steps completed successfully / No console errors", which is
// an assertion that cannot fail: the recorded clicks all succeed by
// construction, so the compiled test goes green whatever the application does.
// A recording knows what was clicked and cannot know what it proved, so the
// honest output is a question rather than an answer somebody will trust.
const expectedResultsPrompt = `
Expected Results:
- TODO: say what must be true at the end for this test to have passed.
  A recorded click sequence cannot tell you that. Name the element, text or
  count that proves the feature worked, and delete this line.
`

// FormatTestFile converts recorded events into a .test.txt behavior test file.
func FormatTestFile(events []RecordedEvent, testName string) string {
	if len(events) == 0 {
		return fmt.Sprintf("Test: %s\n\nSteps:\n1. (no interactions recorded)\n%s", testName, expectedResultsPrompt)
	}

	// Pre-process: merge sequential fills on same selector, merge click+navigate
	processed := mergeEvents(events)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Test: %s\n\nSteps:\n", testName)

	for i, evt := range processed {
		step := eventToStep(evt)
		fmt.Fprintf(&sb, "%d. %s\n", i+1, step)
	}

	sb.WriteString(expectedResultsPrompt)
	return sb.String()
}

// mergeEvents pre-processes events: deduplicates sequential fills on same selector,
// and merges click followed by navigation within 1 second.
func mergeEvents(events []RecordedEvent) []RecordedEvent {
	if len(events) == 0 {
		return events
	}

	result := make([]RecordedEvent, 0, len(events))

	for i := 0; i < len(events); i++ {
		evt := events[i]

		// Skip sequential fills on same selector — keep only the last one
		if evt.Type == "fill" {
			for i+1 < len(events) && events[i+1].Type == "fill" && events[i+1].Selector == evt.Selector {
				i++
				evt = events[i]
			}
		}

		// Merge click followed by navigation within 1 second
		if (evt.Type == "click" || evt.Type == "double_click") && i+1 < len(events) {
			next := events[i+1]
			if next.Type == "navigate" && next.Timestamp.Sub(evt.Timestamp) < time.Second {
				// Skip the navigate event — the click implies it
				result = append(result, evt)
				i++ // skip navigate
				continue
			}
		}

		result = append(result, evt)
	}

	return result
}

// eventToStep converts a single recorded event to a natural language step.
func eventToStep(evt RecordedEvent) string {
	switch evt.Type {
	case "click":
		return formatClickStep(evt)
	case "double_click":
		return "Double-click " + formatClickTarget(evt)
	case "fill":
		return formatFillStep(evt)
	case "select_option":
		return formatSelectStep(evt)
	case "keypress":
		return fmt.Sprintf("Press %s", evt.Value)
	case "navigate":
		return fmt.Sprintf("Navigate to %s", evt.Value)
	case "scroll":
		if evt.Selector == "window" || evt.Selector == "" {
			return "Scroll down on the page"
		}
		return fmt.Sprintf("Scroll within %s", describeElement(evt))
	default:
		return fmt.Sprintf("Perform %s on %s", evt.Type, evt.Selector)
	}
}

// formatClickStep produces a human-readable click step.
func formatClickStep(evt RecordedEvent) string {
	return "Click " + formatClickTarget(evt)
}

// formatClickTarget describes the click target.
func formatClickTarget(evt RecordedEvent) string {
	text := evt.InnerText
	if text != "" {
		tag := strings.ToLower(evt.TagName)
		switch tag {
		case "button":
			return fmt.Sprintf(`the "%s" button`, text)
		case "a":
			return fmt.Sprintf(`the "%s" link`, text)
		case "input":
			if evt.InputType == "submit" {
				return fmt.Sprintf(`the "%s" button`, text)
			}
			return fmt.Sprintf("on %s", evt.Selector)
		default:
			return fmt.Sprintf(`the "%s" %s`, text, tag)
		}
	}
	return fmt.Sprintf("on %s", evt.Selector)
}

// formatFillStep produces a human-readable fill step.
func formatFillStep(evt RecordedEvent) string {
	value := evt.Value
	if evt.InputType == "password" {
		value = "[password]"
	}
	desc := describeElement(evt)
	return fmt.Sprintf(`Enter "%s" in the %s`, value, desc)
}

// formatSelectStep produces a human-readable select step.
func formatSelectStep(evt RecordedEvent) string {
	desc := describeElement(evt)
	return fmt.Sprintf(`Select "%s" from the %s`, evt.Value, desc)
}

// describeElement produces a human-readable description of an element.
func describeElement(evt RecordedEvent) string {
	if evt.InnerText != "" && (evt.TagName == "button" || evt.TagName == "a") {
		return fmt.Sprintf(`"%s" %s`, evt.InnerText, strings.ToLower(evt.TagName))
	}
	// Try to derive a description from the selector
	sel := evt.Selector
	if name, ok := strings.CutPrefix(sel, "input[name=\""); ok {
		name = strings.TrimSuffix(name, "\"]")
		return name + " field"
	}
	if ph, ok := strings.CutPrefix(sel, "input[placeholder=\""); ok {
		ph = strings.TrimSuffix(ph, "\"]")
		return ph + " field"
	}
	if id, ok := strings.CutPrefix(sel, "#"); ok {
		return id + " field"
	}
	if label, ok := strings.CutPrefix(sel, "[aria-label=\""); ok {
		label = strings.TrimSuffix(label, "\"]")
		return label + " field"
	}
	return sel
}

// recorderInitScript is the JavaScript IIFE injected into every page to capture
// user interactions and render the recording overlay.
const recorderInitScript = `(function() {
  // Guard: only activate if the Go bridge function exists
  if (typeof window.__atrRecordEvent !== 'function') return;
  // Guard: don't attach event listeners twice
  if (window.__atrRecorderListenersAttached) {
    // But DO retry overlay creation if it's missing
    if (!document.getElementById('__atr-recorder-overlay')) createOverlay();
    return;
  }
  window.__atrRecorderListenersAttached = true;

  // --- Selector Generation ---
  function generateSelector(el) {
    if (!el || el === document.body || el === document.documentElement) return 'body';

    // If element is inside a shadow DOM, build a prefixed selector:
    // hostSelector >> innerSelector
    const root = el.getRootNode();
    if (root && root !== document && root.host) {
      const hostSel = generateSelector(root.host);
      const innerSel = generateInnerSelector(el, root);
      return hostSel + ' >> ' + innerSel;
    }

    // 1. ID (skip auto-generated looking IDs)
    if (el.id && !/^[a-z]+-[a-f0-9]{4,}$/i.test(el.id) && document.querySelectorAll('#' + CSS.escape(el.id)).length === 1) {
      return '#' + CSS.escape(el.id);
    }

    // 2. data-testid / data-test-id / data-cy
    for (const attr of ['data-testid', 'data-test-id', 'data-cy']) {
      const val = el.getAttribute(attr);
      if (val) return '[' + attr + '="' + val + '"]';
    }

    // 3. aria-label
    const ariaLabel = el.getAttribute('aria-label');
    if (ariaLabel) return '[aria-label="' + ariaLabel + '"]';

    // 4. name attribute (for form elements)
    const tag = el.tagName.toLowerCase();
    if (['input', 'select', 'textarea'].includes(tag) && el.name) {
      const sel = tag + '[name="' + el.name + '"]';
      if (document.querySelectorAll(sel).length === 1) return sel;
    }

    // 5. placeholder (for input/textarea)
    if (['input', 'textarea'].includes(tag) && el.placeholder) {
      const sel = tag + '[placeholder="' + el.placeholder + '"]';
      if (document.querySelectorAll(sel).length === 1) return sel;
    }

    // 6. Unique class combination
    if (el.classList.length > 0) {
      for (let i = 0; i < el.classList.length; i++) {
        const cls = el.classList[i];
        if (cls.startsWith('__atr-')) continue;
        const sel = tag + '.' + CSS.escape(cls);
        if (document.querySelectorAll(sel).length === 1) return sel;
      }
    }

    // 7. nth-child path
    return buildNthChildPath(el);
  }

  // Generate a selector for an element within a shadow root.
  function generateInnerSelector(el, shadowRoot) {
    // Try the same strategies but query within the shadow root
    if (el.id && !/^[a-z]+-[a-f0-9]{4,}$/i.test(el.id)) {
      return '#' + CSS.escape(el.id);
    }
    for (const attr of ['data-testid', 'data-test-id', 'data-cy']) {
      const val = el.getAttribute(attr);
      if (val) return '[' + attr + '="' + val + '"]';
    }
    const ariaLabel = el.getAttribute('aria-label');
    if (ariaLabel) return '[aria-label="' + ariaLabel + '"]';
    const tag = el.tagName.toLowerCase();
    if (['input', 'select', 'textarea'].includes(tag) && el.name) {
      const sel = tag + '[name="' + el.name + '"]';
      if (shadowRoot.querySelectorAll(sel).length === 1) return sel;
    }
    if (['input', 'textarea'].includes(tag) && el.placeholder) {
      const sel = tag + '[placeholder="' + el.placeholder + '"]';
      if (shadowRoot.querySelectorAll(sel).length === 1) return sel;
    }
    if (el.classList && el.classList.length > 0) {
      for (let i = 0; i < el.classList.length; i++) {
        const cls = el.classList[i];
        const sel = tag + '.' + CSS.escape(cls);
        if (shadowRoot.querySelectorAll(sel).length === 1) return sel;
      }
    }
    // Use text content for buttons/links
    const text = el.textContent ? el.textContent.trim() : '';
    if (text && text.length < 50 && ['button', 'a', 'span'].includes(tag)) {
      return tag + ':text("' + text.replace(/"/g, '\\"') + '")';
    }
    return buildNthChildPath(el);
  }

  function buildNthChildPath(el) {
    const parts = [];
    let current = el;
    while (current && current !== document.body && current !== document.documentElement) {
      const tag = current.tagName.toLowerCase();
      const parent = current.parentElement;
      if (!parent) break;
      const siblings = Array.from(parent.children).filter(c => c.tagName.toLowerCase() === tag);
      if (siblings.length === 1) {
        parts.unshift(tag);
      } else {
        const idx = siblings.indexOf(current) + 1;
        parts.unshift(tag + ':nth-child(' + idx + ')');
      }
      current = parent;
    }
    return 'body > ' + parts.join(' > ');
  }

  // Find the deepest meaningful element, including inside shadow DOM.
  // Uses composedPath() to see through shadow DOM boundaries since
  // event.target is retargeted to the shadow host.
  function findDeepestElement(e) {
    let el = e.target;
    // composedPath()[0] gives the actual element inside shadow DOM
    if (e.composedPath && e.composedPath().length > 0) {
      const deep = e.composedPath()[0];
      if (deep && deep.nodeType === Node.ELEMENT_NODE) {
        el = deep;
      } else if (deep && deep.nodeType === Node.TEXT_NODE && deep.parentElement) {
        el = deep.parentElement;
      }
    }
    // Walk up to find the first "meaningful" interactive element
    return findMeaningfulTarget(el);
  }

  // Walk up the DOM to find the closest interactive/meaningful element.
  // Stops at buttons, links, inputs, or elements with roles/labels.
  // Crosses shadow DOM boundaries via host elements.
  // Falls back to the original element if nothing better is found.
  function findMeaningfulTarget(el) {
    const interactiveTags = new Set(['button', 'a', 'input', 'select', 'textarea', 'label', 'summary']);
    const interactiveRoles = new Set(['button', 'link', 'menuitem', 'tab', 'checkbox', 'radio', 'switch', 'option']);
    let current = el;
    let bestEl = el;
    let depth = 0;
    while (current && depth < 15) {
      if (current === document.body || current === document.documentElement) break;
      const tag = current.tagName ? current.tagName.toLowerCase() : '';
      if (interactiveTags.has(tag)) return current;
      const role = current.getAttribute ? current.getAttribute('role') : null;
      if (role && interactiveRoles.has(role)) return current;
      if (current.getAttribute && current.getAttribute('data-testid')) return current;
      if (current.onclick || (current.getAttribute && current.getAttribute('tabindex') === '0')) return current;
      // Don't walk past contenteditable containers
      if (current.isContentEditable && current !== el) break;
      // Cross shadow DOM boundary via host element
      if (!current.parentElement && current.getRootNode && current.getRootNode().host) {
        current = current.getRootNode().host;
      } else {
        current = current.parentElement;
      }
      depth++;
    }
    return bestEl;
  }

  function truncateText(text, max) {
    if (!text) return '';
    text = text.trim().replace(/\s+/g, ' ');
    if (text.length > max) return text.substring(0, max) + '...';
    return text;
  }

  // Get the direct text of an element (not descendants)
  function getDirectText(el) {
    let text = '';
    for (const node of el.childNodes) {
      if (node.nodeType === Node.TEXT_NODE) text += node.textContent;
    }
    return text.trim();
  }

  // Get best human-readable text for an element (prefer direct, fallback to full)
  function getBestText(el) {
    const direct = getDirectText(el);
    if (direct) return truncateText(direct, 50);
    // For elements with only child elements, use textContent but truncated
    const full = el.textContent;
    if (full && full.length < 100) return truncateText(full, 50);
    // For large containers, return nothing
    return '';
  }

  // --- Event Sending ---
  function sendEvent(data) {
    data.url = location.href;
    data.pageTitle = document.title;
    try {
      window.__atrRecordEvent(data);
    } catch(e) { /* binding may be gone during navigation */ }
    // Update overlay
    addOverlayStep(data);
  }

  // --- Click Deduplication ---
  let lastClickSelector = '';
  let lastClickTime = 0;
  const CLICK_DEDUP_MS = 300;

  // --- Overlay ---
  let overlayMinimized = false;
  let stepCount = 0;

  function createOverlay() {
    if (document.getElementById('__atr-recorder-overlay')) return;
    // Wait for body if it doesn't exist yet — poll every 50ms (max 5s)
    if (!document.body) {
      let retries = 0;
      const poll = setInterval(function() {
        retries++;
        if (document.body) { clearInterval(poll); createOverlay(); }
        else if (retries > 100) { clearInterval(poll); }
      }, 50);
      return;
    }

    const overlay = document.createElement('div');
    overlay.id = '__atr-recorder-overlay';
    overlay.innerHTML = [
      '<div id="__atr-recorder-header" style="display:flex;align-items:center;justify-content:space-between;cursor:move;padding:8px 12px;background:#1a1a2e;border-bottom:1px solid #333;">',
      '  <div style="display:flex;align-items:center;gap:8px;">',
      '    <div id="__atr-recorder-dot" style="width:10px;height:10px;border-radius:50%;background:#ff4444;animation:__atr-pulse 1.5s ease-in-out infinite;"></div>',
      '    <span style="color:#fff;font-size:13px;font-weight:600;">ATR Recording</span>',
      '    <span id="__atr-recorder-count" style="color:#aaa;font-size:11px;">(0 steps)</span>',
      '  </div>',
      '  <div style="display:flex;gap:4px;">',
      '    <button id="__atr-recorder-minimize" style="background:none;border:none;color:#aaa;cursor:pointer;font-size:16px;padding:2px 6px;" title="Minimize">_</button>',
      '    <button id="__atr-recorder-stop" style="background:#ff4444;border:none;color:#fff;cursor:pointer;font-size:11px;padding:4px 10px;border-radius:3px;font-weight:600;" title="Stop">Stop</button>',
      '  </div>',
      '</div>',
      '<div id="__atr-recorder-body" style="max-height:300px;overflow-y:auto;padding:8px;">',
      '  <div id="__atr-recorder-steps" style="font-size:12px;color:#ccc;line-height:1.6;"></div>',
      '</div>'
    ].join('\n');

    overlay.setAttribute('style', [
      'position:fixed !important',
      'bottom:16px !important',
      'right:16px !important',
      'width:320px !important',
      'background:#16213e !important',
      'border:1px solid #333 !important',
      'border-radius:8px !important',
      'box-shadow:0 8px 32px rgba(0,0,0,0.4) !important',
      'z-index:2147483647 !important',
      'font-family:-apple-system,BlinkMacSystemFont,sans-serif !important',
      'color:#fff !important',
      'overflow:hidden !important',
      'display:block !important',
      'visibility:visible !important',
      'opacity:1 !important',
      'pointer-events:auto !important',
      'transform:none !important'
    ].join(';'));

    // Add pulse animation
    if (!document.getElementById('__atr-recorder-style')) {
      const style = document.createElement('style');
      style.id = '__atr-recorder-style';
      style.textContent = '@keyframes __atr-pulse { 0%,100% { opacity:1; } 50% { opacity:0.3; } }';
      (document.head || document.documentElement).appendChild(style);
    }

    document.body.appendChild(overlay);

    // Dragging
    let isDragging = false, startX, startY, origX, origY;
    const header = document.getElementById('__atr-recorder-header');
    header.addEventListener('mousedown', function(e) {
      if (e.target.tagName === 'BUTTON') return;
      isDragging = true;
      startX = e.clientX;
      startY = e.clientY;
      const rect = overlay.getBoundingClientRect();
      origX = rect.left;
      origY = rect.top;
      e.preventDefault();
    });
    document.addEventListener('mousemove', function(e) {
      if (!isDragging) return;
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;
      overlay.style.setProperty('left', (origX + dx) + 'px', 'important');
      overlay.style.setProperty('top', (origY + dy) + 'px', 'important');
      overlay.style.setProperty('right', 'auto', 'important');
      overlay.style.setProperty('bottom', 'auto', 'important');
    });
    document.addEventListener('mouseup', function() { isDragging = false; });

    // Minimize
    document.getElementById('__atr-recorder-minimize').addEventListener('click', function(e) {
      e.stopPropagation();
      overlayMinimized = !overlayMinimized;
      const body = document.getElementById('__atr-recorder-body');
      body.style.display = overlayMinimized ? 'none' : 'block';
      overlay.style.setProperty('width', overlayMinimized ? 'auto' : '320px', 'important');
      this.textContent = overlayMinimized ? '+' : '_';
    });

    // Stop
    document.getElementById('__atr-recorder-stop').addEventListener('click', function(e) {
      e.stopPropagation();
      sendEvent({ type: 'stop' });
    });
  }

  function addOverlayStep(data) {
    if (data.type === 'stop') return;
    stepCount++;
    const countEl = document.getElementById('__atr-recorder-count');
    if (countEl) countEl.textContent = '(' + stepCount + ' step' + (stepCount !== 1 ? 's' : '') + ')';

    const stepsEl = document.getElementById('__atr-recorder-steps');
    if (!stepsEl) return;

    let desc = '';
    switch (data.type) {
      case 'click': desc = 'Click ' + (data.innerText ? '"' + data.innerText + '"' : data.selector); break;
      case 'double_click': desc = 'Double-click ' + (data.innerText ? '"' + data.innerText + '"' : data.selector); break;
      case 'fill': desc = 'Type "' + (data.inputType === 'password' ? '[password]' : truncateText(data.value, 30)) + '" in ' + data.selector; break;
      case 'select_option': desc = 'Select "' + data.value + '" from ' + data.selector; break;
      case 'keypress': desc = 'Press ' + data.value; break;
      case 'navigate': desc = 'Navigate to ' + data.value; break;
      case 'scroll': desc = 'Scroll'; break;
      default: desc = data.type;
    }

    const step = document.createElement('div');
    step.style.cssText = 'padding:3px 0;border-bottom:1px solid #1a1a2e;';
    step.textContent = stepCount + '. ' + desc;
    stepsEl.appendChild(step);
    step.scrollIntoView({ block: 'end', behavior: 'smooth' });
  }

  // --- Event Listeners ---

  // Click — deferred so dblclick can cancel it
  let pendingClick = null;
  const DBLCLICK_WAIT_MS = 300;

  document.addEventListener('click', function(e) {
    if (e.target.closest('#__atr-recorder-overlay')) return;
    const el = findDeepestElement(e);
    const selector = generateSelector(el);

    // Deduplicate rapid clicks on the same selector
    const now = Date.now();
    if (selector === lastClickSelector && (now - lastClickTime) < CLICK_DEDUP_MS) {
      lastClickTime = now;
      return;
    }
    lastClickSelector = selector;
    lastClickTime = now;

    // Defer click — dblclick will cancel it if it fires within 300ms
    if (pendingClick) clearTimeout(pendingClick);
    const clickData = {
      type: 'click',
      selector: selector,
      tagName: el.tagName.toLowerCase(),
      innerText: getBestText(el),
      inputType: el.type || ''
    };
    pendingClick = setTimeout(function() {
      pendingClick = null;
      sendEvent(clickData);
    }, DBLCLICK_WAIT_MS);
  }, true);

  // Double click — cancels pending click and sends dblclick instead
  document.addEventListener('dblclick', function(e) {
    if (e.target.closest('#__atr-recorder-overlay')) return;
    // Cancel the pending single click
    if (pendingClick) { clearTimeout(pendingClick); pendingClick = null; }
    const el = findDeepestElement(e);
    sendEvent({
      type: 'double_click',
      selector: generateSelector(el),
      tagName: el.tagName.toLowerCase(),
      innerText: getBestText(el),
      inputType: el.type || ''
    });
  }, true);

  // Input (debounced) — handles both form inputs and contenteditable
  const inputTimers = new Map();
  document.addEventListener('input', function(e) {
    if (e.target.closest('#__atr-recorder-overlay')) return;
    // Use composedPath for shadow DOM
    let el = e.target;
    if (e.composedPath && e.composedPath().length > 0) {
      const deep = e.composedPath()[0];
      if (deep && deep.nodeType === Node.ELEMENT_NODE) el = deep;
      else if (deep && deep.nodeType === Node.TEXT_NODE && deep.parentElement) el = deep.parentElement;
    }
    const selector = generateSelector(el);

    if (inputTimers.has(selector)) clearTimeout(inputTimers.get(selector));
    inputTimers.set(selector, setTimeout(function() {
      inputTimers.delete(selector);
      let value;
      if (el.type === 'password') {
        value = '[password]';
      } else if (el.value !== undefined && el.value !== '') {
        value = el.value;
      } else if (el.isContentEditable || el.closest('[contenteditable]')) {
        // For contenteditable, capture the text content (truncated for large editors)
        value = truncateText(el.textContent, 200);
      } else {
        value = el.textContent || '';
      }
      sendEvent({
        type: 'fill',
        selector: selector,
        value: value,
        tagName: el.tagName.toLowerCase(),
        innerText: '',
        inputType: el.type || (el.isContentEditable ? 'contenteditable' : '')
      });
    }, 500));
  }, true);

  // Select change
  document.addEventListener('change', function(e) {
    if (e.target.closest('#__atr-recorder-overlay')) return;
    let el = e.target;
    if (e.composedPath && e.composedPath().length > 0) {
      const deep = e.composedPath()[0];
      if (deep && deep.nodeType === Node.ELEMENT_NODE) el = deep;
    }
    if (el.tagName.toLowerCase() !== 'select') return;
    const selectedOption = el.options[el.selectedIndex];
    sendEvent({
      type: 'select_option',
      selector: generateSelector(el),
      value: selectedOption ? selectedOption.textContent.trim() : el.value,
      tagName: 'select',
      innerText: '',
      inputType: ''
    });
  }, true);

  // Keydown (non-printable only)
  const specialKeys = new Set(['Enter', 'Escape', 'Tab', 'Backspace', 'Delete',
    'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight',
    'Home', 'End', 'PageUp', 'PageDown',
    'F1','F2','F3','F4','F5','F6','F7','F8','F9','F10','F11','F12']);
  document.addEventListener('keydown', function(e) {
    if (e.target.closest && e.target.closest('#__atr-recorder-overlay')) return;
    let key = e.key;
    const hasModifier = e.ctrlKey || e.metaKey || e.altKey;
    if (!specialKeys.has(key) && !hasModifier) return;

    // Build combo string
    const parts = [];
    if (e.ctrlKey || e.metaKey) parts.push('Ctrl');
    if (e.altKey) parts.push('Alt');
    if (e.shiftKey && hasModifier) parts.push('Shift');
    parts.push(key);
    const combo = parts.join('+');

    sendEvent({
      type: 'keypress',
      selector: generateSelector(e.target),
      value: combo,
      tagName: e.target.tagName.toLowerCase(),
      innerText: '',
      inputType: ''
    });
  }, true);

  // Scroll (debounced)
  let scrollTimer = null;
  document.addEventListener('scroll', function(e) {
    if (scrollTimer) clearTimeout(scrollTimer);
    scrollTimer = setTimeout(function() {
      scrollTimer = null;
      const target = e.target === document ? 'window' : generateSelector(e.target);
      sendEvent({
        type: 'scroll',
        selector: target,
        value: '',
        tagName: '',
        innerText: '',
        inputType: ''
      });
    }, 300);
  }, true);

  // SPA navigation detection (URL polling)
  let lastURL = location.href;
  setInterval(function() {
    if (location.href !== lastURL) {
      const newURL = location.href;
      lastURL = newURL;
      sendEvent({
        type: 'navigate',
        selector: '',
        value: newURL,
        tagName: '',
        innerText: '',
        inputType: ''
      });
    }
  }, 500);

  // Create the overlay only in the top frame (not iframes)
  if (window === window.top) createOverlay();
})();`
