package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"testing"
	"time"
)

// One grammar for every API that takes a target.
//
// The compile prompt promises "Targets are standard CSS or XPath, plus one
// extension: :has-text(...)", and the model takes it at its word: it finds an
// element with an XPath through browser_click and then reads the same XPath
// through atr.text. Clicks and waits resolved it; text, screenshot, scroll,
// style and snapshot reads went through a second resolver that had no XPath
// branch, so the read failed as "invalid selector" — a script fault, a repair,
// and two model calls spent on a selector that was never wrong.
//
// The table below is what keeps the two from drifting apart again: the same
// element, written each way the grammar allows, fed to each API. An API added
// later belongs in it.

// grammarForm is one spelling of a target.
type grammarForm struct {
	name string
	// panel names the orders panel: a scrollable container holding an image.
	panel string
	// button names the "Place order" button.
	button string
	// input names the note field.
	input string
}

var grammarForms = []grammarForm{
	{
		name:   "css",
		panel:  `section[data-name="orders"]`,
		button: `#place`,
		input:  `#note`,
	},
	{
		name:   "xpath",
		panel:  `//section[@data-name="orders"]`,
		button: `//button[normalize-space()="Place order"]`,
		input:  `//input[@aria-label="Order note"]`,
	},
	{
		name:   "has-text",
		panel:  `section:has-text("Orders panel")`,
		button: `button:has-text("Place order")`,
		input:  `input:has-text("Leave a note")`,
	},
}

const (
	// What the orders panel looks like, and the decoy before it does not.
	ordersColor = "rgb(1, 2, 3)"
	ordersWidth = 240
	ordersText  = "Orders panel"
	decoyText   = "Decoy panel"
)

func openGrammarFixture(t *testing.T) {
	t.Helper()
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/selector_grammar.html"); err != nil {
		t.Fatalf("navigate to the grammar fixture: %v", err)
	}
}

// pageValue evaluates an expression in the fixture and returns it as text.
func pageValue(t *testing.T, expr string) string {
	t.Helper()
	v, err := testBrowser.Evaluate(expr)
	if err != nil {
		t.Fatalf("evaluating %s: %v", expr, err)
	}
	return fmt.Sprint(v)
}

// isOrdersPanel checks a capture is of the orders panel rather than the decoy
// above it, which is the same height and a different width.
//
// A whole multiple of the width, not the width itself: other tests in this
// package emulate a high-density display, and a capture is in device pixels.
// No multiple of the decoy's 200 lands on a multiple of 240 at any density a
// test here uses.
func isOrdersPanel(data []byte) error {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the capture is not a PNG: %w", err)
	}
	if scale := cfg.Width / ordersWidth; cfg.Width%ordersWidth != 0 || scale < 1 || scale > 4 {
		return fmt.Errorf("captured an element %dpx wide, want the orders panel at a multiple of %dpx", cfg.Width, ordersWidth)
	}
	return nil
}

func wantOrdersColor(styles map[string]string) error {
	if got := styles["color"]; got != ordersColor {
		return fmt.Errorf("color = %q, want %q — the styles of some other element", got, ordersColor)
	}
	return nil
}

// grammarAPIs is every API that takes a target, each with a check that it
// reached the element the target names.
var grammarAPIs = []struct {
	name  string
	check func(t *testing.T, f grammarForm) error
}{
	// --- reads: these went through the resolver that had no XPath branch ----
	{"GetTextContent", func(t *testing.T, f grammarForm) error {
		got, err := testBrowser.GetTextContent(f.panel, "flat")
		if err != nil {
			return err
		}
		if len(got.Groups) != 1 || !strings.Contains(got.Groups[0].Text, ordersText) ||
			strings.Contains(got.Groups[0].Text, decoyText) {
			return fmt.Errorf("read %+v, want the orders panel's text", got.Groups)
		}
		return nil
	}},
	{"GetComputedStyles", func(t *testing.T, f grammarForm) error {
		styles, err := testBrowser.GetComputedStyles(f.panel, []string{"color"})
		if err != nil {
			return err
		}
		return wantOrdersColor(styles)
	}},
	{"GetBatchComputedStyles", func(t *testing.T, f grammarForm) error {
		results, err := testBrowser.GetBatchComputedStyles([]string{f.panel}, []string{"color"})
		if err != nil {
			return err
		}
		if len(results) != 1 || !results[0].Matched {
			return fmt.Errorf("the selector was reported as matching nothing: %+v", results)
		}
		return wantOrdersColor(results[0].Styles)
	}},
	{"GetMultipleComputedStyles", func(t *testing.T, f grammarForm) error {
		entries, err := testBrowser.GetMultipleComputedStyles(f.panel, []string{"color"})
		if err != nil {
			return err
		}
		if len(entries) != 1 {
			return fmt.Errorf("matched %d elements, want exactly the orders panel", len(entries))
		}
		return wantOrdersColor(entries[0].Styles)
	}},
	{"GetCleanSnapshot", func(t *testing.T, f grammarForm) error {
		html, tree, err := testBrowser.GetCleanSnapshot(f.panel, CleanSnapshotOptions{})
		if err != nil {
			return err
		}
		if tree == nil || tree.Tag != "section" {
			return fmt.Errorf("snapshot root = %+v, want the section", tree)
		}
		if !strings.Contains(html, ordersText) || strings.Contains(html, decoyText) {
			return fmt.Errorf("snapshot is not of the orders panel:\n%s", html)
		}
		return nil
	}},
	{"ScrollElement", func(t *testing.T, f grammarForm) error {
		res, err := testBrowser.ScrollElement(f.panel, 0, 0, true, false)
		if err != nil {
			return err
		}
		if res.ScrollTop <= 0 {
			return fmt.Errorf("scrollTop = %d after scrolling to the bottom", res.ScrollTop)
		}
		if got := pageValue(t, `document.querySelector('[data-name="orders"]').scrollTop > 0 && document.querySelector('[data-name="decoy"]').scrollTop === 0`); got != "true" {
			return fmt.Errorf("the orders panel did not scroll, or the decoy did")
		}
		return nil
	}},
	{"GetElementScreenshotByCSS", func(t *testing.T, f grammarForm) error {
		data, err := testBrowser.GetElementScreenshotByCSS(f.panel)
		if err != nil {
			return err
		}
		return isOrdersPanel(data)
	}},
	{"GetElementFullHeightScreenshot", func(t *testing.T, f grammarForm) error {
		data, err := testBrowser.GetElementFullHeightScreenshot(f.panel)
		if err != nil {
			return err
		}
		return isOrdersPanel(data)
	}},
	{"GetMultipleElementScreenshots", func(t *testing.T, f grammarForm) error {
		results, err := testBrowser.GetMultipleElementScreenshots(f.panel)
		if err != nil {
			return err
		}
		if len(results) != 1 || results[0].Error != "" {
			return fmt.Errorf("captures = %+v, want exactly the orders panel", results)
		}
		return isOrdersPanel(results[0].Data)
	}},
	{"DownloadImages", func(t *testing.T, f grammarForm) error {
		images, err := testBrowser.DownloadImages(f.panel, false)
		if err != nil {
			return err
		}
		if len(images) != 1 || images[0].Error != "" || len(images[0].Data) == 0 ||
			!strings.HasSuffix(images[0].Source, "/test_pixel.png") {
			return fmt.Errorf("images = %+v, want the one image inside the orders panel", images)
		}
		return nil
	}},

	// --- actions and waits: these already took all three ------------------
	{"GetElementScreenshot", func(t *testing.T, f grammarForm) error {
		data, err := testBrowser.GetElementScreenshot(f.panel)
		if err != nil {
			return err
		}
		return isOrdersPanel(data)
	}},
	{"WaitForElement", func(t *testing.T, f grammarForm) error {
		return testBrowser.WaitForElement(context.Background(), f.panel, 2*time.Second)
	}},
	{"WaitForElementVisible", func(t *testing.T, f grammarForm) error {
		return testBrowser.WaitForElementVisible(context.Background(), f.panel, 2*time.Second)
	}},
	{"Click", func(t *testing.T, f grammarForm) error {
		if err := testBrowser.Click(context.Background(), f.button, false); err != nil {
			return err
		}
		if got := pageValue(t, `document.getElementById("events").textContent`); !strings.Contains(got, "click:place;") ||
			strings.Contains(got, "decoy-button") {
			return fmt.Errorf("events = %q, want a click on the Place order button only", got)
		}
		return nil
	}},
	{"Click (double)", func(t *testing.T, f grammarForm) error {
		if err := testBrowser.Click(context.Background(), f.button, true); err != nil {
			return err
		}
		if got := pageValue(t, `document.getElementById("events").textContent`); !strings.Contains(got, "dblclick:place;") {
			return fmt.Errorf("events = %q, want a double-click on the Place order button", got)
		}
		return nil
	}},
	{"Hover", func(t *testing.T, f grammarForm) error {
		if err := testBrowser.Hover(context.Background(), f.button); err != nil {
			return err
		}
		if got := pageValue(t, `document.getElementById("events").textContent`); !strings.Contains(got, "hover:place;") {
			return fmt.Errorf("events = %q, want a hover on the Place order button", got)
		}
		return nil
	}},
	{"Fill", func(t *testing.T, f grammarForm) error {
		if err := testBrowser.Fill(context.Background(), f.input, "ring twice"); err != nil {
			return err
		}
		if got := pageValue(t, `document.getElementById("note").value`); got != "ring twice" {
			return fmt.Errorf("the note field reads %q, want what was typed", got)
		}
		return nil
	}},
	{"Drag", func(t *testing.T, f grammarForm) error {
		if err := testBrowser.Drag(context.Background(), f.button, f.input); err != nil {
			return err
		}
		got := pageValue(t, `document.getElementById("events").textContent`)
		if !strings.Contains(got, "dragstart:place;") || !strings.Contains(got, "drop:note;") ||
			strings.Contains(got, "decoy-button") {
			return fmt.Errorf("events = %q, want the Place order button dragged onto the note field", got)
		}
		return nil
	}},
}

func TestEveryTargetTakingAPIAcceptsTheSameGrammar(t *testing.T) {
	for _, api := range grammarAPIs {
		for _, form := range grammarForms {
			t.Run(api.name+"/"+form.name, func(t *testing.T) {
				openGrammarFixture(t)
				if err := api.check(t, form); err != nil {
					t.Errorf("%s through %s: %v", api.name, form.name, err)
				}
			})
		}
	}
}

// The repro from the report, as filed: the XPath a click resolves must be
// readable too.
func TestTextReadsAcceptXPath(t *testing.T) {
	resetFixture(t)
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/has_text.html"); err != nil {
		t.Fatal(err)
	}

	// Control: the same page is clickable through an XPath.
	if err := testBrowser.Click(context.Background(), `//button[normalize-space()="Sign in"]`, false); err != nil {
		t.Fatalf("control: click through XPath: %v", err)
	}

	got, err := testBrowser.GetTextContent(`//div[@class="row"]/span[contains(., "9 items")]`, "flat")
	if err != nil {
		t.Fatalf("reading text through XPath: %v", err)
	}
	if len(got.Groups) == 0 || got.Groups[0].Text != "Total: 9 items" {
		t.Fatalf("got %+v, want the span reading %q", got, "Total: 9 items")
	}
}

// An XPath that matches nothing is drift, exactly as a CSS selector that
// matches nothing is: repairable, and reported under the same error whichever
// API was asked.
func TestAnXPathThatMatchesNothingIsNotFoundEverywhere(t *testing.T) {
	const missing = `//section[@data-name="no-such-panel"]`

	t.Run("an action", func(t *testing.T) {
		openGrammarFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		defer cancel()
		if err := testBrowser.Click(ctx, missing, false); !errors.Is(err, ErrElementNotFound) {
			t.Errorf("Click: err = %v, want ErrElementNotFound", err)
		}
	})

	t.Run("a read", func(t *testing.T) {
		openGrammarFixture(t)
		if _, err := testBrowser.GetTextContent(missing, "flat"); !errors.Is(err, ErrElementNotFound) {
			t.Errorf("GetTextContent: err = %v, want ErrElementNotFound", err)
		}
	})

	t.Run("a read of every match", func(t *testing.T) {
		openGrammarFixture(t)
		_, err := testBrowser.GetMultipleComputedStyles(missing, nil)
		if err == nil {
			t.Fatal("GetMultipleComputedStyles: an XPath matching nothing was not an error")
		}
		if errors.Is(err, ErrInvalidSelector) {
			t.Errorf("GetMultipleComputedStyles: err = %v — a well-formed XPath was called malformed", err)
		}
	})

	t.Run("a batch read says it did not match", func(t *testing.T) {
		openGrammarFixture(t)
		results, err := testBrowser.GetBatchComputedStyles([]string{missing}, nil)
		if err != nil {
			t.Fatalf("GetBatchComputedStyles: %v", err)
		}
		if len(results) != 1 || results[0].Matched {
			t.Errorf("results = %+v, want one unmatched entry", results)
		}
	})
}

// An XPath the browser cannot parse can never match, so — like malformed CSS —
// it is a script defect and must not be retried. It used to surface as rod's
// raw evaluation error, which classify() reads as environmental.
func TestMalformedXPathIsReportedAsInvalidEverywhere(t *testing.T) {
	const malformed = `//section[@data-name=`

	calls := []struct {
		name string
		call func() error
	}{
		{"Click", func() error { return testBrowser.Click(context.Background(), malformed, false) }},
		{"WaitForElement", func() error {
			return testBrowser.WaitForElement(context.Background(), malformed, 2*time.Second)
		}},
		{"WaitForElementVisible", func() error {
			return testBrowser.WaitForElementVisible(context.Background(), malformed, 10*time.Second)
		}},
		{"GetTextContent", func() error {
			_, err := testBrowser.GetTextContent(malformed, "flat")
			return err
		}},
		{"GetComputedStyles", func() error {
			_, err := testBrowser.GetComputedStyles(malformed, nil)
			return err
		}},
		{"ScrollElement", func() error {
			_, err := testBrowser.ScrollElement(malformed, 0, 10, false, false)
			return err
		}},
		{"GetElementScreenshotByCSS", func() error {
			_, err := testBrowser.GetElementScreenshotByCSS(malformed)
			return err
		}},
		{"GetMultipleComputedStyles", func() error {
			_, err := testBrowser.GetMultipleComputedStyles(malformed, nil)
			return err
		}},
		{"DownloadImages", func() error {
			_, err := testBrowser.DownloadImages(malformed, false)
			return err
		}},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			openGrammarFixture(t)

			start := time.Now()
			err := c.call()
			if !errors.Is(err, ErrInvalidSelector) {
				t.Errorf("err = %v, want ErrInvalidSelector", err)
			}
			// It cannot match, so there is nothing to wait for.
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("took %v to refuse a selector that does not parse", elapsed.Round(time.Millisecond))
			}
		})
	}
}

// A read waits for its element exactly as an action does. The XPath branch is
// new to the read path, so it gets the same proof the CSS one already has.
func TestAReadThroughXPathWaitsForALateElement(t *testing.T) {
	resetFixture(t)
	// 800ms: well inside the three seconds a read with no deadline of its
	// own is given.
	if err := testBrowser.Navigate(context.Background(), testFixtureURL+"/late_element.html?manual=1&ms=800"); err != nil {
		t.Fatal(err)
	}

	// The clock starts here rather than at page load, so the delay is
	// measured from the same moment the lookup is.
	start := time.Now()
	if _, err := testBrowser.Evaluate("window.arm()"); err != nil {
		t.Fatal(err)
	}

	styles, err := testBrowser.GetComputedStyles(`//input[@id="username"]`, []string{"display"})
	if err != nil {
		t.Fatalf("gave up after %v: %v", time.Since(start).Round(time.Millisecond), err)
	}
	if styles["display"] == "" {
		t.Errorf("styles = %v, want the late input's", styles)
	}
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Errorf("returned in %v, before the element could exist — the test is not exercising the wait", elapsed)
	}
}
