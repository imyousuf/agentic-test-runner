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
	got := string(ConcatList(m, EncodeOptions{}))
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
	if strings.Contains(string(ConcatList(m, EncodeOptions{})), "duration 0.000") {
		t.Error("a zero duration would make ffmpeg drop the frame")
	}
}

func TestConcatListHoldsARepeatedFileForItsRealTime(t *testing.T) {
	// A still page writes one file and points a run of frames at it. The
	// export must still last as long as the run did, so the same name appears
	// once per frame with the duration that frame covered.
	m := &Manifest{Frames: []FrameRecord{
		{File: "000001.jpg", AtMs: 0},
		{File: "000001.jpg", AtMs: 1000},
		{File: "000001.jpg", AtMs: 2000},
		{File: "000004.jpg", AtMs: 2100},
	}}
	got := string(ConcatList(m, EncodeOptions{}))

	if n := strings.Count(got, "000001.jpg"); n != 3 {
		t.Errorf("the shared file appears %d times, want 3:\n%s", n, got)
	}
	if n := strings.Count(got, "duration 1.000"); n != 2 {
		t.Errorf("the shared file should hold for a second each time:\n%s", got)
	}
	if !strings.Contains(got, "duration 0.100") {
		t.Errorf("the frame after the still run should last 0.100 s:\n%s", got)
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

/*
A session recording is mostly waiting.

A frame's duration in the concat list is the gap to the next frame, so a page
that sat still for half a minute became half a minute of one motionless
picture. The export now caps that, the same way the player does.
*/
func TestSkipIdleCapsAStillStretch(t *testing.T) {
	// Scored, as a version 2 recording is. The 2s gap ends in a frame that
	// changed; the 40s gap ends in one that did not.
	m := &Manifest{Version: 2, Frames: []FrameRecord{
		{File: "000001.jpg", AtMs: 0, Score: 0.4},
		{File: "000002.jpg", AtMs: 2000, Score: 0.4},
		{File: "000003.jpg", AtMs: 42000, Score: 0},
	}}

	real := string(ConcatList(m, EncodeOptions{}))
	if !strings.Contains(real, "duration 40.000") {
		t.Errorf("real time lost the long gap:\n%s", real)
	}

	cut := string(ConcatList(m, EncodeOptions{SkipIdle: true}))
	if strings.Contains(cut, "duration 40.000") {
		t.Errorf("the 40s freeze survived:\n%s", cut)
	}
	if !strings.Contains(cut, "duration 0.500") {
		t.Errorf("the long gap was not capped at IdleShownMs:\n%s", cut)
	}
	// The 2s gap ends in a frame that changed, so it is motion and must be
	// left alone. Capping by length alone would have sped up the part worth
	// watching to match the part that is not.
	if !strings.Contains(cut, "duration 2.000") {
		t.Errorf("a 2s gap was altered:\n%s", cut)
	}
}

// The player and the export have to agree, or a recording runs at one pace in
// the browser and another after export.
func TestTheExportUsesThePlayersIdleLength(t *testing.T) {
	if IdleShownMs != 500 {
		t.Fatalf("IdleShownMs is %d; web/src/activity.ts says IDLE_SHOWN_MS = 500", IdleShownMs)
	}
}
