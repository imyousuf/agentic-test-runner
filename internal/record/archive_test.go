package record

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seed writes a recording with every part a real one has.
func seed(t *testing.T, id string) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), id)
	if err := os.MkdirAll(filepath.Join(dir, framesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	m := Manifest{
		Version: 2, ID: id, Title: "a session", DurationMs: 4200,
		Frames:   []FrameRecord{{Seq: 1, File: "000001.jpg", AtMs: 0}},
		Events:   []Event{{AtMs: 10, T: "error", Reason: "boom", Count: 1}},
		DevTools: &DevTools{Lines: 2, Errors: 1},
	}
	data, _ := json.Marshal(m)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(manifestFile, string(data))
	write(journalFile, "{\"seq\":1}\n")
	write(devtoolsFile, "{\"t\":\"error\",\"text\":\"boom\"}\n{\"t\":\"req\"}\n")
	write(filepath.Join(framesDir, "000001.jpg"), "not really a jpeg")
	return s
}

// The whole point: what comes back has the timeline and the dev log, not just
// the pictures.
func TestExportImportKeepsTheTimelineAndTheLog(t *testing.T) {
	const id = "20260101-120000-a-session"
	src := seed(t, id)

	var buf bytes.Buffer
	if err := Export(src, id, &buf, false); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := Import(dst, bytes.NewReader(buf.Bytes()), int64(buf.Len()), false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ID != id {
		t.Errorf("imported as %q, want %q", res.ID, id)
	}
	if res.Frames != 1 {
		t.Errorf("got %d frames, want 1", res.Frames)
	}

	m, err := dst.Load(id)
	if err != nil {
		t.Fatalf("the imported recording will not load: %v", err)
	}
	if len(m.Events) != 1 || m.Events[0].T != "error" {
		t.Errorf("the timeline did not survive: %+v", m.Events)
	}
	if m.DevTools == nil || m.DevTools.Errors != 1 {
		t.Errorf("the devtools block did not survive: %+v", m.DevTools)
	}
	rows, err := ReadLog(filepath.Join(dst.Root(), id))
	if err != nil || len(rows) != 2 {
		t.Errorf("the dev log did not survive: %d rows, %v", len(rows), err)
	}
	for _, name := range []string{journalFile, filepath.Join(framesDir, "000001.jpg")} {
		if _, err := os.Stat(filepath.Join(dst.Root(), id, name)); err != nil {
			t.Errorf("%s is missing after import", name)
		}
	}
}

// An archive is a file a stranger can send. Every path written has to come
// from the allowlist, never from a name in the archive.
func TestImportRefusesPathsOutsideTheRecording(t *testing.T) {
	nasty := []string{
		"../../../../etc/passwd",
		"../escape.txt",
		"/etc/passwd",
		`..\..\windows\system32\evil.dll`,
		"frames/../../escape.jpg",
		"frames/../manifest.json",
		"subdir/../../out.txt",
	}
	for _, name := range nasty {
		if rel, ok := archiveMember(name); ok {
			t.Errorf("archiveMember(%q) allowed it as %q", name, rel)
		}
	}

	good := map[string]string{
		"manifest.json":                     "manifest.json",
		"frames.jsonl":                      "frames.jsonl",
		"devtools.jsonl":                    "devtools.jsonl",
		"frames/000042.jpg":                 "frames/000042.jpg",
		"20260101-120000/manifest.json":     "manifest.json",
		"20260101-120000/frames/000001.jpg": "frames/000001.jpg",
	}
	for name, want := range good {
		rel, ok := archiveMember(name)
		if !ok || rel != want {
			t.Errorf("archiveMember(%q) = %q,%v; want %q,true", name, rel, ok, want)
		}
	}

	// And a frame name that is not a frame name.
	for _, name := range []string{"frames/evil.sh", "frames/1.jpg", "frames/000001.jpg.sh"} {
		if _, ok := archiveMember(name); ok {
			t.Errorf("archiveMember(%q) allowed a bad frame name", name)
		}
	}
}

// An import that walks off the end must leave nothing behind, or the library
// lists a recording that is not there.
func TestImportLeavesNothingBehindWhenItFails(t *testing.T) {
	dst, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("frames/000001.jpg")
	_, _ = w.Write([]byte("orphan frame, no manifest"))
	_ = zw.Close()

	if _, err := Import(dst, bytes.NewReader(buf.Bytes()), int64(buf.Len()), false); err == nil {
		t.Fatal("an archive with no manifest imported")
	}
	entries, _ := os.ReadDir(dst.Root())
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

// The id is the manifest's, so a manifest claiming a traversal is refused
// before any directory is made.
func TestImportRefusesAnIDThatIsNotOne(t *testing.T) {
	dst, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../../etc", "not-an-id", "", "20260101-120000-../x"} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create(manifestFile)
		_ = json.NewEncoder(w).Encode(Manifest{Version: 2, ID: bad})
		_ = zw.Close()

		_, err := Import(dst, bytes.NewReader(buf.Bytes()), int64(buf.Len()), false)
		if err == nil {
			t.Errorf("a manifest claiming id %q was accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "not one") {
			t.Errorf("id %q refused for the wrong reason: %v", bad, err)
		}
	}
}

func TestImportRefusesToReplaceUnlessForced(t *testing.T) {
	const id = "20260101-120000-a-session"
	src := seed(t, id)
	var buf bytes.Buffer
	if err := Export(src, id, &buf, false); err != nil {
		t.Fatal(err)
	}

	// Importing into the store it came from finds itself already there.
	if _, err := Import(src, bytes.NewReader(buf.Bytes()), int64(buf.Len()), false); err == nil {
		t.Fatal("import overwrote an existing recording without --force")
	}
	if _, err := Import(src, bytes.NewReader(buf.Bytes()), int64(buf.Len()), true); err != nil {
		t.Fatalf("--force did not replace it: %v", err)
	}
	if _, err := src.Load(id); err != nil {
		t.Errorf("the recording is gone after a forced import: %v", err)
	}
}

// The MP4 is derived from the frames and doubles the size, so it goes only
// when it is asked for.
func TestExportLeavesTheMP4OutUnlessAsked(t *testing.T) {
	const id = "20260101-120000-a-session"
	s := seed(t, id)
	if err := os.WriteFile(filepath.Join(s.Root(), id, mp4File), []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}

	has := func(withMP4 bool) bool {
		var buf bytes.Buffer
		if err := Export(s, id, &buf, withMP4); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Name == mp4File {
				return true
			}
		}
		return false
	}
	if has(false) {
		t.Error("the mp4 went in uninvited")
	}
	if !has(true) {
		t.Error("--mp4 did not include it")
	}
}

func TestExportRefusesAnIDThatIsNotOne(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := Export(s, "../../etc/passwd", &bytes.Buffer{}, false); err == nil {
		t.Error("export accepted a traversal as an id")
	}
}
