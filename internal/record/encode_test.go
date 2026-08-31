package record

import (
	"strings"
	"testing"
)

func TestConcatListRepeatsTheLastFile(t *testing.T) {
	m := &Manifest{Frames: []FrameRecord{
		{File: "000001.jpg", AtMs: 0},
		{File: "000002.jpg", AtMs: 500},
		{File: "000003.jpg", AtMs: 2000},
	}}
	got := string(ConcatList(m))
	lines := strings.Split(strings.TrimSpace(got), "\n")

	if lines[0] != "ffconcat version 1.0" {
		t.Errorf("first line = %q", lines[0])
	}
	// The concat demuxer ends the stream at the start of the final entry, so
	// the last file must appear twice or its frame is lost.
	if strings.Count(got, "000003.jpg") != 2 {
		t.Errorf("the last file appears %d times, want 2:\n%s",
			strings.Count(got, "000003.jpg"), got)
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "file ") {
		t.Errorf("the list must end with a file line and no duration, got %q", last)
	}
	if !strings.Contains(got, "duration 0.500") {
		t.Errorf("the first gap should be 0.500 s:\n%s", got)
	}
	if !strings.Contains(got, "duration 1.500") {
		t.Errorf("the second gap should be 1.500 s:\n%s", got)
	}
}

func TestConcatListNeverEmitsAZeroDuration(t *testing.T) {
	m := &Manifest{Frames: []FrameRecord{
		{File: "000001.jpg", AtMs: 0},
		{File: "000002.jpg", AtMs: 0},
	}}
	if strings.Contains(string(ConcatList(m)), "duration 0.000") {
		t.Error("a zero duration would make ffmpeg drop the frame")
	}
}

func TestCanvasSizeIsEvenBecauseH264NeedsIt(t *testing.T) {
	m := &Manifest{Frames: []FrameRecord{
		{W: 1281, H: 721},
		{W: 800, H: 600},
	}}
	w, h := canvasSize(m)
	if w != 1282 || h != 722 {
		t.Errorf("canvasSize = %dx%d, want 1282x722", w, h)
	}
	if w%2 != 0 || h%2 != 0 {
		t.Error("the canvas has an odd side")
	}
}

func TestCanvasSizeFallsBackWhenNothingIsKnown(t *testing.T) {
	w, h := canvasSize(&Manifest{Frames: []FrameRecord{{}}})
	if w != 1280 || h != 720 {
		t.Errorf("canvasSize = %dx%d, want 1280x720", w, h)
	}
}
