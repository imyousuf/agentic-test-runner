package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// liveFile is the marker a running recording keeps in its own directory.
//
// The manifest only exists after a clean stop, so until then a directory looks
// the same whether the recording is running or whether it died. This file is
// the difference. It is refreshed while the recording runs, so a reader can
// tell a live recording from an abandoned one by the age of the marker alone,
// with no shared memory and no process to ask.
const liveFile = "recording.live.json"

// liveBeat is how often a running recording refreshes its marker.
const liveBeat = 2 * time.Second

// LiveStale is the age past which a marker no longer means "running".
//
// It is several beats wide on purpose. A recorder that is briefly starved of
// CPU must not be reported as dead, because the report would offer a repair
// for a recording that is still writing frames.
const LiveStale = 15 * time.Second

// Live describes a recording that is running right now.
type Live struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	PID       int       `json:"pid"`
	Source    string    `json:"source"` // "cli" or "live-view"
	StartedAt time.Time `json:"startedAt"`
	SeenAt    time.Time `json:"seenAt"`
}

// ElapsedMs is how long this recording has been running.
func (l Live) ElapsedMs() int64 { return time.Since(l.StartedAt).Milliseconds() }

// writeLive puts the marker in the recording directory.
func writeLive(dir string, l Live) {
	l.SeenAt = time.Now()
	data, err := json.Marshal(l)
	if err != nil {
		return
	}
	// A failed marker must never stop a recording. The worst it costs is that
	// the live view does not know this recording is running.
	_ = writeAtomic(filepath.Join(dir, liveFile), data)
}

// clearLive removes the marker, which is what a clean stop looks like.
func clearLive(dir string) { _ = os.Remove(filepath.Join(dir, liveFile)) }

// readLive returns the marker of one recording, and whether it is fresh enough
// to mean that the recording is still running.
func readLive(dir string) (Live, bool) {
	data, err := os.ReadFile(filepath.Join(dir, liveFile))
	if err != nil {
		return Live{}, false
	}
	var l Live
	if err := json.Unmarshal(data, &l); err != nil {
		return Live{}, false
	}
	return l, time.Since(l.SeenAt) < LiveStale
}

// Running lists the recordings that are being written right now, newest first.
//
// More than one is possible: "atr record" and the live view both write to the
// same root, and so does a second "atr record" on another browser.
func (s *Store) Running() ([]Live, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return []Live{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Live{}
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		if l, fresh := readLive(filepath.Join(s.root, e.Name())); fresh {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}
