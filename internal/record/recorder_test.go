package record

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testJPEG builds a real JPEG, because the recorder reads the size from the
// header.
func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRecorderWritesFramesAndAManifest(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "Demo", Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}

	img := testJPEG(t, 320, 240)
	for i := 0; i < 5; i++ {
		r.Write(Image{JPEG: img, TargetID: "A1"})
		time.Sleep(2 * time.Millisecond)
	}
	r.Note(Event{T: "tab", TargetID: "B2", URL: "https://example.test/"})

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) != 5 {
		t.Fatalf("wrote %d frames, want 5", len(m.Frames))
	}
	if m.Frames[0].W != 320 || m.Frames[0].H != 240 {
		t.Errorf("frame size = %vx%v, want 320x240", m.Frames[0].W, m.Frames[0].H)
	}
	if m.Title != "Demo" || m.Browser != "Chrome/1" {
		t.Errorf("manifest lost its metadata: %+v", m)
	}
	if len(m.Events) != 1 || m.Events[0].T != "tab" {
		t.Errorf("events = %+v", m.Events)
	}

	dir := filepath.Join(s.Root(), m.ID)
	for _, f := range m.Frames {
		if _, err := os.Stat(filepath.Join(dir, framesDir, f.File)); err != nil {
			t.Errorf("frame %s is not on disk: %v", f.File, err)
		}
	}

	// The frame timestamps have to rise, or the player and ffmpeg both break.
	for i := 1; i < len(m.Frames); i++ {
		if m.Frames[i].AtMs < m.Frames[i-1].AtMs {
			t.Fatalf("atMs went backwards at %d: %+v", i, m.Frames)
		}
	}
}

func TestStopIsSafeToCallTwice(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test"})
	if err != nil {
		t.Fatal(err)
	}
	r.Write(Image{JPEG: testJPEG(t, 32, 32)})

	first, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("the second Stop returned a different manifest")
	}
}

func TestARecordingWithNoFrameIsAnErrorThatSaysWhy(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stop(); err == nil {
		t.Fatal("Stop returned no error for an empty recording")
	}
}

func TestKeepLastDropsTheOldestFramesFromDisk(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test", Limits: Limits{KeepLast: 50 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	img := testJPEG(t, 64, 64)
	for i := 0; i < 12; i++ {
		r.Write(Image{JPEG: img})
		time.Sleep(15 * time.Millisecond)
	}
	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) >= 12 {
		t.Fatalf("kept %d frames; the window should have dropped some", len(m.Frames))
	}

	// Every frame the manifest lists must still be on disk, and nothing else
	// should be left behind. Frames may share a file, so the two counts differ.
	dir := filepath.Join(s.Root(), m.ID, framesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{}
	for _, f := range m.Frames {
		wanted[f.File] = true
		if _, err := os.Stat(filepath.Join(dir, f.File)); err != nil {
			t.Errorf("frame %s is in the manifest but not on disk", f.File)
		}
	}
	for _, e := range entries {
		if !wanted[e.Name()] {
			t.Errorf("%s is on disk but no frame points at it", e.Name())
		}
	}
}

func TestARunOfIdenticalFramesSharesOneFile(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test"})
	if err != nil {
		t.Fatal(err)
	}
	img := testJPEG(t, 320, 240)
	for i := 0; i < 8; i++ {
		r.Write(Image{JPEG: img})
	}
	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) != 8 {
		t.Fatalf("kept %d frames, want all 8; nothing may be dropped", len(m.Frames))
	}
	if m.Shared != 7 {
		t.Errorf("shared %d frames, want 7", m.Shared)
	}
	for _, f := range m.Frames {
		if f.File != m.Frames[0].File {
			t.Fatalf("frame %d wrote its own file %s", f.Seq, f.File)
		}
	}

	entries, err := os.ReadDir(filepath.Join(s.Root(), m.ID, framesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files on disk, want 1", len(entries))
	}
}

func TestTheRingKeepsAFileWhileAnyFramePointsAtIt(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test", Limits: Limits{KeepLast: 60 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	img := testJPEG(t, 64, 64)
	for i := 0; i < 12; i++ {
		r.Write(Image{JPEG: img})
		time.Sleep(15 * time.Millisecond)
	}
	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	// Every kept frame shares one file with frames the window already cut. The
	// ring must not have deleted it on the first cut.
	dir := filepath.Join(s.Root(), m.ID, framesDir)
	for _, f := range m.Frames {
		if _, err := os.Stat(filepath.Join(dir, f.File)); err != nil {
			t.Fatalf("the ring deleted %s while frame %d still points at it", f.File, f.Seq)
		}
	}
}

func TestMaxSizeWithNoWindowStopsTheRecording(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test", Limits: Limits{MaxSize: 1200}})
	if err != nil {
		t.Fatal(err)
	}
	img := testJPEG(t, 200, 200)
	for i := 0; i < 20; i++ {
		r.Write(Image{JPEG: img})
	}
	deadline := time.Now().Add(2 * time.Second)
	for !r.Full() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !r.Full() {
		t.Fatal("the recorder never reported that it was full")
	}
	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) == 0 {
		t.Fatal("hitting the size limit lost every frame")
	}
}

func TestWriteNeverBlocksAndCountsWhatItDrops(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "a test"})
	if err != nil {
		t.Fatal(err)
	}
	img := testJPEG(t, 16, 16)

	// Far more than the queue can hold. Write has to return anyway, because it
	// runs on the goroutine that acknowledges the screencast frame.
	done := make(chan struct{})
	go func() {
		for i := 0; i < queueDepth*3; i++ {
			r.Write(Image{JPEG: img})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked")
	}

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames)+m.Dropped != queueDepth*3 {
		t.Errorf("%d written + %d dropped != %d offered",
			len(m.Frames), m.Dropped, queueDepth*3)
	}
}

/*
A recording has to say what it is for.

The library had entries like "20260901-071213" -- a bare timestamp, no title,
no slug -- and there is no way to tell later which run that was. The rule lives
in Start rather than in each caller so that the CLI, the live view's button and
anything added later all inherit it, and so that a model driving ATR cannot
skip it the way it skips every optional field.
*/
func TestStartNeedsATitle(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, blank := range []string{"", "   ", "\t\n"} {
		if _, err := Start(store, StartOptions{Title: blank}); err == nil {
			t.Errorf("a title of %q was accepted", blank)
		} else if !strings.Contains(err.Error(), "needs a title") {
			t.Errorf("title %q refused for the wrong reason: %v", blank, err)
		}
	}

	// Nothing was created for the ones that were refused.
	entries, _ := os.ReadDir(store.Root())
	for _, e := range entries {
		t.Errorf("a refused recording left %s behind", e.Name())
	}

	// And a real title still works, with the surrounding space taken off.
	rec, err := Start(store, StartOptions{Title: "  Checkout flow  "})
	if err != nil {
		t.Fatalf("a titled recording was refused: %v", err)
	}
	if !strings.HasSuffix(rec.ID(), "-checkout-flow") {
		t.Errorf("id %q does not carry the title", rec.ID())
	}
	_, _ = rec.Stop()
}
