package remote

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pagesMsg(id, url string) []byte {
	return []byte(`{"t":"pages","pages":[{"id":"` + id + `","url":"` + url + `","active":true}]}`)
}

// A single page application changes its URL without ever changing its tab. The
// timeline has to mark that, or a whole session of a React app looks like one
// long page with nothing happening on it.
func TestTheSinkMarksATabChangeAndANavigation(t *testing.T) {
	store, err := record.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Start(store, record.StartOptions{Title: "a test"})
	if err != nil {
		t.Fatal(err)
	}
	sink := NewRecorderSink(rec)
	sink.Frame(&Frame{JPEG: testJPEG(t)})

	sink.Text(pagesMsg("A1", "https://example.test/one"))
	sink.Text(pagesMsg("A1", "https://example.test/one")) // no change, no mark
	sink.Text(pagesMsg("A1", ""))                         // part way through, no mark
	sink.Text(pagesMsg("A1", "https://example.test/two"))
	sink.Text(pagesMsg("B2", "https://other.test/"))
	sink.Action(Action{Kind: "click", Detail: "left"})

	m, err := rec.Stop()
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range m.Events {
		kinds = append(kinds, ev.T)
	}
	want := []string{"tab", "nav", "tab", "click"}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("events = %v, want %v", kinds, want)
		}
	}
	if m.Events[3].TargetID != "B2" {
		t.Errorf("the click was filed under %q, want the tab it happened in", m.Events[3].TargetID)
	}
}

// actorSink collects the actions a Streamer reports.
type actorSink struct{ acts []Action }

func (a *actorSink) Frame(*Frame)      {}
func (a *actorSink) Text([]byte)       {}
func (a *actorSink) Action(act Action) { a.acts = append(a.acts, act) }
