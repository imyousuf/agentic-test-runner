package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"

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
	// Close extra pages (keep only the first one). Index rather than length:
	// a listing can leave a gap where a page did not answer.
	for pages := testBrowser.ListPages(); len(pages) > 1; pages = testBrowser.ListPages() {
		testBrowser.ClosePage(pages[len(pages)-1].Index)
	}
	if len(testBrowser.ListPages()) > 0 {
		testBrowser.SelectPage(0)
	}
	ctx := context.Background()
	if err := testBrowser.Navigate(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		t.Fatalf("failed to navigate to fixture: %v", err)
	}
	// Reset scroll position — browser may preserve it across navigations
	testBrowser.Evaluate("window.scrollTo(0,0)")
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
	if runtime.GOOS == "windows" {
		// -race overhead + Chromium CDP latency on Windows runners makes
		// the per-element screenshot context flake; the explicit-timeout
		// variant below stays green.
		t.Skip("flaky on Windows under -race: CDP latency causes per-element context deadlines")
	}
	resetFixture(t)
	results, err := testBrowser.GetMultipleElementScreenshots(".card")
	if err != nil {
		t.Fatalf("GetMultipleElementScreenshots error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Error != "" {
			t.Errorf("screenshot %d failed: %s", r.Index, r.Error)
		}
		if len(r.Data) == 0 {
			t.Errorf("screenshot %d is empty", r.Index)
		}
	}
}

func TestBrowserGetMultipleElementScreenshots_WithTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Same Windows-under-race CDP-latency flake as the default-timeout
		// variant above — the per-element timeout still trips even at 30s
		// when -race + cold Chromium overhead stack up.
		t.Skip("flaky on Windows under -race: CDP latency causes per-element context deadlines")
	}
	resetFixture(t)
	// Use a generous timeout — all 3 cards should succeed
	results, err := testBrowser.GetMultipleElementScreenshots(".card", 30*time.Second)
	if err != nil {
		t.Fatalf("GetMultipleElementScreenshots error: %v", err)
	}
	succeeded := 0
	for _, r := range results {
		if r.Error == "" {
			succeeded++
		}
	}
	if succeeded != 3 {
		t.Errorf("expected 3 successful screenshots, got %d", succeeded)
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

func TestBrowserGetElementFullHeightScreenshot_NonScrollable(t *testing.T) {
	resetFixture(t)
	// header has no overflow scroll — full height should still work without timeout
	data, err := testBrowser.GetElementFullHeightScreenshot("header")
	if err != nil {
		t.Fatalf("GetElementFullHeightScreenshot('header') error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty screenshot data")
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

func TestBrowserScrollElement_PageLevel(t *testing.T) {
	resetFixture(t)
	// Use page.Eval to scroll directly and verify ScrollElement uses window.scrollTo
	// First ensure we're at top via eval (avoids browser state issues)
	testBrowser.Evaluate("window.scrollTo(0,0)")

	result, err := testBrowser.ScrollElement("body", 0, 100, false, false)
	if err != nil {
		t.Fatalf("ScrollElement('body') error: %v", err)
	}

	// The returned scrollTop should reflect window.scrollY, not element.scrollTop
	// Verify by checking that scrollTop matches what we get from window.scrollY
	scrollY, err := testBrowser.Evaluate("window.scrollY")
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	sy, ok := scrollY.(float64)
	if !ok {
		t.Fatalf("window.scrollY is %T, want float64", scrollY)
	}
	if int(sy) != result.ScrollTop {
		t.Errorf("window.scrollY = %d, ScrollElement returned scrollTop = %d (should match)", int(sy), result.ScrollTop)
	}
	// Verify page actually scrolled by checking scrollHeight > clientHeight
	if result.ScrollHeight <= result.ClientHeight {
		t.Skip("page not tall enough to scroll — skipping scroll assertion")
	}
	if result.ScrollTop == 0 && result.ScrollHeight > result.ClientHeight {
		t.Error("page is scrollable but scrollTop = 0 after scrollTo(0, 100)")
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

func TestBrowserDownloadImages_WithImgs(t *testing.T) {
	resetFixture(t)
	results, err := testBrowser.DownloadImages("#image-section", false)
	if err != nil {
		t.Fatalf("DownloadImages error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 images, got %d", len(results))
	}
	for _, r := range results {
		if r.Error != "" {
			t.Errorf("image %d error: %s", r.Index, r.Error)
		}
		if r.Method != "download" {
			t.Errorf("image %d method = %q, want 'download'", r.Index, r.Method)
		}
		if len(r.Data) == 0 {
			t.Errorf("image %d has empty data", r.Index)
		}
	}
}

func TestBrowserDownloadImages_FallbackScreenshot(t *testing.T) {
	resetFixture(t)
	results, err := testBrowser.DownloadImages("#no-images-section .visual-card", true)
	if err != nil {
		t.Fatalf("DownloadImages fallback error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(results))
	}
	for _, r := range results {
		if r.Error != "" {
			t.Errorf("screenshot %d error: %s", r.Index, r.Error)
		}
		if r.Method != "screenshot" {
			t.Errorf("screenshot %d method = %q, want 'screenshot'", r.Index, r.Method)
		}
	}
}

func TestBrowserDownloadImages_NoImgsNoFallback(t *testing.T) {
	resetFixture(t)
	_, err := testBrowser.DownloadImages("#no-images-section .visual-card", false)
	if err == nil {
		t.Error("expected error when no <img> tags and no fallback")
	}
}

func TestBrowserDownloadImages_NotFound(t *testing.T) {
	resetFixture(t)
	_, err := testBrowser.DownloadImages("#nonexistent-section", false)
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserGetCleanSnapshot(t *testing.T) {
	resetFixture(t)
	html, tree, err := testBrowser.GetCleanSnapshot("footer", CleanSnapshotOptions{})
	if err != nil {
		t.Fatalf("GetCleanSnapshot error: %v", err)
	}
	if html == "" {
		t.Error("expected non-empty HTML output")
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.Tag != "footer" {
		t.Errorf("tree tag = %q, want 'footer'", tree.Tag)
	}
	if !strings.Contains(html, "<footer") {
		t.Error("HTML should contain <footer")
	}
	if !strings.Contains(html, "Contact") {
		t.Error("HTML should contain 'Contact' text")
	}
	// Should NOT contain script tags
	if strings.Contains(html, "<script") {
		t.Error("HTML should not contain script tags")
	}
}

func TestBrowserGetCleanSnapshot_WithDepth(t *testing.T) {
	resetFixture(t)
	html, _, err := testBrowser.GetCleanSnapshot("footer", CleanSnapshotOptions{Depth: 1})
	if err != nil {
		t.Fatalf("GetCleanSnapshot depth error: %v", err)
	}
	if !strings.Contains(html, "children") {
		t.Error("depth-limited output should contain 'children' placeholder")
	}
}

func TestBrowserGetCleanSnapshot_NotFound(t *testing.T) {
	resetFixture(t)
	_, _, err := testBrowser.GetCleanSnapshot("#nonexistent-thing", CleanSnapshotOptions{})
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestBrowserGetCleanSnapshot_JSON(t *testing.T) {
	resetFixture(t)
	_, tree, err := testBrowser.GetCleanSnapshot("header", CleanSnapshotOptions{})
	if err != nil {
		t.Fatalf("GetCleanSnapshot error: %v", err)
	}
	if tree.Tag != "header" {
		t.Errorf("tree tag = %q, want 'header'", tree.Tag)
	}
	// Header should have children (nav with links)
	if len(tree.Children) == 0 {
		t.Error("expected header to have children")
	}
}

func TestBrowserGetViewport(t *testing.T) {
	resetFixture(t)
	vp, err := testBrowser.GetViewport()
	if err != nil {
		t.Fatalf("GetViewport error: %v", err)
	}
	if vp.Width == 0 || vp.Height == 0 {
		t.Errorf("viewport dimensions should be non-zero: %dx%d", vp.Width, vp.Height)
	}
}

func TestBrowserSetViewport(t *testing.T) {
	resetFixture(t)
	prev, current, err := testBrowser.SetViewport(800, 600)
	if err != nil {
		t.Fatalf("SetViewport error: %v", err)
	}
	if prev == nil {
		t.Error("expected previous viewport")
	}
	if current.Width != 800 || current.Height != 600 {
		t.Errorf("current = %dx%d, want 800x600", current.Width, current.Height)
	}

	// Verify via GetViewport
	vp, err := testBrowser.GetViewport()
	if err != nil {
		t.Fatalf("GetViewport error: %v", err)
	}
	if vp.Width != 800 {
		t.Errorf("GetViewport width = %d, want 800", vp.Width)
	}
}

func TestBrowserSetViewport_WithDPR(t *testing.T) {
	resetFixture(t)
	_, current, err := testBrowser.SetViewport(375, 812, 2.0)
	if err != nil {
		t.Fatalf("SetViewport error: %v", err)
	}
	if current.DeviceScaleFactor != 2.0 {
		t.Errorf("dpr = %f, want 2.0", current.DeviceScaleFactor)
	}
}

func TestBrowserSetViewport_OutOfRange(t *testing.T) {
	resetFixture(t)
	_, _, err := testBrowser.SetViewport(100, 600)
	if err == nil {
		t.Error("expected error for width < 320")
	}
	_, _, err = testBrowser.SetViewport(800, 100)
	if err == nil {
		t.Error("expected error for height < 480")
	}
}

func TestBrowserCheckFont_NotFound(t *testing.T) {
	resetFixture(t)
	result, err := testBrowser.CheckFont("NonExistentFontXYZ")
	if err != nil {
		t.Fatalf("CheckFont error: %v", err)
	}
	if result.Family != "NonExistentFontXYZ" {
		t.Errorf("family = %q, want 'NonExistentFontXYZ'", result.Family)
	}
	if result.Loaded {
		t.Error("expected loaded=false for nonexistent font")
	}
	if result.Status == "loaded" {
		t.Error("expected status != 'loaded' for nonexistent font")
	}
}

func TestBrowserCheckFont_SystemFont(t *testing.T) {
	resetFixture(t)
	// sans-serif is always available as a generic family
	result, err := testBrowser.CheckFont("sans-serif")
	if err != nil {
		t.Fatalf("CheckFont error: %v", err)
	}
	if result.Family != "sans-serif" {
		t.Errorf("family = %q, want 'sans-serif'", result.Family)
	}
	// sans-serif should pass document.fonts.check
	if !result.Loaded {
		t.Log("sans-serif reported as not loaded — document.fonts.check behavior may vary")
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

func TestBrowserGetComputedStyles_FontRenderingDefaults(t *testing.T) {
	resetFixture(t)
	styles, err := testBrowser.GetComputedStyles("#main-heading", nil)
	if err != nil {
		t.Fatalf("GetComputedStyles error: %v", err)
	}
	for _, prop := range []string{"fontFeatureSettings", "textRendering", "fontKerning"} {
		if _, ok := styles[prop]; !ok {
			t.Errorf("expected %q in default computed styles", prop)
		}
	}
}

func TestBrowserGetBatchComputedStyles(t *testing.T) {
	resetFixture(t)
	results, err := testBrowser.GetBatchComputedStyles(
		[]string{"h1", ".card h3", "#nonexistent"},
		nil,
	)
	if err != nil {
		t.Fatalf("GetBatchComputedStyles error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// h1 should match
	if !results[0].Matched {
		t.Error("h1 should be matched")
	}
	if _, ok := results[0].Styles["fontSize"]; !ok {
		t.Error("h1 should have fontSize")
	}
	if results[0].Element == "" {
		t.Error("h1 should have element description")
	}
	// .card h3 should match
	if !results[1].Matched {
		t.Error(".card h3 should be matched")
	}
	// #nonexistent should NOT match
	if results[2].Matched {
		t.Error("#nonexistent should not be matched")
	}
}

func TestBrowserGetBatchComputedStyles_WithProperties(t *testing.T) {
	resetFixture(t)
	results, err := testBrowser.GetBatchComputedStyles(
		[]string{"h1", "footer"},
		[]string{"fontSize", "color"},
	)
	if err != nil {
		t.Fatalf("GetBatchComputedStyles error: %v", err)
	}
	for _, r := range results {
		if r.Matched && len(r.Styles) != 2 {
			t.Errorf("selector %q: expected 2 properties, got %d", r.Selector, len(r.Styles))
		}
	}
}

func TestBrowserGetBatchComputedStylesDiff(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()
	if err := testBrowser.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		t.Fatalf("failed to open second page: %v", err)
	}
	result, err := testBrowser.GetBatchComputedStylesDiff(
		[]string{"h1", "#nonexistent", "footer"},
		0,
		[]string{"fontSize", "fontWeight"},
		"",
	)
	if err != nil {
		t.Fatalf("GetBatchComputedStylesDiff error: %v", err)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}
	// h1 should match with 100% score (same page fixture)
	if !result.Results[0].Matched {
		t.Error("h1 should be matched")
	}
	if result.Results[0].Score != 100 {
		t.Errorf("h1 score = %f, want 100", result.Results[0].Score)
	}
	// #nonexistent should not match
	if result.Results[1].Matched {
		t.Error("#nonexistent should not be matched")
	}
	// footer should match
	if !result.Results[2].Matched {
		t.Error("footer should be matched")
	}
	// Overall score should be average of matched selectors
	if result.OverallScore != 100 {
		t.Errorf("overall score = %f, want 100", result.OverallScore)
	}
}

func TestBrowserGetMultipleComputedStyles(t *testing.T) {
	resetFixture(t)
	entries, err := testBrowser.GetMultipleComputedStyles(".card h3", nil)
	if err != nil {
		t.Fatalf("GetMultipleComputedStyles error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if len(e.Styles) == 0 {
			t.Errorf("entry %d has empty styles", e.Index)
		}
		if _, ok := e.Styles["fontSize"]; !ok {
			t.Errorf("entry %d missing fontSize", e.Index)
		}
	}
	// Verify text is populated
	if entries[0].Text == "" {
		t.Error("expected non-empty text on first entry")
	}
}

func TestBrowserGetMultipleComputedStyles_WithProperties(t *testing.T) {
	resetFixture(t)
	entries, err := testBrowser.GetMultipleComputedStyles(".card h3", []string{"fontSize", "color"})
	if err != nil {
		t.Fatalf("GetMultipleComputedStyles error: %v", err)
	}
	for _, e := range entries {
		if len(e.Styles) != 2 {
			t.Errorf("entry %d expected 2 properties, got %d", e.Index, len(e.Styles))
		}
	}
}

func TestBrowserGetMultipleComputedStyles_NotFound(t *testing.T) {
	resetFixture(t)
	_, err := testBrowser.GetMultipleComputedStyles(".nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent selector")
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

// A page can die without the daemon hearing about it — a tab closed from
// inside the browser, a crashed renderer, a persisted profile reopened with
// stale targets. Asking such a page for its Info fails, and the Must form of
// that call panics. On the daemon that panic kills the HTTP server, and the
// client sees an unexplained EOF rather than an error.
//
// The dead page is put back into the bookkeeping by hand, after the browser's
// own targetDestroyed event has been allowed to arrive and be applied. Closing
// a tab and listing straight afterwards races that event: the worker usually
// removes the page first, so the listing is correct whether or not ListPages
// handles a dead page at all, and the test passes against code that panics in
// production.
func TestListPagesSurvivesADeadPage(t *testing.T) {
	resetFixture(t)
	ctx := context.Background()

	// Chrome intermittently brings up a target whose renderer answers nothing
	// — see ErrRendererUnresponsive. It is a browser-side condition this test
	// is not about, and it cannot be retried around: a fresh same-origin tab
	// lands in the very renderer that is stuck. Skipping on that one typed
	// error keeps a real defect failing.
	if err := testBrowser.NewPage(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		skipIfRendererWedged(t, err)
		t.Fatalf("opening a second tab: %v", err)
	}

	testBrowser.mu.RLock()
	victim := testBrowser.pages[len(testBrowser.pages)-1]
	testBrowser.mu.RUnlock()

	// Close the tab behind the daemon's back, the way a user closing a tab in
	// the browser window does.
	if err := victim.Close(); err != nil {
		t.Fatalf("closing the page directly: %v", err)
	}
	if _, err := victim.Info(); err == nil {
		t.Fatal("the page still answers after Close, so this test would prove nothing")
	}

	// Let the targetDestroyed event land, so what follows is not racing it.
	deadline := time.Now().Add(10 * time.Second)
	for tracked(victim) {
		if time.Now().After(deadline) {
			t.Fatal("the browser never reported the closed tab")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Now put the dead page back, which is the state the event cannot produce:
	// a page in the bookkeeping that is gone, with nothing on its way to say
	// so. Selecting it as well pins the fallback — the daemon must not be
	// left pointing at a tab that no longer exists.
	testBrowser.mu.Lock()
	testBrowser.pages = append(testBrowser.pages, victim)
	testBrowser.targetIDs[victim.TargetID] = victim
	testBrowser.ownedPages[victim] = true
	testBrowser.current = len(testBrowser.pages) - 1
	before := len(testBrowser.pages)
	testBrowser.mu.Unlock()

	// Must not panic, and must drop the dead entry.
	got := testBrowser.ListPages()
	if len(got) != before-1 {
		t.Errorf("ListPages returned %d pages, want %d after one died", len(got), before-1)
	}
	for _, p := range got {
		if p.URL == "" {
			t.Errorf("a dead page was reported instead of dropped: %+v", p)
		}
	}
	if tracked(victim) {
		t.Error("the dead page is still in the bookkeeping; nothing else will ever remove it")
	}

	// Every index the listing reports has to be one the daemon still accepts,
	// and exactly one of them has to be the selected tab.
	currents := 0
	for _, p := range got {
		if err := testBrowser.SelectPage(p.Index); err != nil {
			t.Errorf("ListPages reported index %d, which SelectPage rejects: %v", p.Index, err)
		}
		if p.Current {
			currents++
		}
	}
	if len(got) > 0 && currents != 1 {
		t.Errorf("the listing marked %d tabs as current, want exactly 1", currents)
	}

	// The browser must still be usable afterwards.
	if err := testBrowser.Navigate(ctx, testFixtureURL+"/test_fixture.html"); err != nil {
		skipIfRendererWedged(t, err)
		t.Errorf("browser unusable after a dead page: %v", err)
	}
}

// tracked reports whether the daemon still has the page in its bookkeeping.
func tracked(page *rod.Page) bool {
	testBrowser.mu.RLock()
	defer testBrowser.mu.RUnlock()
	for _, p := range testBrowser.pages {
		if p == page {
			return true
		}
	}
	return false
}

// Chrome rejects a URL with no scheme, which is not a reasonable answer to
// something every address bar accepts.
func TestNormalizeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"example.com", "https://example.com"},
		{"example.com/path", "https://example.com/path"},
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"about:blank", "about:blank"},
		{"file:///tmp/x.html", "file:///tmp/x.html"},
		{"data:text/html,hi", "data:text/html,hi"},
		// Loopback is overwhelmingly plain HTTP; https would give a TLS error
		// that reads as if the dev server were broken.
		{"localhost:3000", "http://localhost:3000"},
		{"localhost", "http://localhost"},
		{"127.0.0.1:8080/app", "http://127.0.0.1:8080/app"},
		{"example.com:8443/x", "https://example.com:8443/x"},
		{"", ""},

		// A path is not a host. Giving one a scheme turned a local file into
		// a request to a name someone else can register.
		{"login.html", "login.html"},
		{"./login.html", "./login.html"},
		{"../pages/login.html", "../pages/login.html"},
		{"/tmp/x.html", "/tmp/x.html"},
		{"pages/login.htm", "pages/login.htm"},
		{"fixtures", "fixtures"},
		{"fixtures/page", "fixtures/page"},
		// A port says it is a machine after all.
		{"buildbox:8080", "https://buildbox:8080"},
		{"buildbox:8080/app", "https://buildbox:8080/app"},
		// A real host that merely serves a page keeps its scheme: the last
		// label is what decides, and "com" is a domain while "html" is not.
		{"example.com/login.html", "https://example.com/login.html"},
	}
	for _, tt := range tests {
		if got := normalizeURL(tt.in); got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A persisted profile keeps Chrome's single-instance lock, and stopping the
// daemon kills Chrome outright — so the lock routinely survives pointing at a
// process that no longer exists. The next Chrome sees it, tries to hand the
// profile to an instance that is gone, and exits before rod can talk to it:
// the daemon comes up with no browser behind it and the first command fails
// with a bare EOF.
func TestClearStaleProfileLock(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}

	// Creating a symlink on Windows needs SeCreateSymbolicLinkPrivilege, which
	// the release runner does not have, and the lock this reasons about is a
	// symlink Chrome writes. Where one cannot be made there is nothing here to
	// test, and failing would report a missing privilege as a defect.
	if err := os.Symlink("target", filepath.Join(t.TempDir(), "probe")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	write := func(t *testing.T, dir, target string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, "SingletonLock")); err != nil {
			t.Fatal(err)
		}
		for _, extra := range []string{"SingletonCookie", "SingletonSocket"} {
			if err := os.Symlink("whatever", filepath.Join(dir, extra)); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(dir, name string) bool {
		_, err := os.Lstat(filepath.Join(dir, name))
		return err == nil
	}

	t.Run("dead pid is cleared", func(t *testing.T) {
		dir := t.TempDir()
		// PID 2^30 is far above any real pid on Linux.
		write(t, dir, fmt.Sprintf("%s-%d", host, 1<<30))

		clearStaleProfileLock(dir)

		for _, name := range singletonFiles {
			if exists(dir, name) {
				t.Errorf("%s survived a stale lock", name)
			}
		}
	})

	// A genuinely running second instance must keep its profile.
	t.Run("live pid is left alone", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, fmt.Sprintf("%s-%d", host, os.Getpid()))

		clearStaleProfileLock(dir)

		if !exists(dir, "SingletonLock") {
			t.Error("a live lock was deleted; that would corrupt the running instance's profile")
		}
	})

	// A profile on shared storage may be locked by another machine whose
	// pids mean nothing here.
	t.Run("another host is left alone", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, fmt.Sprintf("%s-%d", "some-other-host", 1<<30))

		clearStaleProfileLock(dir)

		if !exists(dir, "SingletonLock") {
			t.Error("a lock from another host was deleted")
		}
	})

	t.Run("no lock is not an error", func(t *testing.T) {
		clearStaleProfileLock(t.TempDir())
	})
}

// Only an error that says the tab is gone may evict it. A timeout or a dropped
// message says nothing about whether the tab is still open, and forgetting one
// on that basis strands it: still open, unreachable by every accessor, and no
// longer closed by Close().
func TestTargetGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"closed target", &cdp.Error{Code: -32602, Message: "No target with given id found"}, true},
		{"closed session", &cdp.Error{Code: -32001, Message: "Session with given id not found"}, true},
		{"wrapped", fmt.Errorf("reading info: %w", &cdp.Error{Message: "No target with given id found"}), true},
		{"other cdp failure", &cdp.Error{Code: -32000, Message: "Runtime.evaluate timed out"}, false},
		{"deadline", context.DeadlineExceeded, false},
		{"plain", errors.New("websocket: close 1006 (abnormal closure)"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := targetGone(tc.err); got != tc.want {
			t.Errorf("targetGone(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// waitLoad is what keeps a stalled page from becoming a stalled daemon. Every
// caller that holds b.mu across it — StartRecording does — depends on this
// returning.
func TestWaitLoadIsBounded(t *testing.T) {
	resetFixture(t)
	// The tab this opens never finishes loading. Leaving it in the shared
	// browser would hand every later test a target that is still navigating.
	t.Cleanup(func() { resetFixture(t) })

	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold the response open, so the document never finishes and the load
		// event never fires.
		<-hang
	}))
	defer func() {
		close(hang)
		srv.Close()
	}()

	testBrowser.config.PageTimeout = time.Second
	defer func() { testBrowser.config.PageTimeout = 0 }()

	done := make(chan error, 1)
	go func() { done <- testBrowser.NewPage(context.Background(), srv.URL) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a page that never loads must not report success")
		}
		if !strings.Contains(err.Error(), "did not finish loading") {
			t.Errorf("error = %v, want it to name the load timeout", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("waiting for a page that never loads did not come back; anything holding b.mu across this hangs the daemon")
	}
}

// WaitForText takes text a person typed, and ElementR takes a regular
// expression. Without quoting, ordinary punctuation is read as syntax: this
// pattern matches "Sign up free" and not the string actually on the page.
func TestWaitForTextTakesLiteralText(t *testing.T) {
	resetFixture(t)

	// This navigates the shared page away from the fixture.
	t.Cleanup(func() { resetFixture(t) })

	const phrase = "Sign up (free)"
	if err := testBrowser.Navigate(context.Background(),
		"data:text/html,<p>Sign%20up%20(free)</p>"); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	if err := testBrowser.WaitForText(phrase, 5*time.Second); err != nil {
		t.Errorf("WaitForText(%q) = %v, want it found", phrase, err)
	}
}

// A wait that times out has to say so. Collapsing every failure into "text not
// found" threw away the only information the caller had.
func TestWaitForTextKeepsTheRealError(t *testing.T) {
	resetFixture(t)

	err := testBrowser.WaitForText("no such text is on this page", 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

// Chrome records microsecond timestamps in Preferences, which are far above
// what a float64 holds exactly. Round-tripping the file through map[string]any
// rewrote them on every start.
func TestMarkProfileCleanExitKeepsLargeNumbersExact(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := filepath.Join(dir, "Default", "Preferences")

	const stamp = "13390000000000123"
	original := `{"profile":{"exit_type":"Crashed","exited_cleanly":false,` +
		`"last_engagement_time":` + stamp + `},"other":{"big":9007199254740993}}`
	if err := os.WriteFile(prefs, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	markProfileCleanExit(dir)

	updated, err := os.ReadFile(prefs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{stamp, "9007199254740993"} {
		if !strings.Contains(string(updated), want) {
			t.Errorf("%s was rewritten; Preferences is now %s", want, updated)
		}
	}
	if !strings.Contains(string(updated), `"exit_type":"Normal"`) {
		t.Errorf("the exit type was not marked clean: %s", updated)
	}
}
