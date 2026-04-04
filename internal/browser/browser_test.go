package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// serveFixture starts an HTTP server serving the testdata/ directory.
func serveFixture(t *testing.T) string {
	t.Helper()
	fs := http.FileServer(http.Dir("testdata"))
	ts := httptest.NewServer(fs)
	t.Cleanup(ts.Close)
	return ts.URL
}

// newTestBrowser creates a headless browser for testing.
func newTestBrowser(t *testing.T) *Browser {
	t.Helper()
	cfg := config.BrowserConfig{
		Headless:  true,
		NoSandbox: true,
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create browser: %v", err)
	}
	ctx := context.Background()
	if err := b.Launch(ctx); err != nil {
		t.Fatalf("failed to launch browser: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// navigateToFixture navigates the browser to the test fixture page.
// Creates a new page if the browser has none (same pattern as handleNavigate).
func navigateToFixture(t *testing.T, b *Browser, fixtureURL string) {
	t.Helper()
	ctx := context.Background()
	url := fixtureURL + "/test_fixture.html"
	pages := b.ListPages()
	if len(pages) == 0 {
		if err := b.NewPage(ctx, url); err != nil {
			t.Fatalf("failed to create page: %v", err)
		}
	} else {
		if err := b.Navigate(ctx, url); err != nil {
			t.Fatalf("failed to navigate to fixture: %v", err)
		}
	}
}

func TestBrowserCurrentURL(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	url := b.CurrentURL()
	if !strings.Contains(url, "test_fixture.html") {
		t.Errorf("CurrentURL() = %q, want to contain 'test_fixture.html'", url)
	}
}

func TestBrowserPageTitle(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	title := b.PageTitle()
	if title != "ATR Test Fixture" {
		t.Errorf("PageTitle() = %q, want 'ATR Test Fixture'", title)
	}
}

func TestBrowserScreenshot(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	data, err := b.Screenshot(false)
	if err != nil {
		t.Fatalf("Screenshot() error: %v", err)
	}
	if len(data) == 0 {
		t.Error("Screenshot() returned empty data")
	}
	// PNG magic bytes
	if len(data) < 4 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		t.Error("Screenshot() did not return PNG data")
	}
}

func TestBrowserScreenshotFullPage(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	viewport, err := b.Screenshot(false)
	if err != nil {
		t.Fatalf("Screenshot(false) error: %v", err)
	}

	fullPage, err := b.Screenshot(true)
	if err != nil {
		t.Fatalf("Screenshot(true) error: %v", err)
	}

	// Full page should be at least as large as viewport
	if len(fullPage) < len(viewport) {
		t.Errorf("full page screenshot (%d bytes) smaller than viewport (%d bytes)", len(fullPage), len(viewport))
	}
}

func TestBrowserGetElementScreenshotByCSS(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	data, err := b.GetElementScreenshotByCSS("header")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('header') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("GetElementScreenshotByCSS returned empty data")
	}
	// PNG magic bytes
	if len(data) < 4 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		t.Error("GetElementScreenshotByCSS did not return PNG data")
	}
}

func TestBrowserGetElementScreenshotByCSS_NotFound(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	_, err := b.GetElementScreenshotByCSS("#nonexistent-element")
	if err == nil {
		t.Error("expected error for nonexistent element, got nil")
	}
}

func TestBrowserSnapshot(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	elements, err := b.Snapshot(false)
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if len(elements) == 0 {
		t.Error("Snapshot() returned empty elements")
	}

	// Should find interactive elements like links, buttons, inputs
	foundButton := false
	foundInput := false
	for _, el := range elements {
		if el.TagName == "button" {
			foundButton = true
		}
		if el.TagName == "input" {
			foundInput = true
		}
	}
	if !foundButton {
		t.Error("Snapshot() did not find button element")
	}
	if !foundInput {
		t.Error("Snapshot() did not find input element")
	}
}

func TestBrowserHTML(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	html, err := b.HTML()
	if err != nil {
		t.Fatalf("HTML() error: %v", err)
	}
	if !strings.Contains(html, "ATR Test Fixture") {
		t.Error("HTML() does not contain expected title")
	}
	if !strings.Contains(html, "main-heading") {
		t.Error("HTML() does not contain expected element id")
	}
}

func TestBrowserEvaluate(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.Evaluate("document.title")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	title, ok := result.(string)
	if !ok {
		t.Fatalf("Evaluate() result is %T, want string", result)
	}
	if title != "ATR Test Fixture" {
		t.Errorf("Evaluate('document.title') = %q, want 'ATR Test Fixture'", title)
	}
}

func TestBrowserClick(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()
	if err := b.Click(ctx, "#test-button", false); err != nil {
		t.Fatalf("Click() error: %v", err)
	}

	// Verify click result appeared
	result, err := b.Evaluate("document.getElementById('click-result').textContent")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result != "clicked" {
		t.Errorf("click result = %q, want 'clicked'", result)
	}
}

func TestBrowserFill(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()
	if err := b.Fill(ctx, "#test-input", "hello world"); err != nil {
		t.Fatalf("Fill() error: %v", err)
	}

	result, err := b.Evaluate("document.getElementById('test-input').value")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("input value = %q, want 'hello world'", result)
	}
}

func TestBrowserWaitForElement(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()

	// Element that appears after 500ms delay
	err := b.WaitForElement(ctx, "#delayed-element", 3*time.Second)
	if err != nil {
		t.Errorf("WaitForElement('#delayed-element') error: %v", err)
	}
}

func TestBrowserWaitForElement_Timeout(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()

	// Element that doesn't exist — should timeout
	err := b.WaitForElement(ctx, "#nonexistent", 500*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error for nonexistent element")
	}
}

func TestBrowserWaitForElementVisible(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()

	// #delayed-element appears after 500ms and is visible
	err := b.WaitForElementVisible(ctx, "#delayed-element", 3*time.Second)
	if err != nil {
		t.Errorf("WaitForElementVisible('#delayed-element') error: %v", err)
	}
}

func TestBrowserWaitForElementVisible_Invisible(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	ctx := context.Background()

	// #invisible-element exists but is display:none — should fail visibility check
	err := b.WaitForElementVisible(ctx, "#invisible-element", 2*time.Second)
	if err == nil {
		t.Error("expected error for invisible element, got nil")
	}
}

func TestBrowserGetComputedStylesDiff(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Open same fixture in a second tab
	ctx := context.Background()
	if err := b.NewPage(ctx, fixtureURL+"/test_fixture.html"); err != nil {
		t.Fatalf("failed to open second page: %v", err)
	}

	// Now on page 1 (second tab), compare h1 against page 0 (first tab)
	result, err := b.GetComputedStylesDiff("#main-heading", 0, []string{"fontSize", "fontWeight", "color"}, "")
	if err != nil {
		t.Fatalf("GetComputedStylesDiff error: %v", err)
	}

	// Same page, same selector — should be 100% match
	if result.MismatchCount != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", result.MismatchCount, result.Mismatches)
	}
	if result.Score != 100 {
		t.Errorf("score = %f, want 100", result.Score)
	}
	if result.MatchCount != 3 {
		t.Errorf("matchCount = %d, want 3", result.MatchCount)
	}
}

func TestBrowserGetMultipleElementScreenshots(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	screenshots, err := b.GetMultipleElementScreenshots(".card")
	if err != nil {
		t.Fatalf("GetMultipleElementScreenshots error: %v", err)
	}
	if len(screenshots) != 3 {
		t.Errorf("expected 3 screenshots, got %d", len(screenshots))
	}
	for i, data := range screenshots {
		if len(data) == 0 {
			t.Errorf("screenshot %d is empty", i)
		}
	}
}

func TestBrowserGetMultipleElementScreenshots_NotFound(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	_, err := b.GetMultipleElementScreenshots(".nonexistent-class")
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserGetElementFullHeightScreenshot(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Normal element screenshot (clipped to visible area)
	normal, err := b.GetElementScreenshotByCSS("#scrollable-modal")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS error: %v", err)
	}

	// Full height screenshot (expanded)
	fullHeight, err := b.GetElementFullHeightScreenshot("#scrollable-modal")
	if err != nil {
		t.Fatalf("GetElementFullHeightScreenshot error: %v", err)
	}

	// Full height should be larger since the element has 1000px content in 200px container
	if len(fullHeight) <= len(normal) {
		t.Errorf("full height screenshot (%d bytes) should be larger than normal (%d bytes)", len(fullHeight), len(normal))
	}
}

func TestBrowserGetTextContent_Structured(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.GetTextContent("footer", "structured")
	if err != nil {
		t.Fatalf("GetTextContent error: %v", err)
	}
	if len(result.Groups) == 0 {
		t.Error("expected non-empty text groups")
	}
	if result.Mode != "structured" {
		t.Errorf("mode = %q, want 'structured'", result.Mode)
	}
}

func TestBrowserGetTextContent_Flat(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.GetTextContent("footer", "flat")
	if err != nil {
		t.Fatalf("GetTextContent flat error: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Errorf("flat mode should return 1 group, got %d", len(result.Groups))
	}
	if !strings.Contains(result.Groups[0].Text, "Contact") {
		t.Error("flat text should contain 'Contact'")
	}
}

func TestBrowserGetTextContent_Links(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.GetTextContent("footer", "links")
	if err != nil {
		t.Fatalf("GetTextContent links error: %v", err)
	}
	// Footer has 4 links: test@example.com, Privacy Policy, Terms of Service, Help Center
	if len(result.Groups) < 3 {
		t.Errorf("expected at least 3 links, got %d", len(result.Groups))
	}
	for _, g := range result.Groups {
		if g.Tag != "a" {
			t.Errorf("expected tag 'a', got %q", g.Tag)
		}
		if g.Href == "" {
			t.Error("expected href on link")
		}
	}
}

func TestBrowserGetTextContent_Headings(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.GetTextContent("footer", "headings")
	if err != nil {
		t.Fatalf("GetTextContent headings error: %v", err)
	}
	// Footer has 2 h4 headings: Contact, Links
	if len(result.Groups) != 2 {
		t.Errorf("expected 2 headings, got %d", len(result.Groups))
	}
	for _, g := range result.Groups {
		if g.Tag != "h4" {
			t.Errorf("expected tag 'h4', got %q", g.Tag)
		}
	}
}

func TestBrowserScrollElement(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.ScrollElement("#scrollable-modal", 0, 500, false, false)
	if err != nil {
		t.Fatalf("ScrollElement error: %v", err)
	}
	if result.ScrollTop != 500 {
		t.Errorf("scrollTop = %d, want 500", result.ScrollTop)
	}
	if result.ScrollHeight <= result.ClientHeight {
		t.Error("scrollHeight should be > clientHeight for scrollable element")
	}
}

func TestBrowserScrollElement_ToBottom(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	result, err := b.ScrollElement("#scrollable-modal", 0, 0, true, false)
	if err != nil {
		t.Fatalf("ScrollElement --to-bottom error: %v", err)
	}
	expectedBottom := result.ScrollHeight - result.ClientHeight
	if result.ScrollTop != expectedBottom {
		t.Errorf("scrollTop = %d, want %d (scrollHeight-clientHeight)", result.ScrollTop, expectedBottom)
	}
}

func TestBrowserScrollElement_ToTop(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// First scroll down
	b.ScrollElement("#scrollable-modal", 0, 500, false, false)

	// Then scroll to top
	result, err := b.ScrollElement("#scrollable-modal", 0, 0, false, true)
	if err != nil {
		t.Fatalf("ScrollElement --to-top error: %v", err)
	}
	if result.ScrollTop != 0 {
		t.Errorf("scrollTop = %d, want 0", result.ScrollTop)
	}
}

func TestBrowserGetComputedStyles(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Get default properties for h1 with known inline styles
	styles, err := b.GetComputedStyles("#main-heading", nil)
	if err != nil {
		t.Fatalf("GetComputedStyles error: %v", err)
	}
	if len(styles) == 0 {
		t.Error("GetComputedStyles returned empty map")
	}
	if fs, ok := styles["fontSize"]; !ok || fs != "32px" {
		t.Errorf("fontSize = %q, want '32px'", fs)
	}
	if fw, ok := styles["fontWeight"]; !ok || fw != "700" {
		t.Errorf("fontWeight = %q, want '700'", fw)
	}
}

func TestBrowserGetComputedStyles_FilterProperties(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	styles, err := b.GetComputedStyles("#main-heading", []string{"fontSize", "color"})
	if err != nil {
		t.Fatalf("GetComputedStyles error: %v", err)
	}
	if len(styles) != 2 {
		t.Errorf("expected 2 properties, got %d: %v", len(styles), styles)
	}
	if _, ok := styles["fontSize"]; !ok {
		t.Error("expected fontSize in filtered results")
	}
	if _, ok := styles["color"]; !ok {
		t.Error("expected color in filtered results")
	}
}

func TestBrowserGetComputedStyles_NotFound(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	_, err := b.GetComputedStyles("#nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserFindElementByCSS_TagName(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Test that bare tag names now work via looksLikeCSSSelector
	data, err := b.GetElementScreenshotByCSS("footer")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('footer') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("footer screenshot returned empty data")
	}
}

func TestBrowserFindElementByCSS_Combinator(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Test complex CSS selector with combinator
	data, err := b.GetElementScreenshotByCSS("header nav")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('header nav') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("header nav screenshot returned empty data")
	}
}

func TestBrowserFindElementByCSS_PseudoSelector(t *testing.T) {
	fixtureURL := serveFixture(t)
	b := newTestBrowser(t)
	navigateToFixture(t, b, fixtureURL)

	// Test CSS selector with pseudo-selector
	data, err := b.GetElementScreenshotByCSS(".card:first-child")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('.card:first-child') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("card:first-child screenshot returned empty data")
	}
}
