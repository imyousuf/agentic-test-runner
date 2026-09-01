package record

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// devtoolsFile is the log journal of one recording.
const devtoolsFile = "devtools.jsonl"

// DefaultMaxLog caps the log journal of one recording.
//
// A broken page can report the same error thousands of times a minute, and a
// busy application makes thousands of requests. The frames are the recording,
// so the log must never be the reason a disk fills up.
const DefaultMaxLog int64 = 20 << 20

// NoMaxLog lifts the cap. Limits.MaxLog is zero when nobody chose, and zero
// means the default, so "no limit" needs a value of its own.
const NoMaxLog int64 = -1

// The kinds a LogEvent can be.
const (
	LogConsole = "console" // a console.log, warn, error, info or debug call
	LogError   = "error"   // an uncaught exception
	LogIssue   = "issue"   // a browser log entry: CORS, CSP, a failed subresource
	LogReq     = "req"     // a request left the browser
	LogRes     = "res"     // it came back
	LogNetFail = "netfail" // it failed, was blocked, or was cancelled
	LogTap     = "tap"     // the log moved to another tab
	LogDrop    = "drop"    // the rate cap refused some lines
)

// LogEvent is one thing the page reported.
//
// It is a plain struct on purpose. The CDP layer builds it and this package
// stores it, so a recording never depends on a browser protocol, and a reader
// years from now needs nothing but the field names.
//
// A LogEvent never carries a request body, a response body, or a header. A
// login post holds the password in its body and the session in its headers,
// and the recorder already refuses to write what somebody typed.
type LogEvent struct {
	// AtMs is the position on the recording clock. The recorder fills it in.
	AtMs int64 `json:"atMs"`
	// TS is the wall clock in unix milliseconds, as the tap saw it. A
	// recording can start at any point in a session, so the tap cannot know
	// AtMs and the recorder cannot know when the event really happened.
	TS int64  `json:"ts"`
	T  string `json:"t"`

	Level string `json:"level,omitempty"` // debug, info, log, warning, error
	Text  string `json:"text,omitempty"`
	Stack string `json:"stack,omitempty"`

	ReqID  string `json:"reqId,omitempty"` // ties a res or a netfail to its req
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	Status int    `json:"status,omitempty"`
	Kind   string `json:"kind,omitempty"` // document, xhr, fetch, script, image
	Bytes  int64  `json:"bytes,omitempty"`
	DurMs  int64  `json:"durMs,omitempty"`

	// Count says how many identical lines this one stands for. A rate cap drop
	// reports what it refused, and a repeated failure reports what it
	// collapsed. Zero and one both mean one.
	Count    int    `json:"count,omitempty"`
	TargetID string `json:"targetId,omitempty"`
}

// DevTools is what the manifest says about the log journal.
//
// It is present on every recording made since the log existed, even when the
// page said nothing at all. A quiet page and a recording from before the
// feature are different answers, and only a block with a zero in it can give
// the first one.
type DevTools struct {
	Lines   int   `json:"lines"`
	Bytes   int64 `json:"bytes"`
	Dropped int   `json:"dropped"`
	// Errors counts the failures the page reported, not the marks they made.
	// Fifty repeats of one error collapse into one mark, and a list that says
	// "1 error" about that session is telling the wrong story.
	Errors int `json:"errors"`
	// What the capture was allowed to keep. A reader has to be able to tell
	// "this recording kept no headers" from "this recording predates headers".
	Bodies      bool `json:"bodies"`
	Headers     bool `json:"headers"`
	RedactQuery bool `json:"redactQuery"`
}

// reasonLimit keeps one failure message short enough to read in a tooltip.
const reasonLimit = 200

// failure reports the timeline event this line deserves, and whether it
// deserves one at all.
//
// Only a failure earns a mark. A console.log does not, and neither does a
// request that worked. The mark row is a map of a session, and a map that
// shows everything shows nothing.
func (ev LogEvent) failure() (Event, bool) {
	switch {
	case ev.T == LogError:
		return Event{T: "error", Reason: trimReason(ev.Text), TargetID: ev.TargetID}, true

	case (ev.T == LogConsole || ev.T == LogIssue) && ev.Level == "error":
		return Event{T: "error", Reason: trimReason(ev.Text), TargetID: ev.TargetID}, true

	case ev.T == LogNetFail:
		return Event{
			T: "netfail", URL: ev.URL, TargetID: ev.TargetID,
			Reason: trimReason(strings.TrimSpace(ev.Method + " " + ev.Text)),
		}, true

	case ev.T == LogRes && ev.Status >= 400:
		return Event{
			T: "netfail", URL: ev.URL, TargetID: ev.TargetID,
			Reason: trimReason(fmt.Sprintf("%s %d", ev.Method, ev.Status)),
		}, true
	}
	return Event{}, false
}

// failWindowMs is how close two identical failures have to be for the second
// one to raise a count instead of adding a mark.
const failWindowMs = 1000

// maxFailKeys caps the dedupe cache. It is a cache, not a record: a page that
// reports a thousand different errors must not grow it a thousand entries.
const maxFailKeys = 256

// failMark remembers a failure that is already on the timeline.
type failMark struct {
	idx  int   // where the event sits in the event slice
	last int64 // when it was last seen, on the recording clock
}

// failIndex collapses repeated failures into one mark that carries a count.
//
// A broken page reports the same error fifty times in a moment. Fifty marks in
// one place hide every other mark on the bar, so identical failures inside one
// second become one mark that says how many.
//
// The recorder feeds it one line at a time while it records, and a repair feeds
// it a whole journal afterwards. They share it so that a repaired manifest
// carries the marks a clean stop would have written.
type failIndex struct {
	seen map[string]*failMark
}

func newFailIndex() *failIndex { return &failIndex{seen: map[string]*failMark{}} }

// add returns events with the failure of ev folded into it, and events
// unchanged when ev is not a failure.
func (fx *failIndex) add(events []Event, ev LogEvent) []Event {
	e, ok := ev.failure()
	if !ok {
		return events
	}
	e.AtMs = ev.AtMs
	key := e.T + "\x00" + e.URL + "\x00" + e.Reason

	if m := fx.seen[key]; m != nil && ev.AtMs-m.last <= failWindowMs && m.idx < len(events) {
		events[m.idx].Count = atLeastOne(events[m.idx].Count) + 1
		m.last = ev.AtMs
		return events
	}
	if len(fx.seen) >= maxFailKeys {
		fx.seen = map[string]*failMark{}
	}
	e.Count = 1
	events = append(events, e)
	fx.seen[key] = &failMark{idx: len(events) - 1, last: ev.AtMs}
	return events
}

// IsFailure reports whether an event kind is one of the failures. The player
// draws those in red and ranks them first, and a list counts them.
func IsFailure(t string) bool { return t == "error" || t == "netfail" }

// manifestErrors is how many failures a finished recording holds.
//
// The count comes from the log block when there is one. A recording made
// before the log existed has no block, so its marks are all there is to count.
func manifestErrors(m *Manifest) int {
	if m.DevTools != nil {
		return m.DevTools.Errors
	}
	return countFailures(m.Events)
}

// countFailures totals the failures behind the marks. A mark that stands for
// fifty repeats counts fifty.
func countFailures(events []Event) int {
	n := 0
	for _, e := range events {
		if IsFailure(e.T) {
			n += atLeastOne(e.Count)
		}
	}
	return n
}

func trimReason(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= reasonLimit {
		return s
	}
	return s[:reasonLimit-1] + "…"
}

// logWriter appends the log journal of one recording.
//
// It is the same shape as the frame journal, and for the same reason: a
// recording that was killed still has everything up to the last line, so
// "atr record repair" can rebuild the marks from it.
type logWriter struct {
	buf  *bufio.Writer
	file *os.File
	max  int64

	lines   int
	bytes   int64
	dropped int
	full    bool
}

func newLogWriter(dir string, max int64) (*logWriter, error) {
	f, err := os.Create(filepath.Join(dir, devtoolsFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create the log journal: %w", err)
	}
	switch {
	case max == NoMaxLog:
		max = 0 // zero here means no cap; the caller asked for that
	case max <= 0:
		max = DefaultMaxLog
	}
	return &logWriter{buf: bufio.NewWriter(f), file: f, max: max}, nil
}

// write appends one line. A line refused by the cap is counted, never an
// error: a full log must not end a recording.
func (w *logWriter) write(ev LogEvent) {
	if w == nil {
		return
	}
	// A drop line reports what somebody else refused, so it is counted even
	// when it is the line that does not fit.
	if ev.T == LogDrop {
		w.dropped += atLeastOne(ev.Count)
	}
	if w.full {
		if ev.T != LogDrop {
			w.dropped++
		}
		return
	}

	line, err := json.Marshal(ev)
	if err != nil {
		w.dropped++
		return
	}
	if w.max > 0 && w.bytes+int64(len(line))+1 > w.max {
		w.full = true
		if ev.T != LogDrop {
			w.dropped++
		}
		return
	}
	if _, err := w.buf.Write(append(line, '\n')); err != nil {
		w.dropped++
		return
	}
	w.lines++
	w.bytes += int64(len(line)) + 1
	// Flush often enough that a crash loses a handful of lines, and rarely
	// enough that the disk is not the bottleneck. The frame journal does the
	// same, for the same reason.
	if w.lines%20 == 0 {
		_ = w.buf.Flush()
	}
}

func (w *logWriter) stats() DevTools {
	if w == nil {
		return DevTools{}
	}
	return DevTools{Lines: w.lines, Bytes: w.bytes, Dropped: w.dropped}
}

func (w *logWriter) close() error {
	if w == nil {
		return nil
	}
	if err := w.buf.Flush(); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("failed to flush the log journal: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close the log journal: %w", err)
	}
	return nil
}

func atLeastOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// recoverLog rebuilds the timeline marks and the log block of an interrupted
// recording from its log journal.
//
// The marks are the point. A session that was killed is the one somebody most
// wants to look at, and the failures in it are what they are looking for.
//
// It returns a nil block when the recording has no journal, because that is a
// recording from before the log existed and an empty block would claim the
// page said nothing. The capture flags come back false: only the manifest knew
// what the capture was allowed to keep, and that manifest was never written.
func recoverLog(dir string) ([]Event, *DevTools) {
	log, err := ReadLog(dir)
	if err != nil {
		return []Event{}, nil
	}

	events := []Event{}
	dt := DevTools{Lines: len(log)}
	fx := newFailIndex()
	for _, ev := range log {
		events = fx.add(events, ev)
		if ev.T == LogDrop {
			dt.Dropped += atLeastOne(ev.Count)
		}
	}
	if st, serr := os.Stat(filepath.Join(dir, devtoolsFile)); serr == nil {
		dt.Bytes = st.Size()
	}
	dt.Errors = countFailures(events)
	return events, &dt
}

// ReadLog reads the log journal of a recording.
//
// A truncated last line is expected after a crash, exactly as it is in the
// frame journal, so it is dropped rather than reported.
func ReadLog(dir string) ([]LogEvent, error) {
	f, err := os.Open(filepath.Join(dir, devtoolsFile))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []LogEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev LogEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}
