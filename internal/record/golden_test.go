package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenRecordingSplitsIntoActiveAndIdle replays a real recording through
// the detector.
//
// A synthetic frame cannot answer the question this detector exists for: does
// it tell typing on a real page apart from a caret blinking on the same page?
// A real recording is far too large to commit, so this test is opt in. Point
// ATR_GOLDEN_RECORDING at a recording directory to run it.
//
//	ATR_GOLDEN_RECORDING=~/.atr/recordings/20260831-080536-opal-localdev-chat-prompt \
//	  go test ./internal/record/ -run Golden -v
func TestGoldenRecordingSplitsIntoActiveAndIdle(t *testing.T) {
	dir := os.Getenv("ATR_GOLDEN_RECORDING")
	if dir == "" {
		t.Skip("set ATR_GOLDEN_RECORDING to a recording directory")
	}

	raw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Frames) == 0 {
		t.Fatal("the golden recording has no frame")
	}

	tk := newTracker(ChangeOptions{})
	active, files := 0, 0
	seconds := map[int64]bool{}
	for _, f := range m.Frames {
		data, err := os.ReadFile(filepath.Join(dir, framesDir, f.File))
		if err != nil {
			t.Fatal(err)
		}
		sig, err := signature(data)
		if err != nil {
			t.Fatal(err)
		}
		score, fresh := tk.observe(f.AtMs, sig)
		if score >= DefaultThreshold {
			active++
			seconds[f.AtMs/1000] = true
		}
		if fresh {
			files++
		}
	}

	span := m.Frames[len(m.Frames)-1].AtMs/1000 + 1
	hot := float64(len(seconds)) / float64(span)
	t.Logf("frames %d, files %d (%.0f%% of the frames), active seconds %d of %d (%.0f%%)",
		len(m.Frames), files, 100*float64(files)/float64(len(m.Frames)),
		len(seconds), span, 100*hot)

	// The session was a person typing a prompt and waiting for an answer. Most
	// of it is a still page. A detector that calls almost everything active, or
	// almost nothing, is the failure this work set out to fix.
	if hot < 0.05 || hot > 0.60 {
		t.Errorf("%.0f%% of the seconds are active; that is not a session with a person in it", 100*hot)
	}
	if files >= len(m.Frames) {
		t.Errorf("no frame shared a file; sharing saved nothing")
	}
}
