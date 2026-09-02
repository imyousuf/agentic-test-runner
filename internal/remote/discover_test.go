package remote

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizePassesWebSocketURLThrough(t *testing.T) {
	const want = "ws://127.0.0.1:9222/devtools/browser/abc"
	got, err := normalize(want)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

// go-rod dials the endpoint directly as a WebSocket, so an HTTP form has to be
// resolved through /json/version first. The browser UUID also changes on every
// restart, which is why this happens at connect time and is not configured.
func TestNormalizeResolvesHTTPForms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/xyz"}`))
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	for _, endpoint := range []string{server.URL, "cdp://" + host, host} {
		got, err := normalize(endpoint)
		if err != nil {
			t.Fatalf("normalize(%s): %v", endpoint, err)
		}
		if got != "ws://127.0.0.1:9222/devtools/browser/xyz" {
			t.Fatalf("normalize(%s) returned %s", endpoint, got)
		}
	}
}

func TestNormalizeReportsAMissingBrowser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := normalize(server.URL); err == nil {
		t.Fatal("expected an error when webSocketDebuggerUrl is absent")
	}
}
