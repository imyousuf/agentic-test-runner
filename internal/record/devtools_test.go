package record

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// logAt builds one line stamped at a wall clock offset from the start.
func logAt(start time.Time, offset time.Duration, ev LogEvent) LogEvent {
	ev.TS = start.Add(offset).UnixMilli()
	return ev
}

func TestRecorderWritesTheLogJournalInOrder(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Title: "Log", Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started

	r.Write(Image{JPEG: testJPEG(t, 64, 48), TargetID: "A1"})
	r.Log(logAt(start, 100*time.Millisecond, LogEvent{T: LogConsole, Level: "log", Text: "one"}))
	r.Log(logAt(start, 900*time.Millisecond, LogEvent{T: LogConsole, Level: "log", Text: "two"}))
	r.Log(logAt(start, 1500*time.Millisecond, LogEvent{
		T: LogReq, Method: "GET", URL: "https://example.test/a", ReqID: "1",
	}))

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(s.Root(), m.ID)
	rows, err := ReadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 lines, got %d", len(rows))
	}
	want := []int64{100, 900, 1500}
	for i, row := range rows {
		if row.AtMs != want[i] {
			t.Errorf("line %d is at %d ms, want %d", i, row.AtMs, want[i])
		}
	}
	if rows[0].Text != "one" || rows[2].URL != "https://example.test/a" {
		t.Errorf("the lines came back changed: %+v", rows)
	}

	if m.DevTools == nil {
		t.Fatal("the manifest has no devtools block")
	}
	if m.DevTools.Lines != 3 || m.DevTools.Bytes == 0 {
		t.Errorf("the block reports %d lines and %d bytes", m.DevTools.Lines, m.DevTools.Bytes)
	}
}

// A recording can start long after the session did. The line has to land where
// it happened on the recording clock, not where the recorder got to it.
func TestALineFromBeforeTheRecordingLandsAtZero(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})
	r.Log(logAt(r.started, -10*time.Second, LogEvent{T: LogConsole, Text: "before"}))

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadLog(filepath.Join(s.Root(), m.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AtMs != 0 {
		t.Fatalf("want one line at 0 ms, got %+v", rows)
	}
}

func TestEveryRecordingCarriesADevToolsBlock(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	// A quiet page and a recording from before the log existed are different
	// answers, and only a block with a zero in it gives the first one.
	if m.DevTools == nil {
		t.Fatal("a silent page still needs a devtools block")
	}
	if m.DevTools.Lines != 0 || m.DevTools.Errors != 0 {
		t.Errorf("want an empty block, got %+v", *m.DevTools)
	}
}

func TestAnErrorBecomesATimelineMark(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})

	r.Log(logAt(start, 100*time.Millisecond, LogEvent{
		T: LogError, Level: "error", Text: "TypeError: x is not a function",
	}))
	r.Log(logAt(start, 200*time.Millisecond, LogEvent{
		T: LogNetFail, Method: "GET", URL: "https://example.test/a", Text: "net::ERR_FAILED",
	}))
	r.Log(logAt(start, 300*time.Millisecond, LogEvent{
		T: LogRes, Method: "GET", URL: "https://example.test/b", Status: 500,
	}))
	// Neither of these is a failure, so neither earns a mark.
	r.Log(logAt(start, 400*time.Millisecond, LogEvent{
		T: LogConsole, Level: "warning", Text: "deprecated",
	}))
	r.Log(logAt(start, 500*time.Millisecond, LogEvent{
		T: LogRes, Method: "GET", URL: "https://example.test/c", Status: 200,
	}))

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, e := range m.Events {
		kinds = append(kinds, e.T)
	}
	want := []string{"error", "netfail", "netfail"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("the marks are %v, want %v", kinds, want)
	}
	if m.Events[0].AtMs != 100 {
		t.Errorf("the error mark is at %d ms, want 100", m.Events[0].AtMs)
	}
	if m.Events[2].Reason != "GET 500" {
		t.Errorf("the 500 reads %q", m.Events[2].Reason)
	}
	if m.DevTools.Errors != 3 {
		t.Errorf("the block counts %d errors, want 3", m.DevTools.Errors)
	}
}

func TestRepeatedFailuresCollapseIntoOneMark(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})

	for i := 0; i < 50; i++ {
		r.Log(logAt(start, time.Duration(i*4)*time.Millisecond, LogEvent{
			T: LogError, Level: "error", Text: "TypeError: x is not a function",
		}))
	}

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Events) != 1 {
		t.Fatalf("want one mark, got %d", len(m.Events))
	}
	if m.Events[0].Count != 50 {
		t.Errorf("the mark stands for %d, want 50", m.Events[0].Count)
	}
	// The mark collapsed, the log did not: every line is still on the disk.
	if m.DevTools.Lines != 50 {
		t.Errorf("the journal has %d lines, want 50", m.DevTools.Lines)
	}
	if m.DevTools.Errors != 50 {
		t.Errorf("the block counts %d errors, want 50", m.DevTools.Errors)
	}
}

// Far enough apart is not a repeat. The same error an hour later is news.
func TestTheSameErrorOutsideTheWindowIsANewMark(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})

	r.Log(logAt(start, 0, LogEvent{T: LogError, Text: "boom"}))
	r.Log(logAt(start, 5*time.Second, LogEvent{T: LogError, Text: "boom"}))

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Events) != 2 {
		t.Fatalf("want two marks, got %d", len(m.Events))
	}
}

func TestTheLogStopsAtItsSizeCap(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{
		Browser: "Chrome/1",
		Limits:  Limits{MaxLog: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})

	for i := 0; i < 100; i++ {
		r.Log(logAt(start, time.Duration(i)*time.Millisecond, LogEvent{
			T: LogConsole, Level: "log", Text: strings.Repeat("x", 50),
		}))
	}

	m, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if m.DevTools.Bytes > 400 {
		t.Errorf("the journal is %d bytes, over the 400 cap", m.DevTools.Bytes)
	}
	if m.DevTools.Lines == 0 || m.DevTools.Lines == 100 {
		t.Errorf("the cap kept %d of 100 lines", m.DevTools.Lines)
	}
	if m.DevTools.Dropped != 100-m.DevTools.Lines {
		t.Errorf("kept %d and dropped %d, which do not add to 100",
			m.DevTools.Lines, m.DevTools.Dropped)
	}
	// A full log must never end the recording.
	if len(m.Frames) == 0 {
		t.Error("the recording lost its frames when the log filled")
	}
}

func TestNoMaxLogLiftsTheCap(t *testing.T) {
	w, err := newLogWriter(t.TempDir(), NoMaxLog)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		w.write(LogEvent{T: LogConsole, Text: strings.Repeat("y", 200)})
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	if st := w.stats(); st.Lines != 2000 || st.Dropped != 0 {
		t.Errorf("want every line kept, got %+v", st)
	}
}

func TestADropLineCountsWhatItReports(t *testing.T) {
	w, err := newLogWriter(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	w.write(LogEvent{T: LogDrop, Count: 800})
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	if st := w.stats(); st.Dropped != 800 {
		t.Errorf("the writer counted %d drops, want 800", st.Dropped)
	}
}

func TestRepairRebuildsTheMarksFromTheLog(t *testing.T) {
	s := newTestStore(t)
	r, err := Start(s, StartOptions{Browser: "Chrome/1"})
	if err != nil {
		t.Fatal(err)
	}
	start := r.started
	id := r.ID()
	r.Write(Image{JPEG: testJPEG(t, 64, 48)})
	r.Log(logAt(start, 100*time.Millisecond, LogEvent{T: LogError, Text: "boom"}))
	r.Log(logAt(start, 150*time.Millisecond, LogEvent{T: LogError, Text: "boom"}))
	r.Log(logAt(start, 200*time.Millisecond, LogEvent{
		T: LogNetFail, Method: "GET", URL: "https://example.test/a", Text: "net::ERR_FAILED",
	}))
	if _, err := r.Stop(); err != nil {
		t.Fatal(err)
	}

	// Take the manifest away, which is the state a killed recorder leaves.
	dir := filepath.Join(s.Root(), id)
	if err := os.Remove(filepath.Join(dir, manifestFile)); err != nil {
		t.Fatal(err)
	}

	m, err := s.Repair(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Events) != 2 {
		t.Fatalf("the repair rebuilt %d marks, want 2", len(m.Events))
	}
	if m.Events[0].Count != 2 {
		t.Errorf("the repeated error came back with count %d, want 2", m.Events[0].Count)
	}
	if m.DevTools == nil || m.DevTools.Lines != 3 || m.DevTools.Errors != 3 {
		t.Errorf("the repaired block is %+v", m.DevTools)
	}

	// And the library row reports it without opening the journal again.
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Errors != 3 {
		t.Errorf("the summary reports %+v", list)
	}
}

func TestTrimReasonKeepsATooltipReadable(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := trimReason("line one\nline two")
	if got != "line one line two" {
		t.Errorf("the newline survived: %q", got)
	}
	if len([]rune(trimReason(long))) != reasonLimit {
		t.Errorf("a long reason came back %d runes", len([]rune(trimReason(long))))
	}
}
