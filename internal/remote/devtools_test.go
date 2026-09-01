package remote

import (
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// logSink collects what a Streamer fans out.
type logSink struct{ rows []record.LogEvent }

func (k *logSink) Frame(*Frame)           {}
func (k *logSink) Text([]byte)            {}
func (k *logSink) Log(ev record.LogEvent) { k.rows = append(k.rows, ev) }

func TestTheRateCapKeepsTwoHundredLinesASecond(t *testing.T) {
	s := NewStreamer(Options{})
	sink := &logSink{}
	s.AddSink(sink)

	start := time.Now()
	tp := &tap{s: s, targetID: "A1", windowAt: start}

	// A thousand lines in one window. Two hundred go through, and the rest are
	// refused rather than written.
	for i := 0; i < 1000; i++ {
		if tp.allow(start) {
			tp.s.log(record.LogEvent{T: record.LogConsole})
		}
	}
	if got := len(sink.rows); got != logCap {
		t.Fatalf("the cap let %d lines through, want %d", got, logCap)
	}
	if tp.refused != 1000-logCap {
		t.Errorf("the tap refused %d, want %d", tp.refused, 1000-logCap)
	}

	// The next window opens by saying what it lost. A gap in the log always
	// says it is a gap.
	if !tp.allow(start.Add(time.Second)) {
		t.Fatal("the next window refused the first line")
	}
	last := sink.rows[len(sink.rows)-1]
	if last.T != record.LogDrop || last.Count != 1000-logCap {
		t.Fatalf("the drop line is %+v", last)
	}
	if tp.refused != 0 {
		t.Errorf("the new window starts with %d refusals", tp.refused)
	}
}

func TestScrubTakesTheWholeQueryString(t *testing.T) {
	on := &tap{redact: true}
	off := &tap{}

	const u = "https://example.test/a?token=secret&page=2"
	if got := on.scrub(u); got != "https://example.test/a?…" {
		t.Errorf("redacted to %q", got)
	}
	if got := off.scrub(u); got != u {
		t.Errorf("changed a URL nobody asked to change: %q", got)
	}
	if got := on.scrub("https://example.test/a"); got != "https://example.test/a" {
		t.Errorf("a URL with no query came back as %q", got)
	}
}

// A CSP report names the request it blocked, and it names it in prose. The
// query string is just as secret there as it is in the url field.
func TestScrubTextTakesTheQueryOutOfAMessage(t *testing.T) {
	on := &tap{redact: true}
	off := &tap{}

	const msg = "security: Connecting to 'https://example.test/x?token=secret&a=b' " +
		"violates the policy \"connect-src 'self'\"."
	got := on.scrubText(msg)
	if strings.Contains(got, "secret") {
		t.Errorf("the secret survived: %q", got)
	}
	if !strings.Contains(got, "https://example.test/x?…'") {
		t.Errorf("the closing quote was eaten or the URL was lost: %q", got)
	}
	if got := off.scrubText(msg); got != msg {
		t.Errorf("changed a message nobody asked to change: %q", got)
	}

	// Two URLs in one line, and one of them has no query at all.
	two := on.scrubText("from https://a.test/p to https://b.test/q?k=v now")
	if two != "from https://a.test/p to https://b.test/q?… now" {
		t.Errorf("two URLs became %q", two)
	}
}

func TestConsoleLevelMapsWhatChromeSays(t *testing.T) {
	cases := map[string]string{
		"error": "error", "assert": "error",
		"warning": "warning", "warn": "warning",
		"debug": "debug", "verbose": "debug",
		"info": "info", "log": "log", "table": "log",
	}
	for in, want := range cases {
		if got := consoleLevel(in); got != want {
			t.Errorf("%q became %q, want %q", in, got, want)
		}
	}
}

func TestClipKeepsALineSmall(t *testing.T) {
	long := make([]byte, maxText+100)
	for i := range long {
		long[i] = 'x'
	}
	if got := clip(string(long), maxText); len([]rune(got)) != maxText {
		t.Errorf("a long line came back %d runes", len([]rune(got)))
	}
	if got := clip("short", maxText); got != "short" {
		t.Errorf("a short line changed: %q", got)
	}
}

func TestTheHubKeepsABacklogForALateViewer(t *testing.T) {
	h := NewHub()
	if h.Backlog() != nil {
		t.Fatal("a hub that has heard nothing offered a backlog")
	}
	for i := 0; i < logRing+50; i++ {
		h.Log(record.LogEvent{T: record.LogConsole, Text: "x"})
	}
	if got := len(h.recent); got != logRing {
		t.Errorf("the ring holds %d rows, want %d", got, logRing)
	}
	if h.Backlog() == nil {
		t.Error("the hub has rows but offered no backlog")
	}
}
