package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/config"
)

var (
	testBrowser    *Browser
	testFixtureURL string
)

func TestMain(m *testing.M) {
	// Start fixture server
	fs := http.FileServer(http.Dir("testdata"))
	ts := httptest.NewServer(fs)
	testFixtureURL = ts.URL

	// Launch shared browser
	cfg := config.BrowserConfig{
		Headless:  true,
		NoSandbox: true,
	}
	b, err := New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create browser: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := b.Launch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to launch browser: %v\n", err)
		os.Exit(1)
	}
	testBrowser = b

	// Create initial page
	if err := b.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create page: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	b.Close()
	ts.Close()
	os.Exit(code)
}

// resetFixture navigates the shared browser to the fixture page,
// closing any extra tabs opened by previous tests.
func resetFixture(t *testing.T) {
	t.Helper()
	// Close extra pages (keep only the first one)
	for len(testBrowser.ListPages()) > 1 {
		testBrowser.ClosePage(len(testBrowser.ListPages()) - 1)
	}
	if len(testBrowser.ListPages()) > 0 {
		testBrowser.SelectPage(0)
	}
	ctx := context.Background()
	if err := testBrowser.Navigate(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		t.Fatalf("failed to navigate to fixture: %v", err)
	}
}

func TestBrowserCurrentURL(t *testing.T) {
	resetFixture(t)
	url := testBrowser.CurrentURL()
	if !strings.Contains(url, "test_fixture.html") {
		t.Errorf("CurrentURL() = %q, want to contain 'test_fixture.html'", url)
	}
}

func TestBrowserPageTitle(t *testing.T) {
	resetFixture(t)
	title := testBrowser.PageTitle()
	if title != "ATR Test Fixture" {
		t.Errorf("PageTitle() = %q, want 'ATR Test Fixture'", title)
	}
}

func TestBrowserScreenshot(t *testing.T) {
	resetFixture(t)
	data, err := testBrowser.Screenshot(false)
	if err != nil {
		t.Fatalf("Screenshot() error: %v", err)
	}
	if len(data) == 0 {
		t.Error("Screenshot() returned empty data")
	}
	if len(data) < 4 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		t.Error("Screenshot() did not return PNG data")
	}
}

func TestBrowserScreenshotFullPage(t *testing.T) {
	resetFixture(t)
	viewport, err := testBrowser.Screenshot(false)
	if err != nil {
		t.Fatalf("Screenshot(false) error: %v", err)
	}
	fullPage, err := testBrowser.Screenshot(true)
	if err != nil {
		t.Fatalf("Screenshot(true) error: %v", err)
	}
	if len(fullPage) < len(viewport) {
		t.Errorf("full page screenshot (%d bytes) smaller than viewport (%d bytes)", len(fullPage), len(viewport))
	}
}

func TestBrowserGetElementScreenshotByCSS(t *testing.T) {
	resetFixture(t)
	data, err := testBrowser.GetElementScreenshotByCSS("header")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('header') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("GetElementScreenshotByCSS returned empty data")
	}
	if len(data) < 4 || data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		t.Error("GetElementScreenshotByCSS did not return PNG data")
	}
}

func TestBrowserGetElementScreenshotByCSS_NotFound(t *testing.T) {
	resetFixture(t)
	_, err := testBrowser.GetElementScreenshotByCSS("#nonexistent-element")
	if err == nil {
		t.Error("expected error for nonexistent element, got nil")
	}
}

func TestBrowserSnapshot(t *testing.T) {
	resetFixture(t)
	elements, err := testBrowser.Snapshot(false)
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if len(elements) == 0 {
		t.Error("Snapshot() returned empty elements")
	}
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
	resetFixture(t)
	html, err := testBrowser.HTML()
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
	resetFixture(t)
	result, err := testBrowser.Evaluate("document.title")
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
	resetFixture(t)
	ctx := context.Background()
	if err := testBrowser.Click(ctx, "#test-button", false); err != nil {
		t.Fatalf("Click() error: %v", err)
	}
	result, err := testBrowser.Evaluate("document.getElementById('click-result').textContent")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result != "clicked" {
		t.Errorf("click result = %q, want 'clicked'", result)
	}
}

func TestBrowserFill(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	if err := testBrowser.Fill(ctx, "#test-input", "hello world"); err != nil {
		t.Fatalf("Fill() error: %v", err)
	}
	result, err := testBrowser.Evaluate("document.getElementById('test-input').value")
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("input value = %q, want 'hello world'", result)
	}
}

func TestBrowserWaitForElement(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	err := testBrowser.WaitForElement(ctx, "#delayed-element", 3*time.Second)
	if err != nil {
		t.Errorf("WaitForElement('#delayed-element') error: %v", err)
	}
}

func TestBrowserWaitForElement_Timeout(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	err := testBrowser.WaitForElement(ctx, "#nonexistent", 500*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error for nonexistent element")
	}
}

func TestBrowserWaitForElementVisible(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	err := testBrowser.WaitForElementVisible(ctx, "#delayed-element", 3*time.Second)
	if err != nil {
		t.Errorf("WaitForElementVisible('#delayed-element') error: %v", err)
	}
}

func TestBrowserWaitForElementVisible_Invisible(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	err := testBrowser.WaitForElementVisible(ctx, "#invisible-element", 2*time.Second)
	if err == nil {
		t.Error("expected error for invisible element, got nil")
	}
}

func TestBrowserGetComputedStylesDiff(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	if err := testBrowser.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		t.Fatalf("failed to open second page: %v", err)
	}
	result, err := testBrowser.GetComputedStylesDiff("#main-heading", 0, []string{"fontSize", "fontWeight", "color"}, "")
	if err != nil {
		t.Fatalf("GetComputedStylesDiff error: %v", err)
	}
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
	resetFixture(t)
	screenshots, err := testBrowser.GetMultipleElementScreenshots(".card")
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
	resetFixture(t)
	_, err := testBrowser.GetMultipleElementScreenshots(".nonexistent-class")
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserGetElementFullHeightScreenshot(t *testing.T) {
	resetFixture(t)
	normal, err := testBrowser.GetElementScreenshotByCSS("#scrollable-modal")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS error: %v", err)
	}
	fullHeight, err := testBrowser.GetElementFullHeightScreenshot("#scrollable-modal")
	if err != nil {
		t.Fatalf("GetElementFullHeightScreenshot error: %v", err)
	}
	if len(fullHeight) <= len(normal) {
		t.Errorf("full height screenshot (%d bytes) should be larger than normal (%d bytes)", len(fullHeight), len(normal))
	}
}

func TestBrowserGetTextContent_Structured(t *testing.T) {
	resetFixture(t)
	result, err := testBrowser.GetTextContent("footer", "structured")
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
	resetFixture(t)
	result, err := testBrowser.GetTextContent("footer", "flat")
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
	resetFixture(t)
	result, err := testBrowser.GetTextContent("footer", "links")
	if err != nil {
		t.Fatalf("GetTextContent links error: %v", err)
	}
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
	resetFixture(t)
	result, err := testBrowser.GetTextContent("footer", "headings")
	if err != nil {
		t.Fatalf("GetTextContent headings error: %v", err)
	}
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
	resetFixture(t)
	result, err := testBrowser.ScrollElement("#scrollable-modal", 0, 500, false, false)
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
	resetFixture(t)
	result, err := testBrowser.ScrollElement("#scrollable-modal", 0, 0, true, false)
	if err != nil {
		t.Fatalf("ScrollElement --to-bottom error: %v", err)
	}
	expectedBottom := result.ScrollHeight - result.ClientHeight
	if result.ScrollTop != expectedBottom {
		t.Errorf("scrollTop = %d, want %d (scrollHeight-clientHeight)", result.ScrollTop, expectedBottom)
	}
}

func TestBrowserScrollElement_ToTop(t *testing.T) {
	resetFixture(t)
	testBrowser.ScrollElement("#scrollable-modal", 0, 500, false, false)
	result, err := testBrowser.ScrollElement("#scrollable-modal", 0, 0, false, true)
	if err != nil {
		t.Fatalf("ScrollElement --to-top error: %v", err)
	}
	if result.ScrollTop != 0 {
		t.Errorf("scrollTop = %d, want 0", result.ScrollTop)
	}
}

func TestBrowserGetComputedStyles(t *testing.T) {
	resetFixture(t)
	styles, err := testBrowser.GetComputedStyles("#main-heading", nil)
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
	resetFixture(t)
	styles, err := testBrowser.GetComputedStyles("#main-heading", []string{"fontSize", "color"})
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
	resetFixture(t)
	_, err := testBrowser.GetComputedStyles("#nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserFindElementByCSS_TagName(t *testing.T) {
	resetFixture(t)
	data, err := testBrowser.GetElementScreenshotByCSS("footer")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('footer') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("footer screenshot returned empty data")
	}
}

func TestBrowserFindElementByCSS_Combinator(t *testing.T) {
	resetFixture(t)
	data, err := testBrowser.GetElementScreenshotByCSS("header nav")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('header nav') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("header nav screenshot returned empty data")
	}
}

func TestBrowserFindElementByCSS_PseudoSelector(t *testing.T) {
	resetFixture(t)
	data, err := testBrowser.GetElementScreenshotByCSS(".card:first-child")
	if err != nil {
		t.Fatalf("GetElementScreenshotByCSS('.card:first-child') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("card:first-child screenshot returned empty data")
	}
}
