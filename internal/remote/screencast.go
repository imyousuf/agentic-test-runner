package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/imyousuf/agentic-test-runner/internal/record"
)

// Options tune the stream.
type Options struct {
	Quality  int
	MaxWidth int
	FPS      int
	ViewOnly bool
}

// PageInfo describes one tab for the client.
type PageInfo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// Streamer attaches to a browser and streams one page at a time.
//
// Chrome does not composite a background tab, so it emits no frames for one.
// The streamed page is therefore always brought to the front, and a watchdog
// notices when something else takes the foreground.
type Streamer struct {
	hub  *Hub // the viewer hub, when one is attached; nil for a headless recording
	opts Options

	// streamMu serialises stream() end to end; mu guards the fields below.
	streamMu sync.Mutex

	mu       sync.Mutex
	sinks    []Sink
	browser  *rod.Browser
	page     *rod.Page
	targetID proto.TargetTargetID
	cancel   context.CancelFunc
	seq      int
	lastAt   time.Time
	lastBeat time.Time
	lastType time.Time
	live     bool
	closed   bool
	// switching is true while stream() is between tearing the old stream down
	// and committing the new one, so the watchdog does not mistake that window
	// for a dead view. gen identifies the current stream, so a frame callback
	// from a cancelled one cannot write over its successor's state.
	switching bool
	gen       int
	policy    string // "follow", "pin", or "hold"

	// drag is the HTML5 drag Chrome handed back, if one is in flight.
	drag *dragSession

	// redactQuery strips the query string from every URL the log keeps.
	redactQuery bool

	// lastTap is the seam line of the tab the tap is on. A recording is started
	// after the stream is, so without this the journal would open in the middle
	// of a page and never say which page it is.
	lastTap *record.LogEvent

	// last holds the most recent frame. A static page produces one frame and
	// then nothing, so a viewer that connects later needs this to see anything.
	last *Frame

	// lastPages is the tab list as it was last sent, so the poll can stay quiet
	// while nothing about the tabs has changed.
	lastPages []byte
}

// NewStreamer builds a streamer with no sinks. Attach them with AddSink.
func NewStreamer(opts Options) *Streamer {
	if opts.Quality <= 0 {
		opts.Quality = 60
	}
	if opts.MaxWidth <= 0 {
		opts.MaxWidth = 1600
	}
	if opts.FPS <= 0 {
		opts.FPS = 20
	}
	return &Streamer{opts: opts, policy: "follow"}
}

// AddSink attaches a consumer. Frames go to every sink that is attached when
// the frame arrives.
//
// A Hub is remembered separately so that a status message can report the
// viewer count. A streamer with no hub reports zero viewers, which is correct
// for "atr record" running on its own.
func (s *Streamer) AddSink(k Sink) {
	if k == nil {
		return
	}
	s.mu.Lock()
	s.sinks = append(s.sinks, k)
	if h, ok := k.(*Hub); ok && s.hub == nil {
		s.hub = h
	}
	seam := s.lastTap
	s.mu.Unlock()

	// Tell a late sink which tab it is looking at, before anything else.
	if l, ok := k.(Logger); ok && seam != nil {
		l.Log(*seam)
	}
}

// RemoveSink detaches a consumer. Stopping a recording must not stop the
// stream for the viewers who are still watching.
func (s *Streamer) RemoveSink(k Sink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.sinks[:0]
	for _, existing := range s.sinks {
		if existing != k {
			out = append(out, existing)
		}
	}
	s.sinks = out
}

// emit fans one frame out. The caller must not hold s.mu.
func (s *Streamer) emit(f *Frame) {
	s.mu.Lock()
	sinks := make([]Sink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.Unlock()
	for _, k := range sinks {
		k.Frame(f)
	}
}

// Action is something a viewer did to the page, on its way to the timeline of
// a recording.
//
// Detail says what kind of thing it was, never what it contained. A click
// carries the button, a key carries the key name, and typing carries nothing
// at all: a password is typed the same way as a search term, so the text is
// not ours to write to a disk.
type Action struct {
	Kind   string // "click", "type" or "key"
	Detail string
}

// Actor is a Sink that also wants the actions, not only the pixels. A Hub does
// not implement it, because a viewer already saw the action it sent.
type Actor interface {
	Action(Action)
}

// act fans one action out to the sinks that want them. The caller must not
// hold s.mu.
func (s *Streamer) act(a Action) {
	s.mu.Lock()
	sinks := make([]Sink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.Unlock()
	for _, k := range sinks {
		if actor, ok := k.(Actor); ok {
			actor.Action(a)
		}
	}
}

// emitText fans one control message out. The caller must not hold s.mu.
func (s *Streamer) emitText(msg []byte) {
	s.mu.Lock()
	sinks := make([]Sink, len(s.sinks))
	copy(sinks, s.sinks)
	s.mu.Unlock()
	for _, k := range sinks {
		k.Text(msg)
	}
}

// viewers reports how many people are watching, or zero when no hub is
// attached.
func (s *Streamer) viewers() int {
	s.mu.Lock()
	hub := s.hub
	s.mu.Unlock()
	if hub == nil {
		return 0
	}
	return hub.Count()
}

// ErrViewOnly is returned when input reaches a read-only streamer.
var ErrViewOnly = errors.New("the live view is read only")

// viewOnly reports whether input must be refused. Enforcing here rather than in
// a single handler means the REST, WebSocket and any future surface all inherit
// it, instead of each having to remember the check.
func (s *Streamer) viewOnly() error {
	if s.opts.ViewOnly {
		return ErrViewOnly
	}
	return nil
}

// Live reports whether frames are actually flowing.
func (s *Streamer) Live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
}

// Attach connects to the browser at the given CDP endpoint.
//
// NoDefaultDevice matters: rod otherwise applies its default device emulation
// to every page it touches, which would rewrite the viewport and the user agent
// of the tabs ATR is already driving. The viewer never owns the browser.
func (s *Streamer) Attach(cdpURL string) error {
	browser := rod.New().ControlURL(cdpURL).NoDefaultDevice()
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("failed to attach to the browser at %s: %w", cdpURL, err)
	}
	s.mu.Lock()
	s.browser = browser
	s.mu.Unlock()
	return nil
}

// Version reports the browser build, for the start-up banner.
func (s *Streamer) Version() string {
	s.mu.Lock()
	browser := s.browser
	s.mu.Unlock()
	if browser == nil {
		return "unknown"
	}
	v, err := proto.BrowserGetVersion{}.Call(browser)
	if err != nil {
		return "unknown"
	}
	return v.Product
}

// Pages lists the tabs.
func (s *Streamer) Pages() ([]PageInfo, error) {
	s.mu.Lock()
	browser := s.browser
	current := s.targetID
	s.mu.Unlock()
	if browser == nil {
		return nil, fmt.Errorf("not attached")
	}

	pages, err := browser.Pages()
	if err != nil {
		return nil, fmt.Errorf("failed to list the pages: %w", err)
	}
	out := make([]PageInfo, 0, len(pages))
	for _, p := range pages {
		info, err := p.Info()
		if err != nil {
			continue
		}
		out = append(out, PageInfo{
			ID:     string(p.TargetID),
			Title:  info.Title,
			URL:    info.URL,
			Active: p.TargetID == current,
		})
	}
	return out, nil
}

// Select streams the given tab. An empty id picks the first visible tab.
func (s *Streamer) Select(id string) error {
	s.mu.Lock()
	browser := s.browser
	s.mu.Unlock()
	if browser == nil {
		return fmt.Errorf("not attached")
	}

	pages, err := browser.Pages()
	if err != nil {
		return fmt.Errorf("failed to list the pages: %w", err)
	}
	if len(pages) == 0 {
		return fmt.Errorf("the browser has no page")
	}

	var chosen *rod.Page
	switch {
	case id != "":
		for _, p := range pages {
			if string(p.TargetID) == id {
				chosen = p
			}
		}
		if chosen == nil {
			// Distinct from ClosePage's silence: asking to *look at* a tab
			// that has gone cannot be satisfied by doing nothing, and
			// switching to some other tab is not what was asked for.
			return fmt.Errorf("that tab has closed")
		}
	default:
		chosen = frontMost(pages)
	}

	return s.stream(chosen)
}

// blankPage is what a new tab opens. It is the browser convention, and it
// leaves the URL box as the next thing to use.
const blankPage = "about:blank"

/*
NewPage opens a tab and streams it.

Streaming it is the point. A new tab that appeared in the strip but left the
picture on the old one would look like nothing had happened, and a real browser
puts you in the tab it just opened.
*/
func (s *Streamer) NewPage(url string) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
	s.mu.Lock()
	browser := s.browser
	s.mu.Unlock()
	if browser == nil {
		return fmt.Errorf("not attached")
	}
	if url == "" {
		url = blankPage
	}
	url = resolveURL(url)

	page, err := browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return fmt.Errorf("failed to open a tab: %w", err)
	}
	if err := s.stream(page); err != nil {
		return err
	}
	s.publishPages()
	return nil
}

/*
ClosePage closes one tab.

Two things have to happen in order. The screencast holds the page, so it is
stopped before Chrome is asked to destroy it; otherwise the stream is left
attached to a target that no longer exists and simply goes quiet. Then, if the
closed tab was the one being streamed, the stream moves to another tab rather
than leaving the viewer on a frozen last frame.

The last tab is refused. Chrome quits when its final tab closes, and the browser
belongs to ATR rather than to this viewer, so one click in a web page must not
be able to take it down.
*/
func (s *Streamer) ClosePage(id string) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
	s.mu.Lock()
	browser := s.browser
	current := s.targetID
	s.mu.Unlock()
	if browser == nil {
		return fmt.Errorf("not attached")
	}
	if id == "" {
		return fmt.Errorf("no page id to close")
	}

	pages, err := browser.Pages()
	if err != nil {
		return fmt.Errorf("failed to list the pages: %w", err)
	}
	var target *rod.Page
	rest := make(rod.Pages, 0, len(pages))
	for _, p := range pages {
		if string(p.TargetID) == id {
			target = p
			continue
		}
		rest = append(rest, p)
	}

	// "Is it still there?" is asked before "would closing it be fatal?", and
	// the order matters. Two clicks on the same cross land either side of the
	// one-second tab refresh: with the checks the other way round, the second
	// click on the second-to-last tab answered "this is the last tab" -- about
	// a tab the viewer was not trying to close.
	//
	// Already gone is not a failure either. Closing it is what was asked for,
	// and it is closed.
	if target == nil {
		s.publishPages()
		return nil
	}
	if len(rest) == 0 {
		return fmt.Errorf("this is the last tab, and closing it would close the browser")
	}

	streamed := string(current) == id
	if streamed {
		s.stop()
	}
	if err := target.Close(); err != nil {
		// The stream is already down, so put it back on something rather than
		// leaving the viewer with no frames after a failed close.
		if streamed {
			_ = s.stream(frontMost(pages))
		}
		return fmt.Errorf("failed to close the page: %w", err)
	}
	if streamed {
		if err := s.stream(frontMost(rest)); err != nil {
			return err
		}
	}
	s.publishPages()
	return nil
}

// frontMost prefers the visible tab, because only that one produces frames.
func frontMost(pages rod.Pages) *rod.Page {
	for _, p := range pages {
		res, err := p.Eval(`() => document.visibilityState`)
		if err == nil && res.Value.Str() == "visible" {
			return p
		}
	}
	return pages[0]
}

func (s *Streamer) stream(page *rod.Page) error {
	s.stop()

	// Chrome sends nothing for a background tab.
	if err := (proto.PageBringToFront{}).Call(page); err != nil {
		return fmt.Errorf("failed to bring the page to the front: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	bound := page.Context(ctx)

	s.mu.Lock()
	s.page = bound
	s.targetID = page.TargetID
	s.cancel = cancel
	s.seq = 0
	s.lastAt = time.Now()
	s.live = true
	s.mu.Unlock()

	// The tap rides the same bound page, so it starts with the stream and dies
	// with it. A tab switch therefore moves the log along with the pixels.
	s.startTap(bound, string(page.TargetID))

	go bound.EachEvent(func(e *proto.PageScreencastFrame) {
		// Acknowledge at once. Chrome stops the stream without this, and it
		// must not wait for a slow viewer.
		_ = proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(bound)

		s.mu.Lock()
		s.seq++
		seq := s.seq
		s.lastAt = time.Now()
		wasDown := !s.live
		s.live = true
		s.mu.Unlock()

		if wasDown {
			s.publishStatus(true)
		}

		md := e.Metadata
		frame := &Frame{Seq: seq, JPEG: e.Data}
		if md != nil {
			frame.DeviceWidth = md.DeviceWidth
			frame.DeviceHeight = md.DeviceHeight
			frame.PageScale = md.PageScaleFactor
			frame.OffsetTop = md.OffsetTop
			frame.ScrollX = md.ScrollOffsetX
			frame.ScrollY = md.ScrollOffsetY
		}
		s.mu.Lock()
		s.last = frame
		s.mu.Unlock()
		s.emit(frame)
	})()

	everyNth := int(math.Max(1, math.Round(60/float64(s.opts.FPS))))
	q, mw, mh := s.opts.Quality, s.opts.MaxWidth, s.opts.MaxWidth*2
	if err := (proto.PageStartScreencast{
		Format:        proto.PageStartScreencastFormatJpeg,
		Quality:       &q,
		MaxWidth:      &mw,
		MaxHeight:     &mh,
		EveryNthFrame: &everyNth,
	}).Call(bound); err != nil {
		cancel()
		return fmt.Errorf("failed to start the screencast: %w", err)
	}

	s.publishPages()

	// Send one image at once and mark the stream healthy. A still page emits
	// no frame, so without this the viewer would keep the stall banner and an
	// empty canvas after every tab switch.
	if frame, err := s.Snapshot(); err == nil {
		s.emit(frame)
	}
	s.mu.Lock()
	s.live = true
	s.lastAt = time.Now()
	s.mu.Unlock()
	s.publishStatus(true)

	return nil
}

func (s *Streamer) stop() {
	s.mu.Lock()
	cancel := s.cancel
	page := s.page
	s.cancel = nil
	s.page = nil
	// Clearing live matters: without it a stream() that fails after stop()
	// leaves Live() reporting true with no page behind it.
	s.live = false
	// A drag belongs to the page it started on. Carrying it to the next tab
	// would drop its payload somewhere the user never dragged it.
	s.drag = nil
	s.mu.Unlock()

	if page != nil {
		_ = proto.PageStopScreencast{}.Call(page)
	}
	if cancel != nil {
		cancel()
	}
}

// Close releases the stream. It never closes the browser, because the browser
// belongs to ATR, not to this viewer.
func (s *Streamer) Close() {
	s.stop()
}

// Watch reports a stalled stream and applies the foreground policy.
//
// A background tab emits no frames and no error, so silence is the only
// signal that the agent moved the foreground somewhere else.
func (s *Streamer) Watch(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			page := s.page
			idle := time.Since(s.lastAt)
			live := s.live
			policy := s.policy
			s.mu.Unlock()

			if page == nil {
				continue
			}
			s.pollPages()
			if idle < 2*time.Second {
				continue
			}

			// A static page is legitimately silent. Only report a stall when
			// the page is no longer the visible one.
			res, err := page.Eval(`() => document.visibilityState`)
			visible := err == nil && res.Value.Str() == "visible"
			if visible {
				continue
			}

			if live {
				s.mu.Lock()
				s.live = false
				s.mu.Unlock()
				s.publishStatus(false)
			}

			switch policy {
			case "hold":
				_ = (proto.PageBringToFront{}).Call(page)
			case "pin":
				// Stay on this tab and never fight for the foreground. The
				// heartbeat keeps supplying frames while the tab is hidden.
			case "follow":
				s.mu.Lock()
				browser := s.browser
				s.mu.Unlock()
				if browser == nil {
					continue
				}
				pages, err := browser.Pages()
				if err != nil || len(pages) == 0 {
					continue
				}
				front := frontMost(pages)
				if front.TargetID != s.targetID {
					_ = s.stream(front)
				}
			}
		}
	}
}

// SetPolicy chooses what happens when another tab takes the foreground.
//
// "follow" streams whichever tab is in front. "pin" stays on the selected tab
// and lets the heartbeat supply its frames. "hold" pulls the selected tab back
// to the front, which interrupts whoever is driving the browser.
func (s *Streamer) SetPolicy(p string) {
	switch p {
	case "hold", "follow", "pin":
	default:
		return
	}
	s.mu.Lock()
	s.policy = p
	s.mu.Unlock()
}

// Policy reports the current foreground policy.
func (s *Streamer) Policy() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

// Heartbeat captures a frame whenever the screencast has been silent for
// longer than every.
//
// This is not a safety net, it is the frame source for two ordinary cases. A
// page that does not move emits nothing at all, and a page in a background tab
// emits nothing either, because Chrome does not composite one. The spike in
// docs/session-recording.md measured Page.captureScreenshot on a hidden tab:
// it returns in about 65 ms, it needs no Page.bringToFront, and the image is
// current, not a stale composite. So a recording of a pinned tab keeps
// advancing without ever taking the foreground away from the agent.
//
// The heartbeat tracks its own clock. It must not touch lastAt, because Watch
// reads lastAt to detect a stall and to follow the foreground.
func (s *Streamer) Heartbeat(ctx context.Context, every time.Duration) {
	if every <= 0 {
		return
	}
	ticker := time.NewTicker(every / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			quiet := now.Sub(s.lastAt) >= every && now.Sub(s.lastBeat) >= every
			ready := s.page != nil
			s.mu.Unlock()

			if !ready || !quiet {
				continue
			}
			frame, err := s.Snapshot()
			if err != nil {
				continue
			}
			s.mu.Lock()
			s.lastBeat = time.Now()
			s.mu.Unlock()
			s.emit(frame)
		}
	}
}

// Navigate loads a URL in the streamed tab.
func (s *Streamer) Navigate(url string) error {
	if err := s.viewOnly(); err != nil {
		return err
	}
	s.mu.Lock()
	page := s.page
	s.mu.Unlock()
	if page == nil {
		return fmt.Errorf("no page is selected")
	}
	url = resolveURL(url)
	if err := page.Navigate(url); err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}
	s.publishPages()
	return nil
}

func (s *Streamer) publishStatus(live bool) {
	msg, err := json.Marshal(map[string]any{
		"t":         "status",
		"streaming": live,
		"viewers":   s.viewers(),
	})
	if err != nil {
		return
	}
	s.emitText(msg)
}

func (s *Streamer) publishPages() { s.sendPages(true) }

// pollPages sends the tab list when something about it changed.
//
// Chrome pushes no tab list, and the agent navigates without telling this
// server, so nothing else notices a page that moved on its own. The live view
// needs it to keep the tab titles honest, and the recorder needs it to mark the
// navigation on the timeline. A single page application that only rewrites its
// URL is caught here too, which is the common case in the apps we record.
func (s *Streamer) pollPages() { s.sendPages(false) }

func (s *Streamer) sendPages(force bool) {
	pages, err := s.Pages()
	if err != nil {
		return
	}
	msg, err := json.Marshal(map[string]any{"t": "pages", "pages": pages})
	if err != nil {
		return
	}
	s.mu.Lock()
	same := bytes.Equal(msg, s.lastPages)
	s.lastPages = msg
	s.mu.Unlock()
	if same && !force {
		return
	}
	s.emitText(msg)
}

// LastFrame returns the most recent frame, so a viewer that connects to a
// static page sees the page at once instead of a blank canvas.
func (s *Streamer) LastFrame() *Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Snapshot captures one image on demand.
//
// Chrome emits a screencast frame only when the page changes. A viewer that
// joins a page which is already still would otherwise wait at a blank canvas
// until something moved.
func (s *Streamer) Snapshot() (*Frame, error) {
	page := s.CurrentPage()
	if page == nil {
		return nil, fmt.Errorf("no page is selected")
	}

	quality := s.opts.Quality
	shot, err := (proto.PageCaptureScreenshot{
		Format:  proto.PageCaptureScreenshotFormatJpeg,
		Quality: &quality,
	}).Call(page)
	if err != nil {
		return nil, fmt.Errorf("failed to capture the page: %w", err)
	}

	frame := &Frame{JPEG: shot.Data, PageScale: 1}
	if m, err := (proto.PageGetLayoutMetrics{}).Call(page); err == nil &&
		m.CSSVisualViewport != nil {
		frame.DeviceWidth = m.CSSVisualViewport.ClientWidth
		frame.DeviceHeight = m.CSSVisualViewport.ClientHeight
		frame.PageScale = m.CSSVisualViewport.Scale
		frame.ScrollX = m.CSSVisualViewport.PageX
		frame.ScrollY = m.CSSVisualViewport.PageY
	}

	s.mu.Lock()
	s.seq++
	frame.Seq = s.seq
	s.last = frame
	s.mu.Unlock()

	return frame, nil
}

// CurrentPage exposes the streamed page to the input layer.
func (s *Streamer) CurrentPage() *rod.Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.page
}
