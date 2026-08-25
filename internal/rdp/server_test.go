package rdp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testServer(t *testing.T, token string, viewOnly bool) *Server {
	t.Helper()
	hub := NewHub()
	streamer := NewStreamer(hub, Options{ViewOnly: viewOnly})
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	return NewServer(hub, streamer, assets, token, viewOnly)
}

// The token is the only thing standing between a viewer and full control of the
// browser, so every accepted and rejected form is worth pinning down.
func TestAuthorizedAcceptsEveryTokenForm(t *testing.T) {
	s := testServer(t, "sekret", false)

	cases := []struct {
		name string
		wire func(*http.Request)
		want bool
	}{
		{"no credential", func(*http.Request) {}, false},
		{"query", func(r *http.Request) { r.URL.RawQuery = "t=sekret" }, true},
		{"wrong query", func(r *http.Request) { r.URL.RawQuery = "t=nope" }, false},
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer sekret") }, true},
		{"wrong bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, false},
		{"bare header", func(r *http.Request) { r.Header.Set("Authorization", "sekret") }, false},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: "sekret"}) }, true},
		{"wrong cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: "nope"}) }, false},
		{"prefix of token", func(r *http.Request) { r.URL.RawQuery = "t=sek" }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			tc.wire(r)
			if got := s.authorized(r); got != tc.want {
				t.Fatalf("authorized = %v, want %v", got, tc.want)
			}
		})
	}
}

// An empty token disables auth entirely, which is the documented behaviour for
// a loopback bind with no token configured.
func TestAuthorizedAllowsEverythingWithoutAToken(t *testing.T) {
	s := testServer(t, "", false)
	if !s.authorized(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("an empty token must not gate requests")
	}
}

// checkOrigin is what stops a page on another site from driving the browser
// through the viewer's own credentials.
func TestCheckOriginRejectsForeignPages(t *testing.T) {
	s := testServer(t, "sekret", false)

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
	s := testServer(t, "sekret", true)

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
	s := testServer(t, "sekret", false)

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
	st := NewStreamer(NewHub(), Options{ViewOnly: true})

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
	st := NewStreamer(NewHub(), Options{})

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

// An unauthorised request must never reach a handler.
func TestUnauthorizedRequestsAreRejected(t *testing.T) {
	s := testServer(t, "sekret", false)

	for _, path := range []string{"/", "/api/pages", "/api/navigate?url=http://x/", "/ws"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d, want 401", path, rec.Code)
		}
	}
}

// The cookie is what authorises the stylesheet and script requests that follow
// the first click on the printed URL.
func TestQueryTokenHandsOutACookie(t *testing.T) {
	s := testServer(t, "sekret", false)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?t=sekret", nil))

	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			if c.Value != "sekret" {
				t.Fatalf("cookie value = %q", c.Value)
			}
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie must be HttpOnly and SameSite=Strict: %+v", c)
			}
			return
		}
	}
	t.Fatal("no auth cookie was set")
}
