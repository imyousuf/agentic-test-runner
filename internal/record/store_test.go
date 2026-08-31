package record

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidIDRejectsAnythingThatCouldEscapeTheDirectory(t *testing.T) {
	good := []string{
		"20260831-142530",
		"20260831-142530-login-flow",
		"20260831-142530-a",
	}
	for _, id := range good {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}

	bad := []string{
		"", ".", "..", "../etc/passwd", "/etc/passwd",
		"20260831-142530/../..",
		"20260831-142530-../x",
		"20260831-142530-UPPER",
		"20260831-142530-with_underscore",
		"2026831-142530",
		"20260831-1425300",
		"20260831-142530-" + string(make([]byte, 41)),
		"20260831-142530-a\nb",
	}
	for _, id := range bad {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestValidFrameName(t *testing.T) {
	if !ValidFrameName("000001.jpg") {
		t.Error("000001.jpg should be valid")
	}
	for _, name := range []string{"1.jpg", "000001.png", "../000001.jpg", "000001.jpg.exe", ""} {
		if ValidFrameName(name) {
			t.Errorf("ValidFrameName(%q) = true, want false", name)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Login flow":       "login-flow",
		"  Check  out!!  ": "check-out",
		"":                 "",
		"???":              "",
		"Ünïcödé test":     "n-c-d-test",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewIDStaysValid(t *testing.T) {
	now := time.Date(2026, 8, 31, 14, 25, 30, 0, time.UTC)
	if got := NewID(now, ""); got != "20260831-142530" {
		t.Errorf("NewID = %q", got)
	}
	id := NewID(now, "A very long title that goes on and on and on and will be cut short")
	if !ValidID(id) {
		t.Errorf("NewID produced an id that fails ValidID: %q", id)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreSaveLoadRenameDelete(t *testing.T) {
	s := newTestStore(t)
	id := "20260831-142530-demo"
	if _, err := s.Create(id); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{Version: Version, ID: id, Title: "Demo", DurationMs: 1200,
		Frames: []FrameRecord{{Seq: 1, File: "000001.jpg", AtMs: 0}}}
	if err := s.Save(m); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Demo" || len(got.Frames) != 1 {
		t.Fatalf("round trip lost data: %+v", got)
	}

	if err := s.Rename(id, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Load(id); got.Title != "Renamed" {
		t.Errorf("title = %q, want Renamed", got.Title)
	}

	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(id); err == nil {
		t.Error("Load succeeded after Delete")
	}
}

func TestStoreRejectsABadID(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"../escape", "nope"} {
		if _, err := s.Dir(id); err == nil {
			t.Errorf("Dir(%q) returned no error", id)
		}
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q) returned no error", id)
		}
	}
}

func TestListSkipsForeignDirectoriesAndSortsNewestFirst(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"20260101-000000", "20260831-142530-b"} {
		if _, err := s.Create(id); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(&Manifest{Version: Version, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(s.Root(), "not-a-recording"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d recordings, want 2: %+v", len(list), list)
	}
	if list[0].ID != "20260831-142530-b" {
		t.Errorf("newest first failed: %s came first", list[0].ID)
	}
}

func TestListOnAMissingRootIsEmptyNotAnError(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List returned %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d, want 0", len(list))
	}
}

func TestRepairRebuildsTheManifestAndDropsAFrameThatIsNotOnDisk(t *testing.T) {
	s := newTestStore(t)
	id := "20260831-142530"
	dir, err := s.Create(id)
	if err != nil {
		t.Fatal(err)
	}

	w, err := newWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := w.write(i, int64(i-1)*100, []byte("x"), 320, 240, ""); err != nil {
			t.Fatal(err)
		}
	}
	// A fourth line reached the journal, but the process died before the file
	// did. That is the exact case repair has to survive.
	if _, err := w.write(4, 300, []byte("y"), 320, 240, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, framesDir, "000004.jpg")); err != nil {
		t.Fatal(err)
	}

	m, err := s.Repair(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) != 3 {
		t.Fatalf("recovered %d frames, want 3", len(m.Frames))
	}
	if m.DurationMs != 200 {
		t.Errorf("durationMs = %d, want 200", m.DurationMs)
	}
	if _, err := s.Load(id); err != nil {
		t.Errorf("repair did not write a manifest: %v", err)
	}
}

func TestListMarksARecordingWithNoManifestAsPartial(t *testing.T) {
	s := newTestStore(t)
	id := "20260831-142530"
	dir, err := s.Create(id)
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(1, 0, []byte("x"), 10, 10, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Partial {
		t.Fatalf("want one partial recording, got %+v", list)
	}
	if list[0].Frames != 1 {
		t.Errorf("frames = %d, want 1", list[0].Frames)
	}
}
