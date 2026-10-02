package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A batch of selectors used to travel as one query parameter, joined with
// commas, and the handler split it back on commas. A selector that contains
// one — contains(., "x") in an XPath, :has-text("a, b"), or a plain CSS
// selector list — came out the other side in pieces, each of which matched
// nothing, and the response reported them unmatched without a word. A batch
// goes in a JSON body now, one selector per element, and is not parsed.

func doRaw(s *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	return rr
}

// batchResults pulls the per-selector rows out of a batch response.
func batchResults(t *testing.T, rr *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	data, _ := parseResponse(t, rr).Data.(map[string]any)
	raw, _ := data["results"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		row, _ := r.(map[string]any)
		out = append(out, row)
	}
	return out
}

func TestBatchComputedStylesTakesSelectorsContainingCommas(t *testing.T) {
	resetPage(t)

	selectors := []string{
		`//h1[contains(., "Test Page")]`,   // an XPath with a comma in it
		`h1:has-text("Test Page, or not")`, // a :has-text() with one, matching nothing
		`#main-heading, #no-such-thing`,    // a CSS selector list
		`//div[@class="card"][contains(., "Card Two")]/h3`,
		`#nonexistent`,
	}
	rr := doPost(testServer, "/computed-styles", map[string]any{
		"selectors":  selectors,
		"properties": []string{"fontSize"},
	})

	rows := batchResults(t, rr)
	if len(rows) != len(selectors) {
		t.Fatalf("got %d rows for %d selectors — a selector was split: %s", len(rows), len(selectors), rr.Body.String())
	}
	wantMatched := []bool{true, false, true, true, false}
	for i, row := range rows {
		if row["selector"] != selectors[i] {
			t.Errorf("row %d is for %q, want %q", i, row["selector"], selectors[i])
		}
		if row["matched"] != wantMatched[i] {
			t.Errorf("%q: matched = %v, want %v", selectors[i], row["matched"], wantMatched[i])
		}
	}
	if styles, _ := rows[0]["styles"].(map[string]any); styles["fontSize"] != "32px" {
		t.Errorf("the heading's fontSize = %v, want 32px", styles["fontSize"])
	}
	if styles, _ := rows[0]["styles"].(map[string]any); len(styles) != 1 {
		t.Errorf("styles = %v, want only the property asked for", styles)
	}
}

func TestBatchComputedStylesDiffTakesSelectorsContainingCommas(t *testing.T) {
	resetPage(t)

	selectors := []string{`//h1[contains(., "Test Page")]`, `footer`, `#nonexistent`}
	rr := doPost(testServer, "/computed-styles-diff", map[string]any{
		"selectors":  selectors,
		"against":    0,
		"properties": []string{"fontSize", "fontWeight"},
	})

	rows := batchResults(t, rr)
	if len(rows) != len(selectors) {
		t.Fatalf("got %d rows for %d selectors: %s", len(rows), len(selectors), rr.Body.String())
	}
	for i, want := range []bool{true, true, false} {
		if rows[i]["matched"] != want {
			t.Errorf("%q: matched = %v, want %v", selectors[i], rows[i]["matched"], want)
		}
	}
	// Compared against the same page, what matched is identical to itself.
	if score, _ := rows[0]["score"].(float64); score != 100 {
		t.Errorf("score = %v, want 100 for the heading against itself", rows[0]["score"])
	}
}

// The body is the whole request, so the single and all-matches forms work
// through it too.
func TestComputedStylesBodyTakesTheOtherModes(t *testing.T) {
	resetPage(t)

	rr := doPost(testServer, "/computed-styles", map[string]any{
		"selector":   `//h1[contains(., "Test Page")]`,
		"properties": []string{"fontSize"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("single: status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	data, _ := parseResponse(t, rr).Data.(map[string]any)
	if styles, _ := data["styles"].(map[string]any); styles["fontSize"] != "32px" {
		t.Errorf("single: fontSize = %v, want 32px", styles["fontSize"])
	}

	rr = doPost(testServer, "/computed-styles", map[string]any{"selector_all": `.card h3`})
	if rr.Code != http.StatusOK {
		t.Fatalf("all: status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	data, _ = parseResponse(t, rr).Data.(map[string]any)
	if count, _ := data["count"].(float64); count != 3 {
		t.Errorf("all: count = %v, want 3", data["count"])
	}

	rr = doPost(testServer, "/computed-styles-diff", map[string]any{
		"selector": `//h1[contains(., "Test Page")]`, "against": 0, "properties": []string{"fontSize"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("diff: status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	data, _ = parseResponse(t, rr).Data.(map[string]any)
	if score, _ := data["score"].(float64); score != 100 {
		t.Errorf("diff: score = %v, want 100", data["score"])
	}
}

func TestComputedStylesBodyIsValidated(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{"not JSON", "/computed-styles", `selectors=h1`},
		{"no selector at all", "/computed-styles", `{"properties": ["fontSize"]}`},
		{"an empty batch", "/computed-styles", `{"selectors": []}`},
		{"diff, not JSON", "/computed-styles-diff", `{`},
		{"diff, no selector", "/computed-styles-diff", `{"against": 0}`},
		// Page 0 is a real page, so a missing index must not quietly mean it.
		{"diff, no page to compare against", "/computed-styles-diff", `{"selectors": ["h1"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := doRaw(testServer, http.MethodPost, tt.path, tt.body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusBadRequest, rr.Body.String())
			}
		})
	}

	// The query form takes its lists comma-joined in a string. Carried over
	// into a body that is the wrong type, and the refusal says which.
	rr := doRaw(testServer, http.MethodPost, "/computed-styles", `{"selector": "h1", "properties": "fontSize,color"}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("properties as a string: status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if body := rr.Body.String(); !strings.Contains(body, "properties") || !strings.Contains(body, "array") {
		t.Errorf("the refusal does not say what was wrong: %s", body)
	}

	for _, path := range []string{"/computed-styles", "/computed-styles-diff"} {
		if rr := doRaw(testServer, http.MethodDelete, path, ""); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("DELETE %s: status = %d, want %d", path, rr.Code, http.StatusMethodNotAllowed)
		}
	}
}

// The query form is unchanged, commas and all, for whatever still sends it.
func TestBatchSelectorsInAQueryStillSplitOnCommas(t *testing.T) {
	resetPage(t)

	rows := batchResults(t, doGet(testServer, "/computed-styles?selectors=h1,.card+h3,%23nonexistent"))
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, want := range []bool{true, true, false} {
		if rows[i]["matched"] != want {
			t.Errorf("row %d: matched = %v, want %v", i, rows[i]["matched"], want)
		}
	}
}
