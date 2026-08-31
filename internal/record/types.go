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
const Version = 1

// Options are the capture settings that a recording was made with. They are
// stored in the manifest so a reader knows what it is looking at.
type Options struct {
	Quality  int    `json:"quality"`
	MaxWidth int    `json:"maxWidth"`
	FPS      int    `json:"fps"`
	Policy   string `json:"policy"`
}

// FrameRecord is one image in a recording.
type FrameRecord struct {
	Seq      int     `json:"seq"`
	File     string  `json:"file"`
	AtMs     int64   `json:"atMs"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	TargetID string  `json:"targetId,omitempty"`
}

// Event marks something that happened during a recording. The player draws a
// tick on the timeline for each one.
type Event struct {
	AtMs     int64  `json:"atMs"`
	T        string `json:"t"` // "tab", "stall", "resume", "note"
	TargetID string `json:"targetId,omitempty"`
	URL      string `json:"url,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Manifest describes a whole recording. It is written when the recording
// stops, and it is rebuilt from frames.jsonl by "atr record repair" when a
// recording was interrupted.
type Manifest struct {
	Version    int           `json:"version"`
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	StartedAt  time.Time     `json:"startedAt"`
	StoppedAt  time.Time     `json:"stoppedAt"`
	DurationMs int64         `json:"durationMs"`
	Browser    string        `json:"browser"`
	Options    Options       `json:"options"`
	Dropped    int           `json:"droppedFrames"`
	Bytes      int64         `json:"bytes"`
	Frames     []FrameRecord `json:"frames"`
	Events     []Event       `json:"events"`
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
