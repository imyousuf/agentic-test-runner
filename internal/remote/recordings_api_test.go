package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// newTestServer builds a server with a recordings store and no browser. Every
// route that only reads the store works without one.
func newTestServer(t *testing.T, viewOnly bool) (*Server, *record.Store) {
	t.Helper()
	store, err := record.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	streamer := NewStreamer(Options{})
	s := NewServer(NewHub(), streamer, fstest.MapFS{}, viewOnly)
	return s.WithSession(NewSession(store, streamer, record.Limits{}, false)), store
}

func seed(t *testing.T, store *record.Store, id, title string) {
	t.Helper()
	dir, err := store.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frames", "000001.jpg"), []byte("JPEGBYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&record.Manifest{
		Version: 1, ID: id, Title: title, DurationMs: 100,
		Frames: []record.FrameRecord{{Seq: 1, File: "000001.jpg", AtMs: 0, W: 320, H: 240}},
		Events: []record.Event{},
	}); err != nil {
		t.Fatal(err)
	}
}

func do(t *testing.T, s *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestRecordingsAPIRejectsAPathThatTriesToEscape(t *testing.T) {
	s, store := newTestServer(t, false)
	seed(t, store, "20260831-142530", "Demo")

	// A secret next to the recordings root. None of these requests may reach
	// it.
	secret := filepath.Join(filepath.Dir(store.Root()), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve me"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/recordings/..%2f..%2fsecret.txt",
		"/api/recordings/nope/frames/000001.jpg",
		"/api/recordings/20260831-142530/frames/..%2f..%2fsecret.txt",
		"/api/recordings/20260831-142530/frames/manifest.json",
	} {
		w := do(t, s, http.MethodGet, path, "")
		if w.Code == http.StatusOK {
			t.Errorf("GET %s returned 200 and body %q", path, w.Body.String())
		}
	}
}

func TestRecordingsAPIServesAFrameAndTheManifest(t *testing.T) {
	s, store := newTestServer(t, false)
	seed(t, store, "20260831-142530", "Demo")

	w := do(t, s, http.MethodGet, "/api/recordings/20260831-142530/frames/000001.jpg", "")
	if w.Code != http.StatusOK || w.Body.String() != "JPEGBYTES" {
		t.Fatalf("frame: %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Error("a frame never changes, so it should be cacheable")
	}

	w = do(t, s, http.MethodGet, "/api/recordings/20260831-142530/manifest.json", "")
	if w.Code != http.StatusOK {
		t.Fatalf("manifest: %d", w.Code)
	}
	var m record.Manifest
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Title != "Demo" || len(m.Frames) != 1 {
		t.Errorf("manifest = %+v", m)
	}
}

func TestRecordingsAPIListsRenamesAndDeletes(t *testing.T) {
	s, store := newTestServer(t, false)
	seed(t, store, "20260831-142530", "Demo")

	w := do(t, s, http.MethodGet, "/api/recordings", "")
	var list struct {
		Recordings []record.Summary `json:"recordings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Recordings) != 1 || list.Recordings[0].Title != "Demo" {
		t.Fatalf("list = %+v", list.Recordings)
	}

	w = do(t, s, http.MethodPatch, "/api/recordings/20260831-142530", `{"title":"Renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if m, _ := store.Load("20260831-142530"); m.Title != "Renamed" {
		t.Errorf("title = %q", m.Title)
	}

	w = do(t, s, http.MethodDelete, "/api/recordings/20260831-142530", "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := store.Load("20260831-142530"); err == nil {
		t.Error("the recording survived the delete")
	}
}

func TestAViewOnlyServerRefusesToStartARecording(t *testing.T) {
	s, _ := newTestServer(t, true)
	w := do(t, s, http.MethodPost, "/api/record/start", `{"title":"nope"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestRecordStatusIsIdleUntilSomebodyPressesRecord(t *testing.T) {
	s, _ := newTestServer(t, false)
	w := do(t, s, http.MethodGet, "/api/record/status", "")
	var st record.Status
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Recording {
		t.Error("recording is on by default; it must be off")
	}
}

func TestAServerWithNoSessionServesNoRecordingRoutes(t *testing.T) {
	streamer := NewStreamer(Options{})
	s := NewServer(NewHub(), streamer, fstest.MapFS{}, false)
	for _, path := range []string{"/api/recordings", "/api/record/status"} {
		if w := do(t, s, http.MethodGet, path, ""); w.Code == http.StatusOK {
			t.Errorf("GET %s returned 200 without a session", path)
		}
	}
}
