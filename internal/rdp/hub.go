package rdp

import (
	"sync"
)

// Frame is one encoded screencast image with the metadata the client needs to
// map its canvas coordinates back to page coordinates.
type Frame struct {
	Seq          int     `json:"seq"`
	DeviceWidth  float64 `json:"deviceWidth"`
	DeviceHeight float64 `json:"deviceHeight"`
	PageScale    float64 `json:"pageScale"`
	OffsetTop    float64 `json:"offsetTop"`
	ScrollX      float64 `json:"scrollX"`
	ScrollY      float64 `json:"scrollY"`
	JPEG         []byte  `json:"-"`
}

// viewer is one connected browser tab.
type viewer struct {
	mu      sync.Mutex
	pending *Frame
	text    [][]byte
	wake    chan struct{}
	closed  bool
}

func newViewer() *viewer {
	return &viewer{wake: make(chan struct{}, 1)}
}

// put replaces the pending frame. It never queues, so a slow viewer always
// receives the newest image instead of a backlog of stale ones.
func (v *viewer) put(f *Frame) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.pending = f
	v.mu.Unlock()
	v.signal()
}

// send queues a control message. Control messages are not dropped.
func (v *viewer) send(msg []byte) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.text = append(v.text, msg)
	v.mu.Unlock()
	v.signal()
}

func (v *viewer) signal() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// take returns everything waiting for this viewer.
func (v *viewer) take() (*Frame, [][]byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	f := v.pending
	v.pending = nil
	msgs := v.text
	v.text = nil
	return f, msgs
}

func (v *viewer) close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()
	v.signal()
}

// Hub keeps the connected viewers and fans frames out to them.
type Hub struct {
	mu      sync.RWMutex
	viewers map[*viewer]struct{}
}

func NewHub() *Hub {
	return &Hub{viewers: make(map[*viewer]struct{})}
}

func (h *Hub) add(v *viewer) {
	h.mu.Lock()
	h.viewers[v] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(v *viewer) {
	h.mu.Lock()
	delete(h.viewers, v)
	h.mu.Unlock()
	v.close()
}

// Count reports how many viewers are connected.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.viewers)
}

// Broadcast hands a frame to every viewer.
func (h *Hub) Broadcast(f *Frame) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for v := range h.viewers {
		v.put(f)
	}
}

// BroadcastText hands a control message to every viewer.
func (h *Hub) BroadcastText(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for v := range h.viewers {
		v.send(msg)
	}
}
