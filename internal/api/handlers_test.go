package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/browser"
	"github.com/imyousuf/agentic-test-runner/internal/config"
)

// testEnv holds the shared test environment for handler tests.
type testEnv struct {
	server     *Server
	fixtureURL string
}

// newTestEnv creates a Server with a real headless browser and fixture server.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// Serve the fixture HTML
	fs := http.FileServer(http.Dir("../browser/testdata"))
	fixture := httptest.NewServer(fs)
	t.Cleanup(fixture.Close)

	// Create browser
	cfg := config.BrowserConfig{
		Headless:  true,
		NoSandbox: true,
	}
	b, err := browser.New(cfg)
	if err != nil {
		t.Fatalf("failed to create browser: %v", err)
	}
	ctx := context.Background()
	if err := b.Launch(ctx); err != nil {
		t.Fatalf("failed to launch browser: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	// Create server with the browser
	s := &Server{
		browser: b,
		mux:     http.NewServeMux(),
	}
	s.registerRoutes()

	return &testEnv{server: s, fixtureURL: fixture.URL}
}

// doGet performs a GET request against the test server.
func doGet(s *Server, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1"+path, nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

// doPost performs a POST request with JSON body against the test server.
func doPost(s *Server, path string, body interface{}) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1"+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

// parseResponse parses the APIResponse from a recorder.
func parseResponse(t *testing.T, rr *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (body: %s)", err, rr.Body.String())
	}
	return resp
}

// navigateFixture navigates the test browser to the fixture page.
func navigateFixture(t *testing.T, env *testEnv) {
	t.Helper()
	rr := doPost(env.server, "/navigate", map[string]interface{}{
		"url": env.fixtureURL + "/test_fixture.html",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("navigate failed: %s", rr.Body.String())
	}
}

func TestHandleHealth(t *testing.T) {
	env := newTestEnv(t)
	// Navigate first so browser has a page
	navigateFixture(t, env)

	rr := doGet(env.server, "/health")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Error("expected success=true")
	}
}

func TestHandleNavigate(t *testing.T) {
	env := newTestEnv(t)

	rr := doPost(env.server, "/navigate", map[string]interface{}{
		"url": env.fixtureURL + "/test_fixture.html",
	})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Error("expected success=true")
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if title, ok := data["title"].(string); !ok || title != "ATR Test Fixture" {
		t.Errorf("title = %q, want 'ATR Test Fixture'", title)
	}
}

func TestHandleNavigate_MissingURL(t *testing.T) {
	env := newTestEnv(t)

	rr := doPost(env.server, "/navigate", map[string]interface{}{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleClick(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/click", map[string]interface{}{
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
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/click", map[string]interface{}{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleScreenshot(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/screenshot")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if _, ok := data["data"].(string); !ok {
		t.Error("expected base64 data in response")
	}
}

func TestHandleScreenshot_Selector(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/screenshot?selector=header")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleScreenshot_File(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/screenshot?format=file")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if _, ok := data["path"].(string); !ok {
		t.Error("expected path in file response")
	}
}

func TestHandleSnapshot(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/snapshot")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleHTML(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/html")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleURL(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/url")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleTitle(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/title")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleEval(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/eval", map[string]interface{}{
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
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/console")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleNetwork(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/network")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleErrors(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/errors")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleScreenshot_SelectorAll(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/screenshot?selector_all=.card")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	captured, ok := data["captured"].(float64)
	if !ok || captured != 3 {
		t.Errorf("captured = %v, want 3", data["captured"])
	}
}

func TestHandleScreenshot_SelectorWithFull(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/screenshot?selector=%23scrollable-modal&full=true")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleText(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/text?selector=footer&mode=headings")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleText_MissingSelector(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/text")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleScroll(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/scroll", map[string]interface{}{
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
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if st, ok := data["scrollTop"].(float64); !ok || st != 500 {
		t.Errorf("scrollTop = %v, want 500", data["scrollTop"])
	}
}

func TestHandleScroll_MissingSelector(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/scroll", map[string]interface{}{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleComputedStyles(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/computed-styles?selector=%23main-heading")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	styles, ok := data["styles"].(map[string]interface{})
	if !ok {
		t.Fatal("expected styles to be a map")
	}
	if fs, ok := styles["fontSize"].(string); !ok || fs != "32px" {
		t.Errorf("fontSize = %q, want '32px'", fs)
	}
}

func TestHandleComputedStyles_WithProperties(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/computed-styles?selector=%23main-heading&properties=fontSize,color")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	resp := parseResponse(t, rr)
	if !resp.Success {
		t.Errorf("expected success=true, error: %s", resp.Error)
	}
}

func TestHandleComputedStyles_MissingSelector(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doGet(env.server, "/computed-styles")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleWait(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/wait", map[string]interface{}{
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
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/wait", map[string]interface{}{})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleWait_Timeout(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	rr := doPost(env.server, "/wait", map[string]interface{}{
		"selector": "#nonexistent",
		"timeout":  500,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestHandleWait_Visible(t *testing.T) {
	env := newTestEnv(t)
	navigateFixture(t, env)

	// #invisible-element is display:none — visible wait should fail
	rr := doPost(env.server, "/wait", map[string]interface{}{
		"selector": "#invisible-element",
		"timeout":  2000,
		"visible":  true,
	})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusInternalServerError, rr.Body.String())
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)

	// Navigate expects POST, not GET
	rr := doGet(env.server, "/navigate")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
