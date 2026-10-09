package remote

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testServer(t *testing.T, viewOnly bool) *Server {
	t.Helper()
	hub := NewHub()
	streamer := NewStreamer(Options{ViewOnly: viewOnly})
	streamer.AddSink(hub)
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	return NewServer(hub, streamer, assets, viewOnly)
}

// checkOrigin is what stops a page on another site from driving the browser
// through the viewer's own credentials.
func TestCheckOriginRejectsForeignPages(t *testing.T) {
	s := testServer(t, false)

	cases := map[string]bool{
		"":                              true, // a non-browser client sends no Origin
		"http://127.0.0.1:7788":         true,
		"http://localhost:7788":         true,
		"http://[::1]:7788":             true,
		"https://localhost":             true,
		"http://evil.example":           false,
		"http://127.0.0.1.evil.example": false,
		"http://notlocalhost":           false,
		"://":                           false,
	}

	for origin, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if got := s.checkOrigin(r); got != want {
			t.Fatalf("checkOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}

// --view-only has to be enforced on the server. Enforcing it only in the
// WebSocket dispatch left /api/navigate able to drive the browser.
func TestViewOnlyRefusesNavigateOverREST(t *testing.T) {
	s := testServer(t, true)

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/navigate?t=sekret&url=http://evil.example/", nil)
	s.Handler().ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("view-only /api/navigate = %d, want %d. Body: %s",
			rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// The same request on a normal server must reach the streamer, so the test
// above is proving the view-only guard rather than an unrelated rejection.
func TestNavigateReachesTheStreamerWhenNotViewOnly(t *testing.T) {
	s := testServer(t, false)

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/navigate?t=sekret&url=http://example.com/", nil)
	s.Handler().ServeHTTP(rec, r)

	// No browser is attached, so the streamer rejects it -- but with its own
	// error, not a 403.
	if rec.Code == http.StatusForbidden {
		t.Fatal("a non view-only server must not refuse navigate outright")
	}
}

// Input must be refused by the streamer itself, so every surface inherits it.
//
// The assertion is errors.Is(ErrViewOnly) rather than err != nil on purpose: an
// unattached streamer returns "no page is selected" for all of these, so a bare
// nil check passes even with the guards removed.
func TestStreamerRefusesInputWhenViewOnly(t *testing.T) {
	st := NewStreamer(Options{ViewOnly: true})

	for name, err := range map[string]error{
		"mouse":    st.Mouse(MouseMsg{Kind: "pressed"}),
		"wheel":    st.Wheel(WheelMsg{}),
		"key":      st.Key(KeyMsg{Kind: "down", Key: "a"}),
		"text":     st.Text("hunter2"),
		"navigate": st.Navigate("http://example.com"),
	} {
		if !errors.Is(err, ErrViewOnly) {
			t.Fatalf("%s: got %v, want ErrViewOnly", name, err)
		}
	}
}

// The same calls on a normal streamer must fail for a different reason, which
// is what proves the test above is measuring the guard and not the missing page.
func TestStreamerInputIsNotRefusedWhenWritable(t *testing.T) {
	st := NewStreamer(Options{})

	for name, err := range map[string]error{
		"mouse":    st.Mouse(MouseMsg{Kind: "pressed"}),
		"text":     st.Text("hunter2"),
		"navigate": st.Navigate("http://example.com"),
	} {
		if errors.Is(err, ErrViewOnly) {
			t.Fatalf("%s: a writable streamer must not return ErrViewOnly", name)
		}
	}
}

/*
The live view authenticates nobody, on purpose: a token that changed on every
restart broke the URL people had bookmarked and the cookie their browser held,
both at once.

What that leaves has to be true, so it is pinned here. Everything is served,
and --view-only is still enforced -- it is the only guard left, so it cannot
quietly depend on a credential that no longer exists.
*/
func TestEverythingIsServedWithoutACredential(t *testing.T) {
	s := testServer(t, false)
	for _, path := range []string{"/", "/api/pages"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s answered 401; nothing authenticates any more", path)
		}
	}
}

func TestViewOnlyStillHoldsWithoutACredential(t *testing.T) {
	s := testServer(t, true)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/api/navigate?url=https://example.com", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("navigate on a view-only server = %d, want 403", rec.Code)
	}
}
