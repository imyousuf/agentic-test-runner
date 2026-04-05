package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

var (
	testServer     *Server
	testFixtureURL string
)

func TestMain(m *testing.M) {
	// Serve fixture HTML
	fs := http.FileServer(http.Dir("../browser/testdata"))
	fixture := httptest.NewServer(fs)
	testFixtureURL = fixture.URL

	// Launch shared browser
	cfg := config.BrowserConfig{
		Headless:  true,
		NoSandbox: true,
	}
	b, err := browser.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create browser: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := b.Launch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to launch browser: %v\n", err)
		os.Exit(1)
	}

	testServer = &Server{
		browser: b,
		mux:     http.NewServeMux(),
	}
	testServer.registerRoutes()

	// Navigate to fixture to create initial page
	data, _ := json.Marshal(map[string]string{"url": testFixtureURL + "/test_fixture.html"})
	req := httptest.NewRequest("POST", "/api/v1/navigate", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	testServer.mux.ServeHTTP(rr, req)

	code := m.Run()

	b.Close()
	fixture.Close()
	os.Exit(code)
}

// resetPage navigates back to the fixture, closing extra tabs.
func resetPage(t *testing.T) {
	t.Helper()
	// Close extra pages
	pages := testServer.browser.ListPages()
	for len(pages) > 1 {
		testServer.browser.ClosePage(len(pages) - 1)
		pages = testServer.browser.ListPages()
	}
	if len(pages) > 0 {
		testServer.browser.SelectPage(0)
	}
	doPost(testServer, "/navigate", map[string]any{
		"url": testFixtureURL + "/test_fixture.html",
	})
}

func doGet(s *Server, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1"+path, nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

func doPost(s *Server, path string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1"+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

func parseResponse(t *testing.T, rr *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (body: %s)", err, rr.Body.String())
	}
	return resp
}

func TestHandleHealth(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/health")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Error("expected success=true")
	}
}

func TestHandleNavigate(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/navigate", map[string]any{
		"url": testFixtureURL + "/test_fixture.html",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Error("expected success=true")
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if title, ok := data["title"].(string); !ok || title != "ATR Test Fixture" {
		t.Errorf("title = %q, want 'ATR Test Fixture'", title)
	}
}

func TestHandleNavigate_MissingURL(t *testing.T) {
	rr := doPost(testServer, "/navigate", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleClick(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/click", map[string]any{
		"target": "#test-button",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleClick_MissingTarget(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/click", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleScreenshot(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if _, ok := data["data"].(string); !ok {
		t.Error("expected base64 data in response")
	}
}

func TestHandleScreenshot_Selector(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot?selector=header")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleScreenshot_File(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot?format=file")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if _, ok := data["path"].(string); !ok {
		t.Error("expected path in file response")
	}
}

func TestHandleScreenshot_SelectorAll(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot?selector_all=.card")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	captured, ok := data["captured"].(float64)
	if !ok || captured != 3 {
		t.Errorf("captured = %v, want 3", data["captured"])
	}
}

func TestHandleScreenshot_SelectorAll_WithTimeout(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot?selector_all=.card&timeout=30000")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	total, _ := data["total"].(float64)
	captured, _ := data["captured"].(float64)
	if total != 3 {
		t.Errorf("total = %v, want 3", total)
	}
	if captured != 3 {
		t.Errorf("captured = %v, want 3", captured)
	}
}

func TestHandleScreenshot_SelectorAll_WithOutputDir(t *testing.T) {
	resetPage(t)
	dir := t.TempDir()
	rr := doGet(testServer, "/screenshot?selector_all=.card&output_dir="+dir)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleScreenshot_SelectorWithFull(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/screenshot?selector=%23scrollable-modal&full=true")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleSnapshot(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/snapshot")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleHTML(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/html")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleURL(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/url")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleTitle(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/title")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleEval(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/eval", map[string]any{
		"script": "document.title",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleConsole(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/console")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleNetwork(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/network")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleErrors(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/errors")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleComputedStyles(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/computed-styles?selector=%23main-heading")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	styles, ok := data["styles"].(map[string]any)
	if !ok {
		t.Fatal("expected styles to be a map")
	}
	if fs, ok := styles["fontSize"].(string); !ok || fs != "32px" {
		t.Errorf("fontSize = %q, want '32px'", fs)
	}
}

func TestHandleComputedStyles_WithProperties(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/computed-styles?selector=%23main-heading&properties=fontSize,color")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleComputedStyles_BatchSelectors(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/computed-styles?selectors=h1,.card+h3,%23nonexistent")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	results, ok := data["results"].([]any)
	if !ok {
		t.Fatal("expected results to be an array")
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	// First result (h1) should be matched
	if r, ok := results[0].(map[string]any); ok {
		if r["matched"] != true {
			t.Error("h1 should be matched")
		}
	}
	// Third result (#nonexistent) should not be matched
	if r, ok := results[2].(map[string]any); ok {
		if r["matched"] != false {
			t.Error("#nonexistent should not be matched")
		}
	}
}

func TestHandleComputedStylesDiff_BatchSelectors(t *testing.T) {
	resetPage(t)
	// Open second page
	doPost(testServer, "/navigate", map[string]any{
		"url": testFixtureURL + "/test_fixture.html",
	})
	rr := doGet(testServer, "/computed-styles-diff?selectors=h1,footer&against=0&properties=fontSize,fontWeight")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	overallScore, _ := data["overall_score"].(float64)
	if overallScore != 100 {
		t.Errorf("overall_score = %f, want 100", overallScore)
	}
}

func TestHandleComputedStyles_SelectorAll(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/computed-styles?selector_all=.card+h3")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	count, _ := data["count"].(float64)
	if count != 3 {
		t.Errorf("count = %v, want 3", count)
	}
	elements, ok := data["elements"].([]any)
	if !ok || len(elements) != 3 {
		t.Errorf("expected 3 elements, got %v", data["elements"])
	}
}

func TestHandleComputedStyles_MissingSelector(t *testing.T) {
	rr := doGet(testServer, "/computed-styles")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleCleanSnapshot(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/clean-snapshot?selector=footer")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	html, _ := data["html"].(string)
	if html == "" {
		t.Error("expected non-empty html")
	}
}

func TestHandleCleanSnapshot_JSON(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/clean-snapshot?selector=header&format=json")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["tree"] == nil {
		t.Error("expected tree in JSON response")
	}
}

func TestHandleCleanSnapshot_MissingSelector(t *testing.T) {
	rr := doGet(testServer, "/clean-snapshot")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleViewport_Get(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/viewport")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["width"] == nil || data["height"] == nil {
		t.Error("expected width and height in viewport response")
	}
}

func TestHandleViewport_Set(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/viewport", map[string]any{
		"width":  800,
		"height": 600,
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleViewport_Preset(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/viewport", map[string]any{
		"preset": "mobile",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleViewport_InvalidPreset(t *testing.T) {
	rr := doPost(testServer, "/viewport", map[string]any{
		"preset": "invalid",
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleDownloadImages(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/download-images", map[string]any{
		"selector": "#image-section",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	captured, _ := data["captured"].(float64)
	if captured != 2 {
		t.Errorf("captured = %v, want 2", captured)
	}
}

func TestHandleDownloadImages_FallbackScreenshot(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/download-images", map[string]any{
		"selector":            "#no-images-section .visual-card",
		"fallback_screenshot": true,
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleDownloadImages_MissingSelector(t *testing.T) {
	rr := doPost(testServer, "/download-images", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleFontCheck(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/font-check?family=sans-serif")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["family"] != "sans-serif" {
		t.Errorf("family = %v, want 'sans-serif'", data["family"])
	}
}

func TestHandleFontCheck_MissingFamily(t *testing.T) {
	rr := doGet(testServer, "/font-check")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleFontCheck_NotFound(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/font-check?family=NonExistentFontXYZ")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["loaded"] != false {
		t.Error("expected loaded=false for nonexistent font")
	}
}

func TestHandleComputedStylesDiff(t *testing.T) {
	resetPage(t)
	// Open second page
	doPost(testServer, "/navigate", map[string]any{
		"url": testFixtureURL + "/test_fixture.html",
	})
	rr := doGet(testServer, "/computed-styles-diff?selector=%23main-heading&against=0&properties=fontSize,fontWeight")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleComputedStylesDiff_MissingSelector(t *testing.T) {
	rr := doGet(testServer, "/computed-styles-diff?against=0")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleComputedStylesDiff_MissingAgainst(t *testing.T) {
	rr := doGet(testServer, "/computed-styles-diff?selector=h1")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleText(t *testing.T) {
	resetPage(t)
	rr := doGet(testServer, "/text?selector=footer&mode=headings")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleText_MissingSelector(t *testing.T) {
	rr := doGet(testServer, "/text")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleScroll(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/scroll", map[string]any{
		"selector": "#scrollable-modal",
		"y":        500,
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if st, ok := data["scrollTop"].(float64); !ok || st != 500 {
		t.Errorf("scrollTop = %v, want 500", data["scrollTop"])
	}
}

func TestHandleScroll_MissingSelector(t *testing.T) {
	rr := doPost(testServer, "/scroll", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleWait(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/wait", map[string]any{
		"selector": "#test-button",
		"timeout":  3000,
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleWait_MissingSelector(t *testing.T) {
	rr := doPost(testServer, "/wait", map[string]any{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleWait_Timeout(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/wait", map[string]any{
		"selector": "#nonexistent",
		"timeout":  500,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleWait_Visible(t *testing.T) {
	resetPage(t)
	rr := doPost(testServer, "/wait", map[string]any{
		"selector": "#invisible-element",
		"timeout":  2000,
		"visible":  true,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusInternalServerError, rr.Body.String())
	}
}

func TestHandleScreenshot_SelectorFullNonScrollable(t *testing.T) {
	resetPage(t)
	// header is not scrollable — --selector + --full should not timeout
	rr := doGet(testServer, "/screenshot?selector=header&full=true")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestNewRoutesAccessible(t *testing.T) {
	resetPage(t)
	// Verify all new endpoints return valid JSON (not 404)
	routes := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{"GET", "/computed-styles?selector=h1", nil},
		{"GET", "/text?selector=footer", nil},
		{"POST", "/wait", map[string]any{"selector": "h1", "timeout": 1000}},
		{"POST", "/scroll", map[string]any{"selector": "body", "y": 0}},
	}
	for _, r := range routes {
		var rr *httptest.ResponseRecorder
		if r.method == "GET" {
			rr = doGet(testServer, r.path)
		} else {
			rr = doPost(testServer, r.path, r.body)
		}
		if rr.Code == http.StatusNotFound {
			t.Errorf("route %s %s returned 404 — not registered", r.method, r.path)
		}
		resp := parseResponse(t, rr)
		if !resp.Success {
			t.Errorf("route %s %s failed: %s", r.method, r.path, resp.Error)
		}
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	rr := doGet(testServer, "/navigate")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
