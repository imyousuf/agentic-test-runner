package api

import (
	"net/http"
	"testing"
)

// Drag failed for every caller, so the handler had only ever been tested for
// the request it refuses. This drives one through to the page.
func TestHandleDrag(t *testing.T) {
	resetPage(t)
	if rr := doPost(testServer, "/navigate", map[string]any{"url": testFixtureURL + "/drag.html"}); rr.Code != http.StatusOK {
		t.Fatalf("navigate: %d %s", rr.Code, rr.Body.String())
	}

	rr := doPost(testServer, "/drag", map[string]any{"from": "#card", "to": `//div[@id="done"]`})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rr.Code, rr.Body.String())
	}
	if resp := parseResponse(t, rr); !resp.Success {
		t.Fatalf("error: %s", resp.Error)
	}

	rr = doPost(testServer, "/eval", map[string]any{"script": `document.getElementById("card").parentElement.id`})
	if rr.Code != http.StatusOK {
		t.Fatalf("eval: %d %s", rr.Code, rr.Body.String())
	}
	data, _ := parseResponse(t, rr).Data.(map[string]any)
	if data["result"] != "done" {
		t.Errorf("the card is in %v, want it moved to the done column", data)
	}
}

func TestHandleDrag_MissingEnd(t *testing.T) {
	rr := doPost(testServer, "/drag", map[string]any{"from": "#card"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
