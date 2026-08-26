package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
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
	hub  *Hub
	opts Options

	// streamMu serialises stream() end to end; mu guards the fields below.
	streamMu sync.Mutex

	mu       sync.Mutex
	browser  *rod.Browser
	page     *rod.Page
	targetID proto.TargetTargetID
	cancel   context.CancelFunc
	seq      int
	lastAt   time.Time
	live     bool
	closed   bool
	// switching is true while stream() is between tearing the old stream down
	// and committing the new one, so the watchdog does not mistake that window
	// for a dead view. gen identifies the current stream, so a frame callback
	// from a cancelled one cannot write over its successor's state.
	switching bool
	gen       int
	policy    string // "follow" or "hold"

	// last holds the most recent frame. A static page produces one frame and
	// then nothing, so a viewer that connects later needs this to see anything.
	last *Frame
	// drag is the HTML5 drag Chrome handed back, if one is in flight.
	drag *dragSession
}

func NewStreamer(hub *Hub, opts Options) *Streamer {
	if opts.Quality <= 0 {
		opts.Quality = 60
	}
	if opts.MaxWidth <= 0 {
		opts.MaxWidth = 1600
	}
	if opts.FPS <= 0 {
		opts.FPS = 20
	}
	return &Streamer{hub: hub, opts: opts, policy: "follow"}
}

// Attach connects to the browser at the given CDP endpoint.
//
// NoDefaultDevice matters: rod otherwise applies its default device emulation
// to every page it touches, which would rewrite the viewport and user agent of
// the tabs ATR is already driving. The viewer never owns the browser.
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
			return fmt.Errorf("no page with id %s", id)
		}
	default:
		chosen = frontMost(pages)
	}

	return s.stream(chosen)
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
	// stream() is reachable from the HTTP handler, the WebSocket dispatch and
	// the watchdog at once. Without this, two callers interleave stop() and
	// StartScreencast and leave s.page on one target with the stream on another.
	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	// Close may have run while this call waited for streamMu. Bail rather than
	// bringing a tab to the front during shutdown.
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("the live view is shutting down")
	}

	// Chrome sends nothing for a background tab, so the page has to come to the
	// front. Do it before tearing the old stream down: if it fails, the viewer
	// keeps the stream it had instead of being left with no page at all.
	if err := (proto.PageBringToFront{}).Call(page); err != nil {
		return fmt.Errorf("failed to bring the page to the front: %w", err)
	}

	s.stop()

	ctx, cancel := context.WithCancel(context.Background())
	bound := page.Context(ctx)

	s.mu.Lock()
	s.seq = 0
	s.gen++
	gen := s.gen
	s.switching = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.switching = false
		s.mu.Unlock()
	}()

	go bound.EachEvent(func(e *proto.PageScreencastFrame) {
		// Acknowledge at once. Chrome stops the stream without this, and it
		// must not wait for a slow viewer.
		_ = proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(bound)

		s.mu.Lock()
		if s.gen != gen {
			// A frame from a stream that has already been replaced. Writing
			// here would resurrect live and overwrite the new tab's last frame.
			s.mu.Unlock()
			return
		}
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
		if s.gen != gen {
			s.mu.Unlock()
			return
		}
		s.last = frame
		s.mu.Unlock()
		s.hub.Broadcast(frame)
	})()

	everyNth := int(math.Max(1, math.Round(60/float64(s.opts.FPS))))
	// Before the commit below, so a failure takes the same rollback path as the
	// screencast rather than leaving committed state behind.
	if err := s.interceptDrags(bound, gen); err != nil {
		cancel()
		s.publishStatus(false)
		return err
	}

	q, mw, mh := s.opts.Quality, s.opts.MaxWidth, s.opts.MaxWidth*2
	if err := (proto.PageStartScreencast{
		Format:        proto.PageStartScreencastFormatJpeg,
		Quality:       &q,
		MaxWidth:      &mw,
		MaxHeight:     &mh,
		EveryNthFrame: &everyNth,
	}).Call(bound); err != nil {
		cancel()
		s.publishStatus(false)
		return fmt.Errorf("failed to start the screencast: %w", err)
	}

	// Publish only once the stream is actually running. Committing earlier left
	// a cancelled page behind that still reported streaming: true.
	//
	// Re-check closed in the same critical section: Close may have run while
	// this call was inside a CDP round trip, and committing afterwards would
	// leave a live screencast nobody owns.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		_ = proto.PageStopScreencast{}.Call(bound)
		return fmt.Errorf("the live view is shutting down")
	}
	s.page = bound
	s.targetID = page.TargetID
	s.cancel = cancel
	s.lastAt = time.Now()
	s.live = true
	s.mu.Unlock()

	s.publishPages()

	// Send one image at once. A still page emits no frame, so without this the
	// viewer would keep an empty canvas after every tab switch.
	if frame, err := s.Snapshot(); err == nil {
		s.hub.Broadcast(frame)
	}
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
//
// It does not take streamMu: an in-flight stream() can sit in several unbounded
// CDP round trips, and waiting for it would hang Ctrl-C behind a wedged browser.
// The closed flag stops that stream() from republishing instead.
func (s *Streamer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stop()
}

// Watch reports a stalled stream and applies the foreground policy.
//
// A background tab emits no frames and no error, so silence is the only
// signal that the agent moved the foreground somewhere else.
func (s *Streamer) Watch(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// retries paces the recovery attempts below.
	retries := 0

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
			current := s.targetID
			switching := s.switching
			closed := s.closed
			s.mu.Unlock()

			if closed {
				return
			}

			// No page is selected. That is either an ordinary tab switch in
			// flight, which must be left alone, or an earlier attempt that
			// failed, which is worth recovering from.
			if page == nil {
				if switching {
					continue
				}
				if live {
					s.mu.Lock()
					s.live = false
					s.mu.Unlock()
					s.publishStatus(false)
				}
				// Recover regardless of policy: "hold" holds a tab, and there
				// is no tab to hold here. Back off so a browser that refuses to
				// focus is not hammered with Pages() plus an Eval per tab every
				// second, forever.
				retries++
				if retries <= 1 || retries%(1<<min(retries-1, 5)) == 0 {
					if err := s.Select(""); err == nil {
						retries = 0
					}
				}
				continue
			}
			retries = 0

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
				if front.TargetID != current {
					_ = s.stream(front)
				}
			}
		}
	}
}

// SetPolicy chooses what happens when another tab takes the foreground.
func (s *Streamer) SetPolicy(p string) {
	if p != "hold" && p != "follow" {
		return
	}
	s.mu.Lock()
	s.policy = p
	s.mu.Unlock()
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
		"viewers":   s.hub.Count(),
	})
	if err != nil {
		return
	}
	s.hub.BroadcastText(msg)
}

func (s *Streamer) publishPages() {
	pages, err := s.Pages()
	if err != nil {
		return
	}
	msg, err := json.Marshal(map[string]any{"t": "pages", "pages": pages})
	if err != nil {
		return
	}
	s.hub.BroadcastText(msg)
}

// Live reports whether frames are currently flowing, so a viewer that joins a
// stalled stream is told the truth instead of an unconditional "streaming".
func (s *Streamer) Live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live
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
