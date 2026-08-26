package ops

// Browser primitive Request/Result types and Execute functions live in this
// file. Each primitive follows the pattern:
//
//   type XRequest struct { ... }
//   type XResult  struct { ... }
//   func X(ctx context.Context, b *browser.Browser, req XRequest) (XResult, error)
//
// REST handlers in internal/api/handlers.go and MCP dispatchers in
// internal/mcp/server.go both call into these functions.

import (
	"context"
	"fmt"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
)

// --- Navigation -------------------------------------------------------------

// NavigateRequest is the canonical request for navigating to a URL.
// If no page exists yet a new one is created; otherwise the current page navigates.
type NavigateRequest struct {
	URL string `json:"url" jsonschema:"required" jsonschema_description:"URL to navigate to"`
}

// NavigateResult reports the resulting URL and page title.
type NavigateResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Navigate navigates the current (or a new) page to the requested URL.
func Navigate(ctx context.Context, b *browser.Browser, req NavigateRequest) (NavigateResult, error) {
	if req.URL == "" {
		return NavigateResult{}, fmt.Errorf("url is required")
	}

	// Create a new page when none exist; otherwise reuse the active one.
	if len(b.ListPages()) == 0 {
		if err := b.NewPage(ctx, req.URL); err != nil {
			return NavigateResult{}, fmt.Errorf("failed to create page: %w", err)
		}
	} else {
		if err := b.Navigate(ctx, req.URL); err != nil {
			return NavigateResult{}, fmt.Errorf("navigation failed: %w", err)
		}
	}
	return NavigateResult{URL: b.CurrentURL(), Title: b.PageTitle()}, nil
}

// NavResult is the standard response for back/forward/reload.
type NavResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Back navigates back in browser history.
func Back(_ context.Context, b *browser.Browser) (NavResult, error) {
	if err := b.GoBack(); err != nil {
		return NavResult{}, fmt.Errorf("failed to go back: %w", err)
	}
	return NavResult{URL: b.CurrentURL(), Title: b.PageTitle()}, nil
}

// Forward navigates forward in browser history.
func Forward(_ context.Context, b *browser.Browser) (NavResult, error) {
	if err := b.GoForward(); err != nil {
		return NavResult{}, fmt.Errorf("failed to go forward: %w", err)
	}
	return NavResult{URL: b.CurrentURL(), Title: b.PageTitle()}, nil
}

// Reload reloads the current page.
func Reload(_ context.Context, b *browser.Browser) (NavResult, error) {
	if err := b.Reload(); err != nil {
		return NavResult{}, fmt.Errorf("failed to reload: %w", err)
	}
	return NavResult{URL: b.CurrentURL(), Title: b.PageTitle()}, nil
}

// --- Interaction ------------------------------------------------------------

// ClickRequest selects an element and (optionally) issues a double-click.
type ClickRequest struct {
	Selector    string `json:"selector"     jsonschema:"required" jsonschema_description:"CSS selector, UID, text, aria-label, or data-testid identifying the element to click"`
	DoubleClick bool   `json:"double_click"                       jsonschema_description:"Issue a double-click instead of a single click"`
}

// ClickResult reports the post-click URL and title.
type ClickResult struct {
	Selector string `json:"selector"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

// Click clicks (or double-clicks) the element matching the request.
func Click(ctx context.Context, b *browser.Browser, req ClickRequest) (ClickResult, error) {
	if req.Selector == "" {
		return ClickResult{}, fmt.Errorf("selector is required")
	}
	if err := b.Click(ctx, req.Selector, req.DoubleClick); err != nil {
		return ClickResult{}, fmt.Errorf("click failed on %q: %w", req.Selector, err)
	}
	return ClickResult{Selector: req.Selector, URL: b.CurrentURL(), Title: b.PageTitle()}, nil
}

// FillRequest types a value into a form field.
type FillRequest struct {
	Selector string `json:"selector" jsonschema:"required" jsonschema_description:"CSS selector, UID, text, aria-label, or data-testid identifying the input"`
	Value    string `json:"value"                          jsonschema_description:"Value to type into the field"`
}

// FillResult reports the filled selector and value.
type FillResult struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
}

// Fill types text into the matched input element.
func Fill(ctx context.Context, b *browser.Browser, req FillRequest) (FillResult, error) {
	if req.Selector == "" {
		return FillResult{}, fmt.Errorf("selector is required")
	}
	if err := b.Fill(ctx, req.Selector, req.Value); err != nil {
		return FillResult{}, fmt.Errorf("fill failed: %w", err)
	}
	return FillResult{Selector: req.Selector, Value: req.Value}, nil
}

// HoverRequest hovers the mouse over an element.
type HoverRequest struct {
	Selector string `json:"selector" jsonschema:"required" jsonschema_description:"CSS selector, UID, text, aria-label, or data-testid identifying the element to hover"`
}

// HoverResult reports the hovered selector.
type HoverResult struct {
	Selector string `json:"selector"`
}

// Hover hovers over the matched element.
func Hover(ctx context.Context, b *browser.Browser, req HoverRequest) (HoverResult, error) {
	if req.Selector == "" {
		return HoverResult{}, fmt.Errorf("selector is required")
	}
	if err := b.Hover(ctx, req.Selector); err != nil {
		return HoverResult{}, fmt.Errorf("hover failed: %w", err)
	}
	return HoverResult{Selector: req.Selector}, nil
}

// PressKeyRequest presses a key or key combination.
type PressKeyRequest struct {
	Key string `json:"key" jsonschema:"required" jsonschema_description:"Key or key combination, e.g. Enter, Tab, Control+A"`
}

// PressKeyResult reports the pressed key.
type PressKeyResult struct {
	Key string `json:"key"`
}

// PressKey simulates pressing a key.
func PressKey(_ context.Context, b *browser.Browser, req PressKeyRequest) (PressKeyResult, error) {
	if req.Key == "" {
		return PressKeyResult{}, fmt.Errorf("key is required")
	}
	if err := b.PressKey(req.Key); err != nil {
		return PressKeyResult{}, fmt.Errorf("press key failed: %w", err)
	}
	return PressKeyResult{Key: req.Key}, nil
}

// DragRequest drags from one element to another.
type DragRequest struct {
	From string `json:"from" jsonschema:"required" jsonschema_description:"Source selector, UID, text, aria-label, or data-testid"`
	To   string `json:"to"   jsonschema:"required" jsonschema_description:"Destination selector, UID, text, aria-label, or data-testid"`
}

// DragResult reports the drag endpoints.
type DragResult struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Drag drags from one element to another.
func Drag(ctx context.Context, b *browser.Browser, req DragRequest) (DragResult, error) {
	if req.From == "" || req.To == "" {
		return DragResult{}, fmt.Errorf("from and to are required")
	}
	if err := b.Drag(ctx, req.From, req.To); err != nil {
		return DragResult{}, fmt.Errorf("drag failed: %w", err)
	}
	return DragResult{From: req.From, To: req.To}, nil
}

// --- Inspection -------------------------------------------------------------

// SnapshotRequest captures the accessibility tree.
type SnapshotRequest struct {
	Verbose bool `json:"verbose" jsonschema_description:"Include detailed attributes for each element"`
}

// SnapshotResult is the snapshot output.
type SnapshotResult struct {
	Elements []browser.ElementInfo `json:"elements"`
	Count    int                   `json:"count"`
	URL      string                `json:"url"`
	Title    string                `json:"title"`
}

// Snapshot returns the accessibility tree of visible page elements.
func Snapshot(_ context.Context, b *browser.Browser, req SnapshotRequest) (SnapshotResult, error) {
	elements, err := b.Snapshot(req.Verbose)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot failed: %w", err)
	}
	return SnapshotResult{
		Elements: elements,
		Count:    len(elements),
		URL:      b.CurrentURL(),
		Title:    b.PageTitle(),
	}, nil
}

// ScreenshotRequest captures a screenshot. Whether the result is returned as
// raw bytes (base64 over the wire) or written to disk depends on the surface;
// the ops layer always carries both possibilities.
type ScreenshotRequest struct {
	Selector    string `json:"selector"     jsonschema_description:"CSS selector to screenshot a single element"`
	SelectorAll string `json:"selector_all" jsonschema_description:"CSS selector matching multiple elements; each captured separately"`
	FullPage    bool   `json:"full_page"    jsonschema_description:"Capture full scrollable page (or full element height when used with selector)"`
	OutputDir   string `json:"output_dir"   jsonschema_description:"Directory for saved files (multi-element mode); defaults to OS temp dir"`
	TimeoutMs   int    `json:"timeout_ms"   jsonschema_description:"Per-element timeout in milliseconds for selector_all mode (default 30000)"`
}

// ScreenshotResult carries either the raw screenshot bytes (single element /
// full page) or the per-element list (selector_all). Surfaces decide how to
// surface this — REST returns base64 or saves to disk; MCP saves to disk.
type ScreenshotResult struct {
	// Single-element / full-page result.
	Data []byte `json:"-"`
	MIME string `json:"mime,omitempty"`

	// Multi-element result (selector_all).
	Multi []browser.ElementScreenshotResult `json:"-"`
}

// IsMulti reports whether the result is from a multi-element capture.
func (r ScreenshotResult) IsMulti() bool { return r.Multi != nil }

// Screenshot captures a screenshot per the request. Caller picks the encoding.
func Screenshot(_ context.Context, b *browser.Browser, req ScreenshotRequest) (ScreenshotResult, error) {
	if req.SelectorAll != "" {
		timeout := 30 * time.Second
		if req.TimeoutMs > 0 {
			timeout = time.Duration(req.TimeoutMs) * time.Millisecond
		}
		results, err := b.GetMultipleElementScreenshots(req.SelectorAll, timeout)
		if err != nil {
			return ScreenshotResult{}, fmt.Errorf("screenshot failed: %w", err)
		}
		return ScreenshotResult{Multi: results, MIME: "image/png"}, nil
	}

	var data []byte
	var err error
	switch {
	case req.Selector != "" && req.FullPage:
		data, err = b.GetElementFullHeightScreenshot(req.Selector)
	case req.Selector != "":
		data, err = b.GetElementScreenshotByCSS(req.Selector)
	default:
		data, err = b.Screenshot(req.FullPage)
	}
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("screenshot failed: %w", err)
	}
	return ScreenshotResult{Data: data, MIME: "image/png"}, nil
}

// HTMLResult carries the page HTML and current URL.
type HTMLResult struct {
	HTML string `json:"html"`
	URL  string `json:"url"`
}

// HTML returns the current page's serialized HTML.
func HTML(_ context.Context, b *browser.Browser) (HTMLResult, error) {
	html, err := b.HTML()
	if err != nil {
		return HTMLResult{}, fmt.Errorf("failed to get HTML: %w", err)
	}
	return HTMLResult{HTML: html, URL: b.CurrentURL()}, nil
}

// URLResult carries the current page URL.
type URLResult struct {
	URL string `json:"url"`
}

// URL returns the current URL.
func URL(_ context.Context, b *browser.Browser) (URLResult, error) {
	return URLResult{URL: b.CurrentURL()}, nil
}

// TitleResult carries the current page title.
type TitleResult struct {
	Title string `json:"title"`
}

// Title returns the current page title.
func Title(_ context.Context, b *browser.Browser) (TitleResult, error) {
	return TitleResult{Title: b.PageTitle()}, nil
}

// EvalRequest runs JavaScript in the page.
type EvalRequest struct {
	Script string `json:"script" jsonschema:"required" jsonschema_description:"JavaScript to execute. Bare expressions like 'document.title' work without explicit return."`
}

// EvalResult carries the script's return value.
type EvalResult struct {
	Result any `json:"result"`
}

// Eval executes JavaScript and returns its result.
func Eval(_ context.Context, b *browser.Browser, req EvalRequest) (EvalResult, error) {
	if req.Script == "" {
		return EvalResult{}, fmt.Errorf("script is required")
	}
	val, err := b.Evaluate(req.Script)
	if err != nil {
		return EvalResult{}, fmt.Errorf("eval failed: %w", err)
	}
	return EvalResult{Result: val}, nil
}

// --- Debugging --------------------------------------------------------------

// ConsoleRequest reads recent console messages.
type ConsoleRequest struct {
	Limit int `json:"limit" jsonschema_description:"Maximum number of messages to return (default 50)"`
}

// ConsoleResult lists captured console messages.
type ConsoleResult struct {
	Messages []browser.ConsoleMessage `json:"messages"`
	Count    int                      `json:"count"`
}

// Console returns recent console messages.
func Console(_ context.Context, b *browser.Browser, req ConsoleRequest) (ConsoleResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	msgs := b.GetConsoleMessages(limit)
	return ConsoleResult{Messages: msgs, Count: len(msgs)}, nil
}

// NetworkRequestArgs reads recent network requests.
type NetworkRequestArgs struct {
	Limit int `json:"limit" jsonschema_description:"Maximum number of requests to return (default 50)"`
}

// NetworkResult lists captured network requests.
type NetworkResult struct {
	Requests []browser.NetworkRequest `json:"requests"`
	Count    int                      `json:"count"`
}

// Network returns recent network requests.
func Network(_ context.Context, b *browser.Browser, req NetworkRequestArgs) (NetworkResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	reqs := b.GetNetworkRequests(limit)
	return NetworkResult{Requests: reqs, Count: len(reqs)}, nil
}

// ErrorsResult lists failed network requests.
type ErrorsResult struct {
	FailedRequests []browser.NetworkRequest `json:"failed_requests"`
	Count          int                      `json:"count"`
}

// Errors returns failed network requests.
func Errors(_ context.Context, b *browser.Browser) (ErrorsResult, error) {
	failed := b.GetFailedRequests()
	return ErrorsResult{FailedRequests: failed, Count: len(failed)}, nil
}

// --- Wait -------------------------------------------------------------------

// WaitRequest waits for an element to appear (and optionally become visible).
type WaitRequest struct {
	Selector  string `json:"selector"   jsonschema:"required" jsonschema_description:"CSS selector to wait for"`
	TimeoutMs int    `json:"timeout"                          jsonschema_description:"Timeout in milliseconds (default 5000)"`
	Visible   bool   `json:"visible"                          jsonschema_description:"Also require the element to be visible"`
}

// WaitResult reports the wait outcome.
type WaitResult struct {
	Found    bool   `json:"found"`
	Selector string `json:"selector"`
	Visible  bool   `json:"visible"`
}

// Wait blocks until the selector matches an element (or visible element).
func Wait(ctx context.Context, b *browser.Browser, req WaitRequest) (WaitResult, error) {
	if req.Selector == "" {
		return WaitResult{}, fmt.Errorf("selector is required")
	}
	timeout := 5 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	var err error
	if req.Visible {
		err = b.WaitForElementVisible(ctx, req.Selector, timeout)
	} else {
		err = b.WaitForElement(ctx, req.Selector, timeout)
	}
	if err != nil {
		return WaitResult{}, fmt.Errorf("wait failed: %w", err)
	}
	return WaitResult{Found: true, Selector: req.Selector, Visible: req.Visible}, nil
}

// --- Computed styles --------------------------------------------------------

// ComputedStylesRequest queries computed CSS styles. Exactly one of Selector,
// SelectorAll, or Selectors is required.
type ComputedStylesRequest struct {
	Selector    string   `json:"selector"     jsonschema_description:"CSS selector for a single element"`
	SelectorAll string   `json:"selector_all" jsonschema_description:"CSS selector matching multiple elements"`
	Selectors   []string `json:"selectors"    jsonschema_description:"Batch list of selectors to query in one call"`
	Properties  []string `json:"properties"   jsonschema_description:"CSS property names to return (default: a built-in layout/typography set)"`
}

// ComputedStylesResult reports the styles for the chosen mode. Exactly one
// of Styles, Elements, or BatchResults is populated based on the request.
type ComputedStylesResult struct {
	Mode         string                        `json:"mode"`
	Selector     string                        `json:"selector,omitempty"`
	Styles       map[string]string             `json:"styles,omitempty"`
	Elements     []browser.ComputedStylesEntry `json:"elements,omitempty"`
	BatchResults []browser.BatchStyleResult    `json:"results,omitempty"`
	Count        int                           `json:"count,omitempty"`
}

// ComputedStyles queries computed styles for one selector, all matches, or a batch.
func ComputedStyles(_ context.Context, b *browser.Browser, req ComputedStylesRequest) (ComputedStylesResult, error) {
	if req.Selector == "" && req.SelectorAll == "" && len(req.Selectors) == 0 {
		return ComputedStylesResult{}, fmt.Errorf("selector, selector_all, or selectors is required")
	}

	if len(req.Selectors) > 0 {
		results, err := b.GetBatchComputedStyles(req.Selectors, req.Properties)
		if err != nil {
			return ComputedStylesResult{}, fmt.Errorf("batch styles failed: %w", err)
		}
		return ComputedStylesResult{Mode: "batch", BatchResults: results}, nil
	}

	if req.SelectorAll != "" {
		entries, err := b.GetMultipleComputedStyles(req.SelectorAll, req.Properties)
		if err != nil {
			return ComputedStylesResult{}, fmt.Errorf("failed to get computed styles: %w", err)
		}
		return ComputedStylesResult{
			Mode:     "all",
			Selector: req.SelectorAll,
			Elements: entries,
			Count:    len(entries),
		}, nil
	}

	styles, err := b.GetComputedStyles(req.Selector, req.Properties)
	if err != nil {
		return ComputedStylesResult{}, fmt.Errorf("failed to get computed styles: %w", err)
	}
	return ComputedStylesResult{
		Mode:     "single",
		Selector: req.Selector,
		Styles:   styles,
		Count:    len(styles),
	}, nil
}

// ComputedStylesDiffRequest compares computed styles between pages. Exactly one
// of Selector or Selectors is required.
type ComputedStylesDiffRequest struct {
	Selector       string   `json:"selector"        jsonschema_description:"CSS selector on current page"`
	Selectors      []string `json:"selectors"       jsonschema_description:"Batch list of selectors"`
	Against        int      `json:"against"         jsonschema:"required" jsonschema_description:"Page index to compare against"`
	Properties     []string `json:"properties"      jsonschema_description:"CSS properties to compare (default: built-in set)"`
	SelectorTarget string   `json:"selector_target" jsonschema_description:"Selector on target page (defaults to source selector)"`
}

// ComputedStylesDiffResult carries either a single-selector diff or a batch diff.
type ComputedStylesDiffResult struct {
	Mode string `json:"mode"`

	// Single mode
	Selector      string                            `json:"selector,omitempty"`
	Matches       map[string]string                 `json:"matches,omitempty"`
	Mismatches    map[string]browser.StyleDiffEntry `json:"mismatches,omitempty"`
	MatchCount    int                               `json:"matchCount,omitempty"`
	MismatchCount int                               `json:"mismatchCount,omitempty"`
	Score         float64                           `json:"score,omitempty"`

	// Batch mode
	Results      []browser.BatchDiffResult `json:"results,omitempty"`
	OverallScore float64                   `json:"overall_score,omitempty"`
}

// ComputedStylesDiff compares computed styles between two pages.
func ComputedStylesDiff(_ context.Context, b *browser.Browser, req ComputedStylesDiffRequest) (ComputedStylesDiffResult, error) {
	if req.Selector == "" && len(req.Selectors) == 0 {
		return ComputedStylesDiffResult{}, fmt.Errorf("selector or selectors is required")
	}

	if len(req.Selectors) > 0 {
		batch, err := b.GetBatchComputedStylesDiff(req.Selectors, req.Against, req.Properties, req.SelectorTarget)
		if err != nil {
			return ComputedStylesDiffResult{}, fmt.Errorf("batch diff failed: %w", err)
		}
		return ComputedStylesDiffResult{
			Mode:         "batch",
			Results:      batch.Results,
			OverallScore: batch.OverallScore,
		}, nil
	}

	res, err := b.GetComputedStylesDiff(req.Selector, req.Against, req.Properties, req.SelectorTarget)
	if err != nil {
		return ComputedStylesDiffResult{}, fmt.Errorf("style diff failed: %w", err)
	}
	return ComputedStylesDiffResult{
		Mode:          "single",
		Selector:      res.Selector,
		Matches:       res.Matches,
		Mismatches:    res.Mismatches,
		MatchCount:    res.MatchCount,
		MismatchCount: res.MismatchCount,
		Score:         res.Score,
	}, nil
}

// --- Scroll -----------------------------------------------------------------

// ScrollRequest scrolls within an element's scroll container.
type ScrollRequest struct {
	Selector string `json:"selector"  jsonschema:"required" jsonschema_description:"CSS selector of scrollable element"`
	X        int    `json:"x"                                jsonschema_description:"Horizontal scroll position in pixels"`
	Y        int    `json:"y"                                jsonschema_description:"Vertical scroll position in pixels"`
	ToBottom bool   `json:"to_bottom"                        jsonschema_description:"Scroll to the bottom of the element"`
	ToTop    bool   `json:"to_top"                           jsonschema_description:"Scroll to the top of the element"`
}

// ScrollResult reports the post-scroll metrics of the element.
type ScrollResult struct {
	Selector     string `json:"selector"`
	ScrollTop    int    `json:"scrollTop"`
	ScrollLeft   int    `json:"scrollLeft"`
	ScrollHeight int    `json:"scrollHeight"`
	ScrollWidth  int    `json:"scrollWidth"`
	ClientHeight int    `json:"clientHeight"`
	ClientWidth  int    `json:"clientWidth"`
}

// Scroll scrolls within the targeted element.
func Scroll(_ context.Context, b *browser.Browser, req ScrollRequest) (ScrollResult, error) {
	if req.Selector == "" {
		return ScrollResult{}, fmt.Errorf("selector is required")
	}
	r, err := b.ScrollElement(req.Selector, req.X, req.Y, req.ToBottom, req.ToTop)
	if err != nil {
		return ScrollResult{}, fmt.Errorf("scroll failed: %w", err)
	}
	return ScrollResult{
		Selector:     req.Selector,
		ScrollTop:    r.ScrollTop,
		ScrollLeft:   r.ScrollLeft,
		ScrollHeight: r.ScrollHeight,
		ScrollWidth:  r.ScrollWidth,
		ClientHeight: r.ClientHeight,
		ClientWidth:  r.ClientWidth,
	}, nil
}

// --- Text -------------------------------------------------------------------

// TextRequest extracts text from an element.
type TextRequest struct {
	Selector string `json:"selector" jsonschema:"required" jsonschema_description:"CSS selector to extract text from"`
	Mode     string `json:"mode"                           jsonschema_description:"Extraction mode: structured (default), flat, links, headings"`
}

// TextResult carries the extracted text groups.
type TextResult struct {
	Selector string              `json:"selector"`
	Mode     string              `json:"mode"`
	Groups   []browser.TextGroup `json:"groups"`
	Count    int                 `json:"count"`
}

// Text extracts text content from the matched element.
func Text(_ context.Context, b *browser.Browser, req TextRequest) (TextResult, error) {
	if req.Selector == "" {
		return TextResult{}, fmt.Errorf("selector is required")
	}
	r, err := b.GetTextContent(req.Selector, req.Mode)
	if err != nil {
		return TextResult{}, fmt.Errorf("text extraction failed: %w", err)
	}
	return TextResult{
		Selector: r.Selector,
		Mode:     r.Mode,
		Groups:   r.Groups,
		Count:    len(r.Groups),
	}, nil
}

// --- Font check -------------------------------------------------------------

// FontCheckRequest checks whether a font family is loaded.
type FontCheckRequest struct {
	Family string `json:"family" jsonschema:"required" jsonschema_description:"Font family name to check"`
}

// FontCheckResult reports whether the font is declared and loaded.
type FontCheckResult struct {
	Family   string `json:"family"`
	Declared bool   `json:"declared"`
	Loaded   bool   `json:"loaded"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Fallback string `json:"fallback,omitempty"`
}

// FontCheck verifies that the named font is loaded and rendering.
func FontCheck(_ context.Context, b *browser.Browser, req FontCheckRequest) (FontCheckResult, error) {
	if req.Family == "" {
		return FontCheckResult{}, fmt.Errorf("family is required")
	}
	r, err := b.CheckFont(req.Family)
	if err != nil {
		return FontCheckResult{}, fmt.Errorf("font check failed: %w", err)
	}
	return FontCheckResult{
		Family:   r.Family,
		Declared: r.Declared,
		Loaded:   r.Loaded,
		Status:   r.Status,
		Reason:   r.Reason,
		Fallback: r.Fallback,
	}, nil
}

// --- Download images --------------------------------------------------------

// DownloadImagesRequest downloads images within an element scope.
type DownloadImagesRequest struct {
	Selector           string `json:"selector"            jsonschema:"required" jsonschema_description:"CSS selector enclosing the images"`
	OutputDir          string `json:"output_dir"                                jsonschema_description:"Directory for saved images (default: OS temp dir)"`
	FallbackScreenshot bool   `json:"fallback_screenshot"                       jsonschema_description:"Screenshot the matching elements when no <img> tags are found"`
}

// DownloadImagesResult carries the raw browser-side download list. Surfaces
// are responsible for persisting the bytes to disk and shaping their own
// response.
type DownloadImagesResult struct {
	Images    []browser.DownloadedImage `json:"-"`
	OutputDir string                    `json:"output_dir,omitempty"`
}

// DownloadImages fetches images within the selector scope.
func DownloadImages(_ context.Context, b *browser.Browser, req DownloadImagesRequest) (DownloadImagesResult, error) {
	if req.Selector == "" {
		return DownloadImagesResult{}, fmt.Errorf("selector is required")
	}
	imgs, err := b.DownloadImages(req.Selector, req.FallbackScreenshot)
	if err != nil {
		return DownloadImagesResult{}, fmt.Errorf("download images failed: %w", err)
	}
	return DownloadImagesResult{Images: imgs, OutputDir: req.OutputDir}, nil
}

// --- Clean snapshot ---------------------------------------------------------

// CleanSnapshotRequest captures a cleaned DOM subtree.
type CleanSnapshotRequest struct {
	Selector  string `json:"selector"   jsonschema:"required" jsonschema_description:"CSS selector identifying the subtree root"`
	Depth     int    `json:"depth"                            jsonschema_description:"Maximum tree depth (0 = unlimited)"`
	MaxLength int    `json:"max_length"                       jsonschema_description:"Maximum output characters (default 5000)"`
	SVGFull   bool   `json:"svg_full"                         jsonschema_description:"Include full SVG path data instead of collapsing to tag-only"`
	JSON      bool   `json:"json"                             jsonschema_description:"Return a structured JSON tree instead of HTML"`
}

// CleanSnapshotResult carries the rendered HTML and (optionally) the JSON tree.
type CleanSnapshotResult struct {
	Selector string                `json:"selector"`
	HTML     string                `json:"html,omitempty"`
	Tree     *browser.CleanDOMNode `json:"tree,omitempty"`
}

// CleanSnapshot returns a sanitized DOM subtree.
func CleanSnapshot(_ context.Context, b *browser.Browser, req CleanSnapshotRequest) (CleanSnapshotResult, error) {
	if req.Selector == "" {
		return CleanSnapshotResult{}, fmt.Errorf("selector is required")
	}
	html, tree, err := b.GetCleanSnapshot(req.Selector, browser.CleanSnapshotOptions{
		Depth:     req.Depth,
		SVGFull:   req.SVGFull,
		MaxLength: req.MaxLength,
	})
	if err != nil {
		return CleanSnapshotResult{}, fmt.Errorf("clean snapshot failed: %w", err)
	}
	if req.JSON {
		return CleanSnapshotResult{Selector: req.Selector, Tree: tree}, nil
	}
	return CleanSnapshotResult{Selector: req.Selector, HTML: html}, nil
}

// --- Viewport ---------------------------------------------------------------

// ViewportRequest gets or sets the viewport.
type ViewportRequest struct {
	Width  int     `json:"width"   jsonschema_description:"Viewport width in pixels (omit to query current viewport)"`
	Height int     `json:"height"  jsonschema_description:"Viewport height in pixels (omit to query current viewport)"`
	DPR    float64 `json:"dpr"     jsonschema_description:"Device pixel ratio (default 1)"`
	Preset string  `json:"preset"  jsonschema_description:"Named preset: mobile (375x812), tablet (768x1024), desktop (1440x900), wide (1920x1080)"`
}

// ViewportResult carries the response. Mode is "get" or "set".
type ViewportResult struct {
	Mode     string                `json:"mode"`
	Width    int                   `json:"width,omitempty"`
	Height   int                   `json:"height,omitempty"`
	DPR      float64               `json:"deviceScaleFactor,omitempty"`
	Previous *browser.ViewportSize `json:"previous,omitempty"`
	Current  *browser.ViewportSize `json:"current,omitempty"`
}

// resolvePreset turns a preset name into width/height; returns (0, 0, error)
// if the preset is unknown.
func resolvePreset(preset string) (int, int, error) {
	switch preset {
	case "mobile":
		return 375, 812, nil
	case "tablet":
		return 768, 1024, nil
	case "desktop":
		return 1440, 900, nil
	case "wide":
		return 1920, 1080, nil
	default:
		return 0, 0, fmt.Errorf("unknown preset: %s (use mobile, tablet, desktop, or wide)", preset)
	}
}

// GetViewport returns the current viewport.
func GetViewport(_ context.Context, b *browser.Browser) (ViewportResult, error) {
	vp, err := b.GetViewport()
	if err != nil {
		return ViewportResult{}, fmt.Errorf("failed to get viewport: %w", err)
	}
	return ViewportResult{
		Mode:   "get",
		Width:  vp.Width,
		Height: vp.Height,
		DPR:    vp.DeviceScaleFactor,
	}, nil
}

// SetViewport applies a new viewport size (or named preset).
func SetViewport(_ context.Context, b *browser.Browser, req ViewportRequest) (ViewportResult, error) {
	w, h := req.Width, req.Height
	if req.Preset != "" {
		pw, ph, err := resolvePreset(req.Preset)
		if err != nil {
			return ViewportResult{}, err
		}
		w, h = pw, ph
	}
	if w == 0 || h == 0 {
		return ViewportResult{}, fmt.Errorf("width and height are required")
	}

	var dprArgs []float64
	if req.DPR > 0 {
		dprArgs = []float64{req.DPR}
	}
	prev, current, err := b.SetViewport(w, h, dprArgs...)
	if err != nil {
		return ViewportResult{}, fmt.Errorf("viewport resize failed: %w", err)
	}
	return ViewportResult{Mode: "set", Previous: prev, Current: current}, nil
}

// --- Page management --------------------------------------------------------

// NewPageRequest opens a new tab.
type NewPageRequest struct {
	URL string `json:"url" jsonschema_description:"URL to open in the new tab (default about:blank)"`
}

// PagesResult lists open tabs.
type PagesResult struct {
	Pages []browser.PageInfo `json:"pages"`
	Count int                `json:"count"`
}

// NewPage opens a new tab.
func NewPage(ctx context.Context, b *browser.Browser, req NewPageRequest) (PagesResult, error) {
	url := req.URL
	if url == "" {
		url = "about:blank"
	}
	if err := b.NewPage(ctx, url); err != nil {
		return PagesResult{}, fmt.Errorf("failed to create page: %w", err)
	}
	pages := b.ListPages()
	return PagesResult{Pages: pages, Count: len(pages)}, nil
}

// ListPages returns the current set of open tabs.
func ListPages(_ context.Context, b *browser.Browser) (PagesResult, error) {
	pages := b.ListPages()
	return PagesResult{Pages: pages, Count: len(pages)}, nil
}

// SelectPageRequest activates an existing tab.
type SelectPageRequest struct {
	Index int `json:"index" jsonschema:"required" jsonschema_description:"Zero-based index of the tab to activate"`
}

// SelectPageResult reports the new tab list and the active index.
type SelectPageResult struct {
	Pages   []browser.PageInfo `json:"pages"`
	Current int                `json:"current"`
}

// SelectPage activates the tab at the given index.
func SelectPage(_ context.Context, b *browser.Browser, req SelectPageRequest) (SelectPageResult, error) {
	if err := b.SelectPage(req.Index); err != nil {
		return SelectPageResult{}, fmt.Errorf("failed to select page: %w", err)
	}
	// Current comes from the listing rather than from the request: listing can
	// discover that a tab has died and renumber the ones after it, and echoing
	// the requested index back would then name a tab nobody selected.
	pages := b.ListPages()
	current := req.Index
	for _, p := range pages {
		if p.Current {
			current = p.Index
			break
		}
	}
	return SelectPageResult{Pages: pages, Current: current}, nil
}

// ClosePageRequest closes a tab by index.
type ClosePageRequest struct {
	Index int `json:"index" jsonschema:"required" jsonschema_description:"Zero-based index of the tab to close"`
}

// ClosePage closes the tab at the given index and returns the remaining tabs.
func ClosePage(_ context.Context, b *browser.Browser, req ClosePageRequest) (PagesResult, error) {
	if err := b.ClosePage(req.Index); err != nil {
		return PagesResult{}, fmt.Errorf("failed to close page: %w", err)
	}
	pages := b.ListPages()
	return PagesResult{Pages: pages, Count: len(pages)}, nil
}

// --- Recording --------------------------------------------------------------

// RecordStartRequest begins a recording session.
type RecordStartRequest struct {
	URL string `json:"url" jsonschema_description:"Initial URL to navigate to before recording (optional)"`
}

// RecordStartResult reports that recording has started.
type RecordStartResult struct {
	Recording bool `json:"recording"`
}

// RecordStart begins a recording session.
func RecordStart(_ context.Context, b *browser.Browser, req RecordStartRequest) (RecordStartResult, error) {
	if err := b.StartRecording(req.URL); err != nil {
		return RecordStartResult{}, err
	}
	return RecordStartResult{Recording: true}, nil
}

// RecordStopResult is returned by RecordStop.
type RecordStopResult struct {
	EventCount  int                     `json:"event_count"`
	TestContent string                  `json:"test_content"`
	Events      []browser.RecordedEvent `json:"events"`
}

// RecordStop stops a recording and returns the captured events plus a
// rendered .test.txt body.
func RecordStop(_ context.Context, b *browser.Browser) (RecordStopResult, error) {
	events, err := b.StopRecording()
	if err != nil {
		return RecordStopResult{}, err
	}
	testContent := browser.FormatTestFile(events, "Recorded Session")
	return RecordStopResult{
		EventCount:  len(events),
		TestContent: testContent,
		Events:      events,
	}, nil
}

// RecordStatusResult reports whether a recording is in progress.
type RecordStatusResult struct {
	Recording  bool `json:"recording"`
	EventCount int  `json:"event_count"`
}

// RecordStatus reports whether a recording is in progress.
func RecordStatus(_ context.Context, b *browser.Browser) (RecordStatusResult, error) {
	return RecordStatusResult{
		Recording:  b.IsRecording(),
		EventCount: b.RecordingEventCount(),
	}, nil
}

// --- Ask --------------------------------------------------------------------

// AskRequest is a natural-language question to be answered by the LLM-backed
// Ask agent. The ops layer accepts the request, but execution requires an
// LLM client; surfaces inject that themselves and call AskWithRunner.
type AskRequest struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Natural-language question about the current page"`
}

// AskResult carries the agent's answer.
type AskResult struct {
	Answer string `json:"answer"`
}

// AskRunner is the function signature for executing a natural-language query
// against the page. Surfaces (REST/MCP) supply this — the ops layer doesn't
// know about the LLM client/agent stack.
type AskRunner func(ctx context.Context, question string) (string, error)

// Ask runs the supplied AskRunner and returns its answer.
func Ask(ctx context.Context, run AskRunner, req AskRequest) (AskResult, error) {
	if req.Question == "" {
		return AskResult{}, fmt.Errorf("question is required")
	}
	if run == nil {
		return AskResult{}, fmt.Errorf("ask runner not configured")
	}
	answer, err := run(ctx, req.Question)
	if err != nil {
		return AskResult{}, fmt.Errorf("ask failed: %w", err)
	}
	return AskResult{Answer: answer}, nil
}

// --- HUD --------------------------------------------------------------------

// HudEnableRequest asks for the in-page agent panel to be installed on every
// page of the browser. As with Ask, execution needs an LLM client, so the
// surface supplies the handler factory.
type HudEnableRequest struct {
	WorkingDir string `json:"working_dir,omitempty" jsonschema_description:"Directory the agent's shell, read and search tools operate in. Defaults to the daemon's working directory."`
}

// HudResult reports the state of the panel.
type HudResult struct {
	Enabled bool `json:"enabled"`
}

// HudHandlerFactory builds the handler that executes HUD turns. Surfaces
// (REST) supply this — the ops layer doesn't know about the LLM client/agent
// stack.
type HudHandlerFactory func(workingDir string) (browser.HudHandler, error)

// HudEnable installs the in-page agent panel.
func HudEnable(_ context.Context, b *browser.Browser, newHandler HudHandlerFactory, req HudEnableRequest) (HudResult, error) {
	if newHandler == nil {
		return HudResult{}, fmt.Errorf("hud handler factory not configured")
	}
	handler, err := newHandler(req.WorkingDir)
	if err != nil {
		return HudResult{}, err
	}
	if err := b.EnableHud(handler); err != nil {
		return HudResult{}, fmt.Errorf("enabling hud: %w", err)
	}
	return HudResult{Enabled: true}, nil
}

// HudDisable removes the panel from every page.
func HudDisable(_ context.Context, b *browser.Browser) (HudResult, error) {
	if err := b.DisableHud(); err != nil {
		return HudResult{}, fmt.Errorf("disabling hud: %w", err)
	}
	return HudResult{Enabled: false}, nil
}

// HudStatus reports whether the panel is installed.
func HudStatus(_ context.Context, b *browser.Browser) (HudResult, error) {
	return HudResult{Enabled: b.HudEnabled()}, nil
}
