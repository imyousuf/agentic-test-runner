package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestARunningRecordingLeavesALiveMarkerAndClearsItOnStop(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "Live one", Source: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	r.Write(Image{JPEG: testJPEG(t, 64, 64)})

	running, err := s.Running()
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 {
		t.Fatalf("Running() = %d recordings, want 1", len(running))
	}
	if running[0].Source != "cli" || running[0].Title != "Live one" {
		t.Errorf("marker lost its metadata: %+v", running[0])
	}
	if running[0].PID != os.Getpid() {
		t.Errorf("marker pid = %d, want %d", running[0].PID, os.Getpid())
	}

	// A list has to say so too, or the web UI cannot tell a live recording
	// from one that died before it wrote a manifest.
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Live || list[0].Partial {
		t.Errorf("summary while running = %+v, want live and not partial", list[0])
	}
	if list[0].Title != "Live one" {
		t.Errorf("summary title = %q, want the one the marker holds", list[0].Title)
	}

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), m.ID, liveFile)); !os.IsNotExist(err) {
		t.Errorf("the marker outlived the recording: %v", err)
	}
	after, err := s.Running()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("Running() = %+v after a clean stop, want none", after)
	}
}

func TestAStaleMarkerReadsAsNotRunning(t *testing.T) {
	s := newTestStore(t)
	id := NewID(time.Now(), "stale")
	dir := filepath.Join(s.Root(), id)
	if err := os.MkdirAll(filepath.Join(dir, framesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := `{"seq":1,"file":"000001.jpg","atMs":0,"w":64,"h":64}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, journalFile), []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}
	// A recorder that was killed leaves its last beat behind. Anything older
	// than the stale window is a corpse, not a recording.
	data, err := json.Marshal(Live{
		ID:        id,
		Source:    "cli",
		StartedAt: time.Now().Add(-time.Hour),
		SeenAt:    time.Now().Add(-LiveStale - time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, liveFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, fresh := readLive(dir); fresh {
		t.Error("readLive called a stale marker fresh")
	}
	running, err := s.Running()
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 0 {
		t.Errorf("Running() = %+v, want none", running)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Live || !list[0].Partial {
		t.Errorf("summary = %+v, want partial and not live", list)
	}
}

func TestDeleteRefusesARecordingThatIsStillBeingWritten(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Source: "live-view"})
	if err != nil {
		t.Fatal(err)
	}
	r.Write(Image{JPEG: testJPEG(t, 64, 64)})
	id := r.ID()

	if err := s.Delete(id); err == nil {
		t.Fatal("Delete removed a live recording")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), id)); err != nil {
		t.Errorf("the directory went away anyway: %v", err)
	}

	if _, err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(id); err != nil {
		t.Errorf("Delete after the stop: %v", err)
	}
}
