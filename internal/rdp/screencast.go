package rdp

import (
	"context"
	"encoding/json"
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

	mu       sync.Mutex
	browser  *rod.Browser
	page     *rod.Page
	targetID proto.TargetTargetID
	cancel   context.CancelFunc
	seq      int
	lastAt   time.Time
	live     bool
	policy   string // "follow" or "hold"

	// last holds the most recent frame. A static page produces one frame and
	// then nothing, so a viewer that connects later needs this to see anything.
	last *Frame
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
func (s *Streamer) Attach(cdpURL string) error {
	browser := rod.New().ControlURL(cdpURL)
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
		s.hub.Broadcast(frame)
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
		s.hub.Broadcast(frame)
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

			if page == nil || idle < 2*time.Second {
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
				if front.TargetID != s.targetID {
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

// Navigate loads a URL in the streamed tab.
func (s *Streamer) Navigate(url string) error {
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
