package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The selector reads are reachable three ways — CLI, REST and MCP — and the
// CLI is a thin client of these handlers. An XPath has to survive the trip
// through a query string or a JSON body and still be read as an XPath at the
// other end, not refused as CSS that does not parse.
func TestSelectorReadsAcceptXPathOverREST(t *testing.T) {
	const heading = `//h1[@id="main-heading"]`
	const modal = `//div[@id="scrollable-modal"]`

	t.Run("text", func(t *testing.T) {
		resetPage(t)
		rr := doGet(testServer, "/text?mode=flat&selector="+url.QueryEscape(heading))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		// The text read back, not merely a 200: the response echoes the
		// selector, so the body alone would mention the heading either way.
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		groups, _ := data["groups"].([]any)
		if len(groups) != 1 {
			t.Fatalf("groups = %v, want the heading's text", data["groups"])
		}
		if group, _ := groups[0].(map[string]any); group["text"] != "Test Page Heading" {
			t.Errorf("read %v, want the heading's text", groups[0])
		}
	})

	t.Run("computed-styles", func(t *testing.T) {
		resetPage(t)
		rr := doGet(testServer, "/computed-styles?properties=fontSize&selector="+url.QueryEscape(heading))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		styles, _ := data["styles"].(map[string]any)
		if styles["fontSize"] != "32px" {
			t.Errorf("fontSize = %v, want the heading's 32px", styles["fontSize"])
		}
	})

	t.Run("computed-styles of every match", func(t *testing.T) {
		resetPage(t)
		rr := doGet(testServer, "/computed-styles?selector_all="+url.QueryEscape(`//*[@class="card"]/h3`))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		if count, _ := data["count"].(float64); count != 3 {
			t.Errorf("count = %v, want the 3 card headings", data["count"])
		}
	})

	t.Run("clean-snapshot", func(t *testing.T) {
		resetPage(t)
		rr := doGet(testServer, "/clean-snapshot?selector="+url.QueryEscape(`//footer`))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		if html, _ := data["html"].(string); !strings.Contains(html, "<footer") || !strings.Contains(html, "Contact") {
			t.Errorf("the snapshot is not of the footer: %v", data["html"])
		}
	})

	t.Run("scroll", func(t *testing.T) {
		resetPage(t)
		rr := doPost(testServer, "/scroll", map[string]any{"selector": modal, "y": 500})
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		if st, _ := data["scrollTop"].(float64); st != 500 {
			t.Errorf("scrollTop = %v, want 500", data["scrollTop"])
		}
	})

	t.Run("screenshot", func(t *testing.T) {
		resetPage(t)
		rr := doGet(testServer, "/screenshot?selector="+url.QueryEscape(heading))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		if image, _ := data["data"].(string); image == "" {
			t.Errorf("no image came back: %v", data)
		}
	})

	t.Run("download-images", func(t *testing.T) {
		resetPage(t)
		rr := doPost(testServer, "/download-images", map[string]any{
			"selector":   `//*[@id="image-section"]`,
			"output_dir": t.TempDir(),
		})
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
		}
		data, _ := parseResponse(t, rr).Data.(map[string]any)
		if captured, _ := data["captured"].(float64); captured != 2 {
			t.Errorf("captured = %v, want both images in the section", data["captured"])
		}
	})
}
