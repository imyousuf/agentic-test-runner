package remote

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// Logger is a Sink that also wants what the page reported, not only the
// pixels. A Hub implements it so that a watcher sees the console live, and
// RecorderSink implements it so that a recording keeps it.
type Logger interface {
	Log(record.LogEvent)
}

// log fans one line out to the sinks that want them. The caller must not hold
// s.mu.
func (s *Streamer) log(ev record.LogEvent) {
	s.mu.Lock()
	sinks := make([]Sink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.Unlock()
	for _, k := range sinks {
		if l, ok := k.(Logger); ok {
			l.Log(ev)
		}
	}
}

// SetRedactQuery drops the query string from every URL the log keeps.
//
// A URL is metadata and a session token in a query string is not, and nothing
// out here can tell which parameter is the secret. So the flag takes the whole
// query or none of it.
func (s *Streamer) SetRedactQuery(on bool) {
	s.mu.Lock()
	s.redactQuery = on
	s.mu.Unlock()
}

// RedactQuery reports whether URLs are being stripped.
func (s *Streamer) RedactQuery() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.redactQuery
}

const (
	// logCap is how many lines a second the tap will write. A page in a loop
	// can report thousands, and past a couple of hundred nobody is reading
	// them anyway.
	logCap = 200
	// maxPending caps the table of requests that have not settled. It exists
	// only to hold the method and the start time until the response arrives,
	// and a page that leaves ten thousand requests open must not grow it.
	maxPending = 1024
	// maxText and maxStack keep one line small enough that a busy page cannot
	// fill the journal with a single message.
	maxText  = 4000
	maxStack = 4000
	// stackDepth is how many frames of a stack trace the log keeps. The top of
	// a stack says where the error is; the rest is framework.
	stackDepth = 10
)

// pendingReq is a request the tap has seen leave, and not yet come back.
type pendingReq struct {
	method string
	url    string
	kind   string
	status int
	start  time.Time
}

// tap turns what one page reports into log lines.
//
// It follows the streamed tab, and only that tab. A popup that never comes to
// the front is not in the log, and neither is the old tab after the streamer
// followed the foreground somewhere else. Every switch writes a "tap" line, so
// a person reading the journal can see the seam instead of guessing at it.
//
// Everything in it is touched by one goroutine: rod dispatches the callbacks of
// a single EachEvent call in order, so the tap needs no lock.
type tap struct {
	s        *Streamer
	targetID string
	redact   bool

	pending  map[proto.NetworkRequestID]*pendingReq
	windowAt time.Time
	inWindow int
	refused  int
}

// startTap subscribes to what the page reports. It returns at once; the
// subscription lives on the context of the bound page, so cancelling the
// stream ends it.
func (s *Streamer) startTap(bound *rod.Page, targetID string) {
	zero := 0
	// Zero buffers because the tap keeps no body. Chrome would otherwise hold
	// every response in memory for a Network.getResponseBody nobody calls.
	_ = proto.NetworkEnable{
		MaxTotalBufferSize:    &zero,
		MaxResourceBufferSize: &zero,
		MaxPostDataSize:       &zero,
	}.Call(bound)
	_ = proto.RuntimeEnable{}.Call(bound)
	_ = proto.LogEnable{}.Call(bound)

	t := &tap{
		s:        s,
		targetID: targetID,
		redact:   s.RedactQuery(),
		pending:  map[proto.NetworkRequestID]*pendingReq{},
		windowAt: time.Now(),
	}

	url := ""
	if info, err := bound.Info(); err == nil {
		url = info.URL
	}
	seam := record.LogEvent{
		T: record.LogTap, TS: nowMs(), URL: t.scrub(url),
		Text: "the log follows this tab",
	}
	// Kept as well as sent: a recording is started after the stream is, and its
	// journal has to open with the seam rather than in the middle of a page.
	s.mu.Lock()
	s.lastTap = &seam
	s.mu.Unlock()
	t.put(seam)

	go bound.EachEvent(
		func(e *proto.RuntimeConsoleAPICalled) { t.console(e) },
		func(e *proto.RuntimeExceptionThrown) { t.exception(e) },
		func(e *proto.LogEntryAdded) { t.entry(e) },
		func(e *proto.NetworkRequestWillBeSent) { t.reqSent(e) },
		func(e *proto.NetworkResponseReceived) { t.resReceived(e) },
		func(e *proto.NetworkLoadingFinished) { t.reqDone(e) },
		func(e *proto.NetworkLoadingFailed) { t.reqFailed(e) },
	)()
}

func (t *tap) console(e *proto.RuntimeConsoleAPICalled) {
	t.put(record.LogEvent{
		T:     record.LogConsole,
		TS:    stampMs(float64(e.Timestamp)),
		Level: consoleLevel(string(e.Type)),
		Text:  consoleText(e.Args),
		Stack: stackText(e.StackTrace),
	})
}

func (t *tap) exception(e *proto.RuntimeExceptionThrown) {
	d := e.ExceptionDetails
	if d == nil {
		return
	}
	// The exception object carries the message a person recognises; the details
	// text is the wrapper around it ("Uncaught").
	text := d.Text
	if d.Exception != nil {
		if s := objText(d.Exception); s != "" {
			text = s
		}
	}
	t.put(record.LogEvent{
		T: record.LogError, TS: stampMs(float64(e.Timestamp)),
		Level: "error", Text: clip(text, maxText),
		URL: t.scrub(d.URL), Stack: stackText(d.StackTrace),
	})
}

func (t *tap) entry(e *proto.LogEntryAdded) {
	en := e.Entry
	if en == nil {
		return
	}
	// Chrome reports an uncaught error and a failed request here as well, and
	// both already have a line of their own with more in it. Keeping these too
	// would put two marks on the timeline for one failure.
	switch en.Source {
	case proto.LogLogEntrySourceJavascript, proto.LogLogEntrySourceNetwork:
		return
	}
	t.put(record.LogEvent{
		T: record.LogIssue, TS: stampMs(float64(en.Timestamp)),
		Level: consoleLevel(string(en.Level)),
		Text:  clip(strings.TrimSpace(string(en.Source)+": "+en.Text), maxText),
		URL:   t.scrub(en.URL), ReqID: string(en.NetworkRequestID),
		Stack: stackText(en.StackTrace),
	})
}

func (t *tap) reqSent(e *proto.NetworkRequestWillBeSent) {
	if e.Request == nil {
		return
	}
	url := t.scrub(e.Request.URL)
	if len(t.pending) < maxPending {
		t.pending[e.RequestID] = &pendingReq{
			method: e.Request.Method, url: url,
			kind: string(e.Type), start: time.Now(),
		}
	}
	t.put(record.LogEvent{
		T: record.LogReq, TS: nowMs(), ReqID: string(e.RequestID),
		Method: e.Request.Method, URL: url, Kind: string(e.Type),
	})
}

// resReceived only remembers the status. The line goes out when the request
// settles, so that one row carries the status, the size and how long it took.
func (t *tap) resReceived(e *proto.NetworkResponseReceived) {
	p := t.pending[e.RequestID]
	if p == nil || e.Response == nil {
		return
	}
	p.status = e.Response.Status
	if p.kind == "" {
		p.kind = string(e.Type)
	}
}

func (t *tap) reqDone(e *proto.NetworkLoadingFinished) {
	ev := record.LogEvent{
		T: record.LogRes, TS: nowMs(), ReqID: string(e.RequestID),
		Bytes: int64(e.EncodedDataLength),
	}
	if p := t.pending[e.RequestID]; p != nil {
		ev.Method, ev.URL, ev.Kind, ev.Status = p.method, p.url, p.kind, p.status
		ev.DurMs = time.Since(p.start).Milliseconds()
		delete(t.pending, e.RequestID)
	}
	t.put(ev)
}

func (t *tap) reqFailed(e *proto.NetworkLoadingFailed) {
	reason := e.ErrorText
	switch {
	case e.CorsErrorStatus != nil && e.CorsErrorStatus.CorsError != "":
		reason = "CORS " + string(e.CorsErrorStatus.CorsError)
	case e.BlockedReason != "":
		reason = "blocked: " + string(e.BlockedReason)
	case e.Canceled:
		reason = "cancelled"
	}
	ev := record.LogEvent{
		T: record.LogNetFail, TS: nowMs(), ReqID: string(e.RequestID),
		Kind: string(e.Type), Text: clip(reason, maxText),
	}
	if p := t.pending[e.RequestID]; p != nil {
		ev.Method, ev.URL = p.method, p.url
		ev.DurMs = time.Since(p.start).Milliseconds()
		delete(t.pending, e.RequestID)
	}
	t.put(ev)
}

// put writes one line unless the rate cap refuses it.
func (t *tap) put(ev record.LogEvent) {
	if !t.allow(time.Now()) {
		return
	}
	if ev.TargetID == "" {
		ev.TargetID = t.targetID
	}
	// Here rather than in each handler, because a URL turns up in prose as
	// often as it does in the url field: a CSP report names the request it
	// blocked, and a stack names the script it came from. Redaction that only
	// covered the field would be a promise the log does not keep.
	ev.Text = t.scrubText(ev.Text)
	ev.Stack = t.scrubText(ev.Stack)
	t.s.log(ev)
}

// allow reports whether the tap may write one more line now.
//
// The refusals are never silent. The next window opens with a drop line saying
// how many went, so a gap in the log always says it is a gap.
func (t *tap) allow(now time.Time) bool {
	if now.Sub(t.windowAt) >= time.Second {
		refused := t.refused
		t.windowAt, t.inWindow, t.refused = now, 0, 0
		if refused > 0 {
			t.s.log(record.LogEvent{
				T: record.LogDrop, TS: nowMs(), Count: refused,
				TargetID: t.targetID,
				Text: fmt.Sprintf("the rate cap dropped %d lines at %d a second",
					refused, logCap),
			})
		}
	}
	if t.inWindow >= logCap {
		t.refused++
		return false
	}
	t.inWindow++
	return true
}

// scrub takes the query string off a URL when the recording asked for it.
func (t *tap) scrub(u string) string {
	if !t.redact || u == "" {
		return u
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i] + "?…"
	}
	return u
}

// urlInText finds an http URL inside a message. It stops at whitespace and at
// the quote characters a message wraps a URL in, so the closing quote of
// "Connecting to 'https://host/p?q'" is not read as part of the query.
var urlInText = regexp.MustCompile(`https?://[^\s'"<>` + "`" + `]+`)

// scrubText takes the query string off every URL in a message.
func (t *tap) scrubText(s string) string {
	if !t.redact || s == "" {
		return s
	}
	return urlInText.ReplaceAllStringFunc(s, t.scrub)
}

// consoleLevel maps what Chrome calls a severity onto the five the log uses.
func consoleLevel(t string) string {
	switch t {
	case "error", "assert":
		return "error"
	case "warning", "warn":
		return "warning"
	case "debug", "verbose":
		return "debug"
	case "info":
		return "info"
	default:
		return "log"
	}
}

func consoleText(args []*proto.RuntimeRemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if s := objText(a); s != "" {
			parts = append(parts, s)
		}
	}
	return clip(strings.Join(parts, " "), maxText)
}

func objText(o *proto.RuntimeRemoteObject) string {
	switch {
	case o == nil:
		return ""
	case !o.Value.Nil():
		return o.Value.String()
	case o.Description != "":
		return o.Description
	default:
		return string(o.Type)
	}
}

// stackText flattens a stack trace to one line per frame.
//
// It is a string and not a structure on purpose. The dock shows it as text, and
// a journal that a person can read with "less" is worth more than one that
// needs a parser.
func stackText(st *proto.RuntimeStackTrace) string {
	if st == nil || len(st.CallFrames) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range st.CallFrames {
		if i >= stackDepth {
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		name := f.FunctionName
		if name == "" {
			name = "(anonymous)"
		}
		fmt.Fprintf(&b, "%s (%s:%d:%d)", name, f.URL, f.LineNumber+1, f.ColumnNumber+1)
	}
	return clip(b.String(), maxStack)
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// nowMs is the wall clock the recorder turns into a position on the timeline.
func nowMs() int64 { return time.Now().UnixMilli() }

// stampMs converts a CDP timestamp, which is milliseconds since the epoch as a
// float. A zero stamp means the event carried none, and the recorder then falls
// back to the moment it stored the line.
func stampMs(ts float64) int64 {
	if ts <= 0 {
		return nowMs()
	}
	return int64(ts)
}
