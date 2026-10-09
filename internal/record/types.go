// Package record captures what the browser shows and writes it to disk as a
// recording.
//
// A recording is a directory of JPEG frames and a manifest. That is the
// format, not an intermediate step towards one. The web player draws the
// frames on a canvas and uses the manifest for timing, so watching a recording
// needs no video encoder at all. MP4 is an export, and it is the only thing
// that needs ffmpeg.
package record

import "time"

// Version is the manifest schema version.
//
// Version 2 added the per frame activity score, and let more than one frame
// point at the same file. A version 1 recording has neither, so a player has
// to fall back to timing alone for those.
const Version = 2

// Options are the capture settings that a recording was made with. They are
// stored in the manifest so a reader knows what it is looking at.
type Options struct {
	Quality  int    `json:"quality"`
	MaxWidth int    `json:"maxWidth"`
	FPS      int    `json:"fps"`
	Policy   string `json:"policy"`

	// How the activity scores were produced, and the threshold the recorder
	// suggests for them. A player may use another threshold: the score itself
	// carries no threshold, so a finished recording can still be re-judged.
	RefLagMs        int     `json:"refLagMs,omitempty"`
	ChangeThreshold float64 `json:"changeThreshold,omitempty"`
	DedupeEpsilon   float64 `json:"dedupeEpsilon,omitempty"`
	KeepEveryMs     int     `json:"keepEveryMs,omitempty"`
}

// FrameRecord is one image in a recording.
//
// File is not unique. A run of frames that show the same picture all point at
// the one file that was written for the run, so the recording keeps every
// moment on the timeline without keeping a copy of every moment on the disk.
type FrameRecord struct {
	Seq      int     `json:"seq"`
	File     string  `json:"file"`
	AtMs     int64   `json:"atMs"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	TargetID string  `json:"targetId,omitempty"`
	// Score is how much this frame differs from the frame one reference lag
	// earlier, from 0 to 1. It is absent in a version 1 recording.
	Score float64 `json:"score,omitempty"`
}

// Event marks something that happened during a recording. The player draws a
// mark on the timeline for each one, so a person can find the moment an action
// took place instead of reading the whole session.
//
// A "type" event says that somebody typed, and never what they typed. A
// recording is a file on a disk, and a password is typed the same way as a
// search term, so the text is not ours to keep.
type Event struct {
	AtMs int64 `json:"atMs"`
	// T is one of "tab", "nav", "click", "type", "key", "error", "netfail",
	// "stall", "resume" or "note".
	T        string `json:"t"`
	TargetID string `json:"targetId,omitempty"`
	URL      string `json:"url,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Count is how many identical events this one stands for. A broken page
	// reports the same error fifty times in a moment, and fifty marks in one
	// place hide every other mark on the bar. Zero and one both mean one.
	Count int `json:"count,omitempty"`
}

// Manifest describes a whole recording. It is written when the recording
// stops, and it is rebuilt from frames.jsonl by "atr record repair" when a
// recording was interrupted.
type Manifest struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	StartedAt  time.Time `json:"startedAt"`
	StoppedAt  time.Time `json:"stoppedAt"`
	DurationMs int64     `json:"durationMs"`
	Browser    string    `json:"browser"`
	Options    Options   `json:"options"`
	Dropped    int       `json:"droppedFrames"`
	// Shared counts the frames that point at a file another frame owns. No
	// frame is lost; this is only how much of the disk the sharing saved.
	Shared int           `json:"sharedFrames"`
	Bytes  int64         `json:"bytes"`
	Frames []FrameRecord `json:"frames"`
	Events []Event       `json:"events"`
	// DevTools describes devtools.jsonl. It is nil only on a recording made
	// before the log existed, and a player reads that as "no dock", not as an
	// empty one.
	DevTools *DevTools `json:"devtools,omitempty"`
}

// Summary is one row in the recordings list. It carries what the library view
// shows without reading every frame entry of every manifest.
type Summary struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
	Frames     int       `json:"frames"`
	Bytes      int64     `json:"bytes"`
	HasMP4     bool      `json:"hasMp4"`
	Partial    bool      `json:"partial"` // no manifest.json; needs "atr record repair"
	// Live means this recording is being written right now, so it has no
	// manifest yet and must not be offered for repair or deletion.
	Live   bool   `json:"live"`
	Source string `json:"source,omitempty"` // what is writing it: "cli" or "live-view"
	// Errors is how many failures the page reported, so a list can say which
	// session is the one worth opening.
	Errors int `json:"errors"`
}

// Status is what a caller sees while a recording runs.
type Status struct {
	Recording bool   `json:"recording"`
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	ElapsedMs int64  `json:"elapsedMs"`
	Frames    int    `json:"frames"`
	Bytes     int64  `json:"bytes"`
	Dropped   int    `json:"dropped"`
}
